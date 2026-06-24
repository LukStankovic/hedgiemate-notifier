package state

import (
	"testing"
	"time"
)

func TestFirstChargingAction(t *testing.T) {
	cases := []struct {
		name         string
		value        string
		sinceConnect time.Duration
		energyAdded  float64
		want         chargingFirstAction
	}{
		{"new charge, no energy yet, after connect", "Charging", 6 * time.Hour, 0, chargingFullStart},
		{"charging at connect", "Charging", 2 * time.Second, 0, chargingTickerOnly},
		{"energy already added → ongoing", "Charging", 6 * time.Hour, 9.8, chargingTickerOnly},
		{"just past grace, no energy", "Charging", chargingStartGrace + time.Second, 0, chargingFullStart},
		{"just under grace", "Charging", chargingStartGrace - time.Second, 0, chargingTickerOnly},
		{"not charging", "Disconnected", 6 * time.Hour, 0, chargingNoAction},
		{"complete", "Complete", 6 * time.Hour, 5, chargingNoAction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstChargingAction(tc.value, tc.sinceConnect, tc.energyAdded); got != tc.want {
				t.Errorf("firstChargingAction(%q, %s, %.1f) = %d, want %d", tc.value, tc.sinceConnect, tc.energyAdded, got, tc.want)
			}
		})
	}
}

func TestSinceConnectedZero(t *testing.T) {
	m := &Manager{}
	if d := m.sinceConnected(); d < time.Minute {
		t.Errorf("never-connected sinceConnected = %s, want a long span", d)
	}
	m.MarkConnected()
	if d := m.sinceConnected(); d > time.Minute {
		t.Errorf("just-connected sinceConnected = %s, want near zero", d)
	}
}
