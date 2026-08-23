package remotecfg

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

func testStore() *Store {
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// An empty store must behave as "no config at all": every getter returns the
// caller's default, which is what keeps a relay that sends nothing (or an older
// relay that has never heard of config) bit-for-bit identical to the old build.
func TestEmptyStoreReturnsDefaults(t *testing.T) {
	s := testStore()
	if got := s.Duration(KeyDriveTick, 30*time.Second, MinTick, MaxTick); got != 30*time.Second {
		t.Fatalf("Duration on empty store = %v, want 30s", got)
	}
	if got := s.Float(KeyDriveFastAbove, 80, 0, 400); got != 80 {
		t.Fatalf("Float on empty store = %v, want 80", got)
	}
	if s.Bool(KeyRawFields, false) {
		t.Fatal("Bool on empty store = true, want false")
	}
	if s.Version() != 0 {
		t.Fatalf("Version on empty store = %d, want 0", s.Version())
	}
}

func TestApplyAndRead(t *testing.T) {
	s := testStore()
	if !s.Apply(1, map[string]string{KeyDriveTick: "10", KeyDriveFastAbove: "60", KeyRawFields: "true"}) {
		t.Fatal("Apply(1) returned false")
	}
	if got := s.Duration(KeyDriveTick, 30*time.Second, MinTick, MaxTick); got != 10*time.Second {
		t.Fatalf("drive tick = %v, want 10s", got)
	}
	if got := s.Float(KeyDriveFastAbove, 80, 0, 400); got != 60 {
		t.Fatalf("fast above = %v, want 60", got)
	}
	if !s.Bool(KeyRawFields, false) {
		t.Fatal("raw fields = false, want true")
	}
}

// The relay is trusted to tune, not to break: a value below the floor would burn
// the user's ActivityKit budget and battery, one above the ceiling would freeze
// the card, and a zero would panic time.NewTicker.
func TestClamps(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"below floor", "1", MinTick},
		{"above ceiling", "3600", MaxTick},
		{"zero falls back to default", "0", 30 * time.Second},
		{"negative falls back to default", "-5", 30 * time.Second},
		{"garbage falls back to default", "fast", 30 * time.Second},
		{"empty falls back to default", "", 30 * time.Second},
		{"at floor", "5", MinTick},
		{"at ceiling", "120", MaxTick},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore()
			s.Apply(1, map[string]string{KeyDriveTick: tc.value})
			if got := s.Duration(KeyDriveTick, 30*time.Second, MinTick, MaxTick); got != tc.want {
				t.Fatalf("value %q gave %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestFloatClamps(t *testing.T) {
	s := testStore()
	s.Apply(1, map[string]string{KeyDriveFastAbove: "9000"})
	if got := s.Float(KeyDriveFastAbove, 80, 0, 400); got != 400 {
		t.Fatalf("got %v, want clamp to 400", got)
	}
	s.Apply(2, map[string]string{KeyDriveFastAbove: "-10"})
	if got := s.Float(KeyDriveFastAbove, 80, 0, 400); got != 0 {
		t.Fatalf("got %v, want clamp to 0", got)
	}
}

// A typo must not silently turn a feature on.
func TestBoolOnlyAcceptsCanonicalSpellings(t *testing.T) {
	s := testStore()
	s.Apply(1, map[string]string{KeyRawFields: "yes please"})
	if s.Bool(KeyRawFields, false) {
		t.Fatal("garbage value enabled the flag")
	}
	s.Apply(2, map[string]string{KeyRawFields: "1"})
	if !s.Bool(KeyRawFields, false) {
		t.Fatal(`"1" should parse as true`)
	}
}

// Responses to concurrent event POSTs can come back out of order, so an older
// body must never undo the newest config.
func TestVersionGate(t *testing.T) {
	s := testStore()
	s.Apply(5, map[string]string{KeyDriveTick: "10"})

	if s.Apply(4, map[string]string{KeyDriveTick: "90"}) {
		t.Fatal("older version was applied")
	}
	if s.Apply(5, map[string]string{KeyDriveTick: "90"}) {
		t.Fatal("same version was re-applied")
	}
	if got := s.Duration(KeyDriveTick, 30*time.Second, MinTick, MaxTick); got != 10*time.Second {
		t.Fatalf("drive tick = %v, want the version-5 value 10s", got)
	}
	if !s.Apply(6, map[string]string{KeyDriveTick: "90"}) {
		t.Fatal("newer version was rejected")
	}
	if got := s.Duration(KeyDriveTick, 30*time.Second, MinTick, MaxTick); got != 90*time.Second {
		t.Fatalf("drive tick = %v, want 90s", got)
	}
}

// version <= 0 is what an older relay's response decodes to.
func TestApplyIgnoresMissingConfig(t *testing.T) {
	s := testStore()
	if s.Apply(0, map[string]string{KeyDriveTick: "10"}) {
		t.Fatal("version 0 was applied")
	}
	if s.Apply(1, nil) {
		t.Fatal("nil values were applied")
	}
	if got := s.Duration(KeyDriveTick, 30*time.Second, MinTick, MaxTick); got != 30*time.Second {
		t.Fatalf("drive tick = %v, want the default 30s", got)
	}
}

// Unknown keys are the whole point: the relay must be able to ship a knob this
// build has never heard of without breaking it, and without a release.
func TestUnknownKeysAreHarmless(t *testing.T) {
	s := testStore()
	if !s.Apply(1, map[string]string{
		"drive.tick_s":             "10",
		"something.invented.later": "42",
		"":                         "empty key",
	}) {
		t.Fatal("Apply returned false")
	}
	if got := s.Duration(KeyDriveTick, 30*time.Second, MinTick, MaxTick); got != 10*time.Second {
		t.Fatalf("known key not applied alongside unknown ones: %v", got)
	}
}

// A new config replaces the old one wholesale: a key the relay stopped sending
// must fall back to the default, not linger from the previous version.
func TestApplyReplacesRatherThanMerges(t *testing.T) {
	s := testStore()
	s.Apply(1, map[string]string{KeyDriveTick: "10", KeyChargeTick: "20"})
	s.Apply(2, map[string]string{KeyDriveTick: "15"})
	if got := s.Duration(KeyChargeTick, 60*time.Second, MinTick, MaxTick); got != 60*time.Second {
		t.Fatalf("charge tick = %v, want the default 60s after the key was dropped", got)
	}
}

// The store is read from ticker goroutines while HTTP goroutines write it.
func TestConcurrentAccess(t *testing.T) {
	s := testStore()
	done := make(chan struct{})
	go func() {
		for i := 1; i <= 200; i++ {
			s.Apply(i, map[string]string{KeyDriveTick: "10"})
		}
		close(done)
	}()
	for i := 0; i < 200; i++ {
		_ = s.Duration(KeyDriveTick, 30*time.Second, MinTick, MaxTick)
		_ = s.Bool(KeyRawFields, false)
	}
	<-done
}
