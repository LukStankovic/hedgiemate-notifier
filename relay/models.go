package relay

import "time"

type EventPayload struct {
	EventType     string     `json:"event_type"`
	CarID         string     `json:"car_id"`
	CarName       string     `json:"car_name"`
	Title         string     `json:"title"`
	Body          string     `json:"body"`
	TitleLocKey   string     `json:"title_loc_key,omitempty"`
	TitleLocArgs  []string   `json:"title_loc_args,omitempty"`
	BodyLocKey    string     `json:"body_loc_key,omitempty"`
	BodyLocArgs   []string   `json:"body_loc_args,omitempty"`
	BodyArgsTyped []TypedArg `json:"body_args_typed,omitempty"`
	Timestamp     time.Time  `json:"timestamp"`
	Data          EventData  `json:"data"`
}

// TypedArg lets the relay reformat a single body_loc_args entry using the
// recipient's per-user unit preferences. The notifier still provides a
// pre-formatted fallback in BodyLocArgs[Index] (using DISTANCE_UNIT) so
// older relays without typed-arg support keep working unchanged.
type TypedArg struct {
	Index int     `json:"index"`
	Type  string  `json:"type"`  // "distance_km" (extensible: temp_c, pressure_bar)
	Value float64 `json:"value"` // raw value in canonical unit (km / °C / bar)
}

type EventData struct {
	BatteryLevel                 int     `json:"battery_level"`
	ChargerPower                 float64 `json:"charger_power"`
	ChargeEnergyAdded            float64 `json:"charge_energy_added"`
	TimeToFullCharge             float64 `json:"time_to_full_charge"`
	RatedRangeKm                 float64 `json:"rated_range_km"`
	UsableBattery                int     `json:"usable_battery_level"`
	Geofence                     string  `json:"geofence"`
	State                        string  `json:"state"`
	ChargingState                string  `json:"charging_state"`
	PluggedIn                    bool    `json:"plugged_in"`
	ChargeLimitSoc               int     `json:"charge_limit_soc,omitempty"`
	UpdateVersion                string  `json:"update_version,omitempty"`
	PreviousGeofence             string  `json:"previous_geofence,omitempty"`
	IsMetric                     bool    `json:"is_metric"`
	Speed                        int     `json:"speed,omitempty"`
	OutsideTemp                  float64 `json:"outside_temp,omitempty"`
	Distance                     float64 `json:"distance,omitempty"`
	Duration                     float64 `json:"duration,omitempty"`
	ActiveRouteDestination       string  `json:"active_route_destination,omitempty"`
	ActiveRouteDistanceToArrival float64 `json:"active_route_distance_to_arrival,omitempty"`
	ActiveRouteMinutesToArrival  float64 `json:"active_route_minutes_to_arrival,omitempty"`
	ActiveRouteEnergyAtArrival   int     `json:"active_route_energy_at_arrival,omitempty"`
	ActiveRouteTrafficDelay      float64 `json:"active_route_traffic_delay,omitempty"`
	ActiveRouteLatitude          float64 `json:"active_route_latitude,omitempty"`
	ActiveRouteLongitude         float64 `json:"active_route_longitude,omitempty"`
}
