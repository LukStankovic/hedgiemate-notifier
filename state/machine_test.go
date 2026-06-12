package state

import (
	"strconv"
	"testing"
)

// fullCar exercises every body-template branch: geofence + range + energy +
// limit all present. battery == limit on purpose — the strictest case for
// battery-arg tagging, because the battery and limit args render as the SAME
// string ("85%") and only the first occurrence is the battery.
func fullCar() *CarState {
	return &CarState{
		BatteryLevel:      85,
		ChargeLimitSoc:    85,
		Geofence:          "Doma",
		PreviousGeofence:  "Doma",
		ChargerPower:      11,
		ChargeEnergyAdded: 51.8,
		RatedRangeKm:      473.9,
		UpdateVersion:     "2026.20.1",
	}
}

func minimalCar() *CarState {
	return &CarState{BatteryLevel: 42}
}

// batteryEvents are all event types whose bodies contain the current battery
// percentage (i.e. everything buildNotification handles except
// software_update, which only carries a version string).
var batteryEvents = []string{
	"vehicle_falling_asleep", "vehicle_asleep", "vehicle_woke",
	"drive_started", "drive_ended",
	"charging_started", "charging_completed", "charging_interrupted",
	"battery_low", "battery_full",
	"charger_connected", "charger_disconnected",
	"geofence_entered", "geofence_exited",
}

// TestBatteryArgTaggedForEveryEvent locks the contract the relay relies on:
// every battery-bearing body has EXACTLY ONE battery_percent typed arg, its
// index points at the battery string, and its value is the raw rated
// battery_level. The relay uses it to decide the displayed SoC (usable vs
// rated vs charge limit) from values instead of string surgery — if a new
// body template forgets the tag, the relay silently falls back to the legacy
// string-matching heuristic, so this test is the only thing that notices.
func TestBatteryArgTaggedForEveryEvent(t *testing.T) {
	m := &Manager{distanceUnit: "km"}

	for _, eventType := range batteryEvents {
		for _, tc := range []struct {
			name string
			car  *CarState
		}{
			{"full", fullCar()},
			{"minimal", minimalCar()},
		} {
			t.Run(eventType+"/"+tc.name, func(t *testing.T) {
				_, _, bodyKey, bodyArgs, typed, _, _ := m.buildNotification(eventType, "Model Y", tc.car)
				if bodyKey == "" {
					t.Fatalf("no body key for %s", eventType)
				}
				bat := strconv.Itoa(tc.car.BatteryLevel) + "%"

				var batteryArgs []int
				for _, ta := range typed {
					if ta.Type == "battery_percent" {
						batteryArgs = append(batteryArgs, ta.Index)
						if int(ta.Value) != tc.car.BatteryLevel {
							t.Fatalf("battery typed value = %v, want %d", ta.Value, tc.car.BatteryLevel)
						}
					}
				}
				if len(batteryArgs) != 1 {
					t.Fatalf("want exactly 1 battery_percent typed arg, got %d (args=%v typed=%v)", len(batteryArgs), bodyArgs, typed)
				}
				idx := batteryArgs[0]
				if idx < 0 || idx >= len(bodyArgs) || bodyArgs[idx] != bat {
					t.Fatalf("battery typed arg index %d does not point at %q (args=%v)", idx, bat, bodyArgs)
				}
				// First occurrence rule: no EARLIER arg may render the same
				// string — with battery == limit the limit arg is identical,
				// and tagging the limit instead would let the relay rewrite
				// the user's configured limit.
				for i := 0; i < idx; i++ {
					if bodyArgs[i] == bat {
						t.Fatalf("battery typed arg index %d skipped an earlier %q at %d (args=%v)", idx, bat, i, bodyArgs)
					}
				}
			})
		}
	}
}

// TestSoftwareUpdateHasNoBatteryArg: the only battery-free body must not get
// a tag (the version string can never equal "<n>%", but lock it anyway).
func TestSoftwareUpdateHasNoBatteryArg(t *testing.T) {
	m := &Manager{distanceUnit: "km"}
	_, _, _, _, typed, _, _ := m.buildNotification("software_update", "Model Y", fullCar())
	for _, ta := range typed {
		if ta.Type == "battery_percent" {
			t.Fatalf("software_update must not carry a battery typed arg: %v", typed)
		}
	}
}

// TestRangeArgSurvivesBatteryTagging: the battery tag is appended to the
// existing typed args, never replacing the distance_km entry the per-user
// unit rewrite depends on.
func TestRangeArgSurvivesBatteryTagging(t *testing.T) {
	m := &Manager{distanceUnit: "km"}
	_, _, _, bodyArgs, typed, _, _ := m.buildNotification("charging_completed", "Model Y", fullCar())

	var hasRange, hasBattery bool
	for _, ta := range typed {
		switch ta.Type {
		case "distance_km":
			hasRange = true
			if ta.Index < 0 || ta.Index >= len(bodyArgs) {
				t.Fatalf("distance index out of range: %v (args=%v)", ta, bodyArgs)
			}
		case "battery_percent":
			hasBattery = true
		}
	}
	if !hasRange || !hasBattery {
		t.Fatalf("want both distance_km and battery_percent typed args, got %v", typed)
	}
}
