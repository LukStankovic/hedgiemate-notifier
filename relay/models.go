package relay

import "time"

type EventPayload struct {
	EventType string    `json:"event_type"`
	CarID     string    `json:"car_id"`
	Timestamp time.Time `json:"timestamp"`
	Data      EventData `json:"data"`
}

type EventData struct {
	BatteryLevel      int     `json:"battery_level"`
	ChargerPower      float64 `json:"charger_power"`
	ChargeEnergyAdded float64 `json:"charge_energy_added"`
	TimeToFullCharge  float64 `json:"time_to_full_charge"`
	RatedRangeKm      float64 `json:"rated_range_km"`
	UsableBattery     int     `json:"usable_battery_level"`
	Geofence          string  `json:"geofence"`
	State             string  `json:"state"`
	ChargingState     string  `json:"charging_state"`
	PluggedIn         bool    `json:"plugged_in"`
}
