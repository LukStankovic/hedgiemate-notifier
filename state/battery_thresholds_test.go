package state

import "testing"

// runLevels replays battery_level readings through the threshold decision and
// counts how many battery_low / battery_full alerts fire.
func runLevels(m *Manager, car *CarState, levels []int) (low, full int) {
	for _, lv := range levels {
		car.BatteryLevel = lv
		for _, e := range m.batteryThresholdEvents(car) {
			switch e {
			case "battery_low":
				low++
			case "battery_full":
				full++
			}
		}
	}
	return low, full
}

// TestBatteryThresholdHysteresis: alerts fire once per genuine threshold
// crossing and not on a charging-state flap or level jitter, while a real
// discharge/recharge cycle re-alerts. low=20%, high=80%, margin=3.
func TestBatteryThresholdHysteresis(t *testing.T) {
	cases := []struct {
		name      string
		levels    []int
		wantLow   int
		wantFull  int
	}{
		{
			// Sits at the full threshold while charging_state flaps: the extra
			// ticks at 90 must not re-fire (latch isn't reset by charging_state).
			name:     "sits at full, repeated ticks -> one battery_full",
			levels:   []int{70, 85, 90, 90, 90, 90},
			wantFull: 1,
		},
		{
			// Plug in at an already-low level: repeated low ticks, no re-fire.
			name:    "sits at low, repeated ticks -> one battery_low",
			levels:  []int{25, 20, 18, 18, 18},
			wantLow: 1,
		},
		{
			// Regen jitter around the low threshold: 20↔21 must not re-fire
			// (21 < 20+3 margin, no re-arm).
			name:    "low jitter 20<->21 -> one battery_low",
			levels:  []int{22, 20, 21, 20, 21, 20},
			wantLow: 1,
		},
		{
			// Jitter around the high threshold: 79↔80 must NOT re-fire.
			name:     "high jitter 79<->80 -> one battery_full",
			levels:   []int{75, 80, 79, 80, 79, 80},
			wantFull: 1,
		},
		{
			// Genuine cycle: full at 90, drive down well below the deadband
			// (re-arms at <=77), charge back to 90 -> fires AGAIN. Proves a real
			// refill still alerts.
			name:     "discharge below deadband then recharge -> two battery_full",
			levels:   []int{90, 60, 90},
			wantFull: 2,
		},
		{
			// Genuine low cycle: low at 18, charge above the deadband (>=23),
			// drain back to 18 -> fires AGAIN.
			name:    "low, recover above deadband, drop again -> two battery_low",
			levels:  []int{18, 30, 18},
			wantLow: 2,
		},
		{
			// Just inside the deadband does NOT re-arm (low+margin-1 = 22).
			name:    "recover to deadband edge minus one keeps latch -> one battery_low",
			levels:  []int{18, 22, 18},
			wantLow: 1,
		},
		{
			// Exactly at the re-arm point DOES re-arm (low+margin = 23).
			name:    "recover to exact deadband edge re-arms -> two battery_low",
			levels:  []int{18, 23, 18},
			wantLow: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manager{batteryLowPct: 20, batteryHighPct: 80}
			car := &CarState{}
			low, full := runLevels(m, car, tc.levels)
			if low != tc.wantLow || full != tc.wantFull {
				t.Fatalf("levels %v: got low=%d full=%d, want low=%d full=%d",
					tc.levels, low, full, tc.wantLow, tc.wantFull)
			}
		})
	}
}
