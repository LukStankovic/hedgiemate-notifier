// Package remotecfg holds the relay-driven runtime configuration: the values
// the relay may change WITHOUT a notifier release.
//
// Why it exists: users self-host the notifier, so every release depends on them
// pulling a new image and the tail of old versions never fully disappears. A
// tuning value hardcoded here is therefore effectively unchangeable in the
// field. This store is the seam that makes it changeable — the relay sends
// key/value pairs on the response to POST /v1/events, and the notifier applies
// them at the next ticker boundary.
//
// Three rules keep it safe:
//
//  1. Absent config means today's behaviour. Every getter takes the caller's
//     default, so a relay that sends nothing (or an older relay that doesn't
//     know about config at all) leaves the notifier bit-for-bit as it was.
//  2. Every value is clamped by the CALLER's bounds, not trusted. The relay is
//     trusted to tune, not to break: a zero would panic time.NewTicker, a 1s
//     tick would burn the user's ActivityKit budget and battery, and a 2h tick
//     would silently freeze a Live Activity.
//  3. Unknown keys are ignored, not an error. That is what lets the relay ship
//     a new knob later without another notifier release.
//
// The store never controls WHETHER something is sent, only how often it is
// sampled. Event filtering is the relay's job; a bad config here must not be
// able to silence a user's notifications.
package remotecfg

import (
	"log/slog"
	"strconv"
	"sync"
	"time"
)

// Keys the relay may set today. This list is not a whitelist — Apply keeps
// every key it is given, and the getters decide what is understood — it is
// documentation of what this build reads.
const (
	KeyDriveTick       = "drive.tick_s"
	KeyDriveTickFast   = "drive.tick_fast_s"
	KeyDriveFastAbove  = "drive.fast_above_kmh"
	KeyChargeTick      = "charge.tick_s"
	KeyChargeTickFast  = "charge.tick_fast_s"
	KeyChargeFastAbove = "charge.fast_above_kw"
	KeyRawFields       = "data.raw_enabled"
)

// Tick bounds shared by both Live Activity tickers. 5s is the floor because
// TeslaMate itself polls the car every ~2.5s, so nothing below that samples
// anything new; 120s is the ceiling because past that a "live" card is a lie.
const (
	MinTick = 5 * time.Second
	MaxTick = 120 * time.Second
)

// Store holds the last configuration the relay sent. Safe for concurrent use:
// the relay client writes from its HTTP goroutines, the state manager reads
// from the ticker goroutines.
type Store struct {
	mu      sync.RWMutex
	version int
	values  map[string]string
	logger  *slog.Logger

	// warned dedupes the "I ignored / clamped this" logs so a permanently bad
	// value costs one line, not one line per tick.
	warned map[string]bool
}

func New(logger *slog.Logger) *Store {
	return &Store{
		values: map[string]string{},
		logger: logger,
		warned: map[string]bool{},
	}
}

// Apply installs a newer configuration and reports whether it changed anything.
//
// The version gate matters: responses arrive on concurrent event POSTs and can
// therefore come back out of order, so a stale body must not be able to undo
// the newest config. version <= 0 is treated as "no config" and ignored, which
// is what an older relay's response decodes to.
func (s *Store) Apply(version int, values map[string]string) bool {
	if version <= 0 || values == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if version <= s.version {
		return false
	}
	s.version = version
	s.values = make(map[string]string, len(values))
	for k, v := range values {
		s.values[k] = v
	}
	// Reset the warn latches: a new config deserves fresh complaints.
	s.warned = map[string]bool{}
	s.logger.Info("applied remote config", "version", version, "keys", len(values))
	return true
}

// Version is the currently applied config version (0 = none).
func (s *Store) Version() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

// Duration reads a whole-seconds value, clamped to [min, max]. Anything the
// relay sends that isn't a positive integer leaves def in place.
func (s *Store) Duration(key string, def, min, max time.Duration) time.Duration {
	raw, ok := s.lookup(key)
	if !ok {
		return def
	}
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		s.warn(key, "ignoring unusable remote config value", "value", raw, "using", def.String())
		return def
	}
	d := time.Duration(secs) * time.Second
	if d < min {
		s.warn(key, "clamping remote config value up to the floor", "value", d.String(), "using", min.String())
		return min
	}
	if d > max {
		s.warn(key, "clamping remote config value down to the ceiling", "value", d.String(), "using", max.String())
		return max
	}
	return d
}

// Float reads a numeric threshold (speed in km/h, power in kW), clamped.
func (s *Store) Float(key string, def, min, max float64) float64 {
	raw, ok := s.lookup(key)
	if !ok {
		return def
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		s.warn(key, "ignoring unusable remote config value", "value", raw, "using", def)
		return def
	}
	if v < min {
		s.warn(key, "clamping remote config value up to the floor", "value", v, "using", min)
		return min
	}
	if v > max {
		s.warn(key, "clamping remote config value down to the ceiling", "value", v, "using", max)
		return max
	}
	return v
}

// Bool reads a flag. Only the canonical strconv spellings count; anything else
// keeps def, because a typo must not silently turn a feature on.
func (s *Store) Bool(key string, def bool) bool {
	raw, ok := s.lookup(key)
	if !ok {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		s.warn(key, "ignoring unusable remote config value", "value", raw, "using", def)
		return def
	}
	return v
}

func (s *Store) lookup(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.values[key]
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

func (s *Store) warn(key, msg string, args ...any) {
	s.mu.Lock()
	already := s.warned[key]
	s.warned[key] = true
	s.mu.Unlock()
	if already {
		return
	}
	s.logger.Warn(msg, append([]any{"key", key}, args...)...)
}
