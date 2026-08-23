package state

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hedgiemate/notifier/mqtt"
	"github.com/hedgiemate/notifier/remotecfg"
)

func cadenceManager(t *testing.T, cfg map[string]string) *Manager {
	t.Helper()
	// The name cache persists to the working directory, so keep it in a temp
	// dir: without it these tests would write car_names.json into the package
	// and leak state between runs.
	t.Chdir(t.TempDir())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := &Manager{
		cars:   map[string]*CarState{},
		logger: logger,
		cache:  newCarNameCache(),
	}
	if cfg != nil {
		store := remotecfg.New(logger)
		store.Apply(1, cfg)
		m.SetRemoteConfig(store)
	}
	return m
}

// The whole point of shipping the seam with today's numbers: a notifier that
// never hears from the relay must tick exactly as this build always did.
func TestCadenceDefaultsMatchShippedBehaviour(t *testing.T) {
	m := cadenceManager(t, nil)

	if got := m.drivingInterval(&CarState{Speed: 50}); got != 30*time.Second {
		t.Fatalf("driving below the threshold = %v, want 30s", got)
	}
	if got := m.drivingInterval(&CarState{Speed: 81}); got != 15*time.Second {
		t.Fatalf("driving above the threshold = %v, want 15s", got)
	}
	if got := m.chargingInterval(&CarState{ChargerPower: 7}); got != 60*time.Second {
		t.Fatalf("charging on AC = %v, want 60s", got)
	}
	if got := m.chargingInterval(&CarState{ChargerPower: 50}); got != 30*time.Second {
		t.Fatalf("charging on DC = %v, want 30s", got)
	}
}

// The threshold is exclusive today (speed > 80), and that must not drift: 80
// itself stays on the slow tier.
func TestCadenceThresholdIsExclusive(t *testing.T) {
	m := cadenceManager(t, nil)
	if got := m.drivingInterval(&CarState{Speed: 80}); got != 30*time.Second {
		t.Fatalf("exactly at the threshold = %v, want the slow tier 30s", got)
	}
	if got := m.chargingInterval(&CarState{ChargerPower: 11}); got != 60*time.Second {
		t.Fatalf("exactly at the threshold = %v, want the slow tier 60s", got)
	}
}

func TestCadenceHonoursRemoteConfig(t *testing.T) {
	m := cadenceManager(t, map[string]string{
		remotecfg.KeyDriveTick:       "20",
		remotecfg.KeyDriveTickFast:   "8",
		remotecfg.KeyDriveFastAbove:  "60",
		remotecfg.KeyChargeTick:      "90",
		remotecfg.KeyChargeTickFast:  "45",
		remotecfg.KeyChargeFastAbove: "22",
	})

	if got := m.drivingInterval(&CarState{Speed: 50}); got != 20*time.Second {
		t.Fatalf("driving slow tier = %v, want 20s", got)
	}
	if got := m.drivingInterval(&CarState{Speed: 61}); got != 8*time.Second {
		t.Fatalf("driving fast tier at the new threshold = %v, want 8s", got)
	}
	if got := m.chargingInterval(&CarState{ChargerPower: 11}); got != 90*time.Second {
		t.Fatalf("charging slow tier = %v, want 90s (11 kW is below the new 22 kW threshold)", got)
	}
	if got := m.chargingInterval(&CarState{ChargerPower: 50}); got != 45*time.Second {
		t.Fatalf("charging fast tier = %v, want 45s", got)
	}
}

// A relay sending nonsense must degrade to something sane, never to a
// time.NewTicker(0) panic or a 1s push storm.
func TestCadenceClampsHostileConfig(t *testing.T) {
	m := cadenceManager(t, map[string]string{
		remotecfg.KeyDriveTick:      "0",
		remotecfg.KeyDriveTickFast:  "1",
		remotecfg.KeyChargeTick:     "99999",
		remotecfg.KeyChargeTickFast: "not-a-number",
	})

	if got := m.drivingInterval(&CarState{Speed: 10}); got != 30*time.Second {
		t.Fatalf("zero tick = %v, want the default 30s", got)
	}
	if got := m.drivingInterval(&CarState{Speed: 120}); got != remotecfg.MinTick {
		t.Fatalf("1s tick = %v, want the floor %v", got, remotecfg.MinTick)
	}
	if got := m.chargingInterval(&CarState{ChargerPower: 3}); got != remotecfg.MaxTick {
		t.Fatalf("99999s tick = %v, want the ceiling %v", got, remotecfg.MaxTick)
	}
	if got := m.chargingInterval(&CarState{ChargerPower: 150}); got != 30*time.Second {
		t.Fatalf("garbage tick = %v, want the default 30s", got)
	}
}

// Raw passthrough is off unless the relay asks, so the payload stays exactly
// the size it is today.
func TestRawPassthroughOffByDefault(t *testing.T) {
	for _, cfg := range []map[string]string{nil, {remotecfg.KeyRawFields: "false"}} {
		m := cadenceManager(t, cfg)
		m.HandleMessage("1", mqtt.FieldSpeed, "42")
		m.HandleUnknownMessage("1", mqtt.TopicField("tpms_pressure_fl"), "2.9")

		car := m.cars["1"]
		if car == nil {
			t.Fatal("car was not created")
		}
		if car.raw != nil {
			t.Fatalf("raw map populated while the flag is off: %v", car.raw)
		}
		if rawSnapshot(car) != nil {
			t.Fatal("rawSnapshot returned a map while the flag is off")
		}
		// The typed field must still be parsed as usual.
		if car.Speed != 42 {
			t.Fatalf("speed = %d, want 42", car.Speed)
		}
	}
}

func TestRawPassthroughCapturesKnownAndUnknownFields(t *testing.T) {
	m := cadenceManager(t, map[string]string{remotecfg.KeyRawFields: "true"})

	m.HandleMessage("1", mqtt.FieldSpeed, "42")
	m.HandleUnknownMessage("1", mqtt.TopicField("tpms_pressure_fl"), "2.9")

	raw := rawSnapshot(m.cars["1"])
	if raw["speed"] != "42" {
		t.Fatalf("known field missing from raw: %v", raw)
	}
	if raw["tpms_pressure_fl"] != "2.9" {
		t.Fatalf("unknown field missing from raw: %v", raw)
	}
}

// A chatty or misconfigured broker must not be able to grow the map without
// limit, and an over-long value must not ride along whole.
func TestRawPassthroughIsBounded(t *testing.T) {
	m := cadenceManager(t, map[string]string{remotecfg.KeyRawFields: "true"})

	for i := 0; i < maxRawFields*2; i++ {
		m.HandleUnknownMessage("1", mqtt.TopicField("field_"+string(rune('a'+i%26))+string(rune('a'+i/26))), "x")
	}
	long := make([]byte, maxRawValueLen*3)
	for i := range long {
		long[i] = 'y'
	}
	m.HandleUnknownMessage("1", mqtt.TopicField("long_one"), string(long))

	raw := rawSnapshot(m.cars["1"])
	if len(raw) > maxRawFields {
		t.Fatalf("raw map holds %d fields, want at most %d", len(raw), maxRawFields)
	}
	for k, v := range raw {
		if len(v) > maxRawValueLen {
			t.Fatalf("field %q value is %d bytes, want at most %d", k, len(v), maxRawValueLen)
		}
	}
}

// Turning the flag back off must drop the snapshot rather than keep shipping a
// frozen copy forever.
func TestRawPassthroughClearsWhenDisabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := remotecfg.New(logger)
	store.Apply(1, map[string]string{remotecfg.KeyRawFields: "true"})
	t.Chdir(t.TempDir())
	m := &Manager{cars: map[string]*CarState{}, logger: logger, cache: newCarNameCache()}
	m.SetRemoteConfig(store)

	m.HandleMessage("1", mqtt.FieldSpeed, "42")
	if rawSnapshot(m.cars["1"]) == nil {
		t.Fatal("raw map was not populated while enabled")
	}

	store.Apply(2, map[string]string{remotecfg.KeyRawFields: "false"})
	m.HandleMessage("1", mqtt.FieldSpeed, "43")
	if got := rawSnapshot(m.cars["1"]); got != nil {
		t.Fatalf("raw map survived the flag going off: %v", got)
	}
}

// The raw map has to actually reach the wire, not just the CarState: the
// snapshot is taken while the manager's lock is held and the payload is
// marshalled after it is released.
func TestRawPassthroughReachesThePayload(t *testing.T) {
	m := cadenceManager(t, map[string]string{remotecfg.KeyRawFields: "true"})
	m.HandleMessage("1", mqtt.FieldDisplayName, "Ňufík")
	m.HandleMessage("1", mqtt.FieldBatteryLevel, "81")
	m.HandleUnknownMessage("1", mqtt.TopicField("tpms_pressure_fl"), "2.9")

	payload := m.buildPayload("1", "live_activity_driving_update", m.cars["1"])
	if payload.Data.Raw["tpms_pressure_fl"] != "2.9" {
		t.Fatalf("payload.Data.Raw = %v, want the unknown field", payload.Data.Raw)
	}
	// And the snapshot must be a copy: mutating the car afterwards must not
	// change a payload already handed off.
	m.HandleUnknownMessage("1", mqtt.TopicField("tpms_pressure_fl"), "9.9")
	if payload.Data.Raw["tpms_pressure_fl"] != "2.9" {
		t.Fatal("payload shared the live map instead of a snapshot")
	}
}

// With the flag off the field must be absent from the payload entirely, not
// present and empty, so the event size is unchanged.
func TestRawAbsentFromPayloadWhenDisabled(t *testing.T) {
	m := cadenceManager(t, nil)
	m.HandleMessage("1", mqtt.FieldBatteryLevel, "81")

	payload := m.buildPayload("1", "live_activity_driving_update", m.cars["1"])
	if payload.Data.Raw != nil {
		t.Fatalf("payload.Data.Raw = %v, want nil", payload.Data.Raw)
	}
}
