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
		want         chargingFirstAction
	}{
		{"new charge well after connect", "Charging", 6 * time.Hour, chargingFullStart},
		{"charging at connect", "Charging", 2 * time.Second, chargingTickerOnly},
		{"just past grace", "Charging", chargingStartGrace + time.Second, chargingFullStart},
		{"just under grace", "Charging", chargingStartGrace - time.Second, chargingTickerOnly},
		{"not charging", "Disconnected", 6 * time.Hour, chargingNoAction},
		{"complete", "Complete", 6 * time.Hour, chargingNoAction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstChargingAction(tc.value, tc.sinceConnect); got != tc.want {
				t.Errorf("firstChargingAction(%q, %s) = %d, want %d", tc.value, tc.sinceConnect, got, tc.want)
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
