package state

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var updateContract = flag.Bool("update-contract", false, "regenerate notifier→relay contract golden payloads")

// contractCar is the vehicle snapshot used to freeze the EventPayload JSON.
// Every field the relay reads is set; driveStartTime stays zero so
// Distance/Duration are deterministic.
func contractCar() *CarState {
	return &CarState{
		BatteryLevel:      79,
		UsableBattery:     79,
		ChargeLimitSoc:    80,
		ChargerPower:      11,
		ChargeEnergyAdded: 53.0,
		TimeToFullCharge:  0,
		RatedRangeKm:      446.24,
		Geofence:          "Doma",
		PreviousGeofence:  "Doma",
		State:             "online",
		ChargingState:     "Complete",
		PluggedIn:         true,
		UpdateVersion:     "2026.20.1",
		Speed:             0,
		OutsideTemp:       19.5,
	}
}

// contractEvents is every event type the notifier emits.
var contractEvents = []string{
	"vehicle_falling_asleep", "vehicle_asleep", "vehicle_woke",
	"drive_started", "drive_ended",
	"charging_started", "charging_completed", "charging_interrupted",
	"battery_low", "battery_full",
	"charger_connected", "charger_disconnected",
	"software_update",
	"geofence_entered", "geofence_exited",
	"sentry_recording",
}

// TestEventPayloadContract freezes the EventPayload JSON for every event type
// into testdata/contract/<event>.json. A change to a field name, loc-key, or
// arg layout fails the test until the golden is regenerated (-update-contract),
// so the wire format the relay parses can't drift unnoticed.
func TestEventPayloadContract(t *testing.T) {
	m := &Manager{distanceUnit: "km"}
	dir := filepath.Join("testdata", "contract")
	if *updateContract {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	for _, eventType := range contractEvents {
		t.Run(eventType, func(t *testing.T) {
			payload := m.buildPayload("1", eventType, contractCar())
			payload.Timestamp = time.Time{} // zero the only non-deterministic field

			got, err := json.MarshalIndent(payload, "", "  ")
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got = append(got, '\n')

			path := filepath.Join(dir, eventType+".json")
			if *updateContract {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run `go test -run TestEventPayloadContract -update-contract`): %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("EventPayload for %s drifted from golden.\n--- got ---\n%s\n--- want ---\n%s\nIf intentional, regenerate: go test ./state -run TestEventPayloadContract -update-contract",
					eventType, got, want)
			}
		})
	}
}
