package mqtt

import (
	"fmt"
	"strings"
)

// TopicField identifies the type of data carried by an MQTT topic.
type TopicField string

const (
	FieldState             TopicField = "state"
	FieldChargingState     TopicField = "charging_state"
	FieldBatteryLevel      TopicField = "battery_level"
	FieldPluggedIn         TopicField = "plugged_in"
	FieldUpdateAvailable   TopicField = "update_available"
	FieldGeofence          TopicField = "geofence"
	FieldChargerPower      TopicField = "charger_power"
	FieldChargeEnergyAdded TopicField = "charge_energy_added"
	FieldTimeToFullCharge  TopicField = "time_to_full_charge"
	FieldRatedRangeKm      TopicField = "rated_battery_range_km"
	FieldUsableBattery     TopicField = "usable_battery_level"
	FieldDisplayName       TopicField = "display_name"
	FieldModel             TopicField = "model"
	FieldChargeLimitSoc    TopicField = "charge_limit_soc"
	FieldUpdateVersion     TopicField = "update_version"
	FieldSpeed             TopicField = "speed"
	FieldOutsideTemp       TopicField = "outside_temp"
	FieldOdometer          TopicField = "odometer"
	FieldActiveRoute       TopicField = "active_route"
	// FieldCenterDisplayState: Tesla display state; 7 = Sentry Mode recording.
	FieldCenterDisplayState TopicField = "center_display_state"
)

var allFields = []TopicField{
	FieldState,
	FieldChargingState,
	FieldBatteryLevel,
	FieldPluggedIn,
	FieldUpdateAvailable,
	FieldGeofence,
	FieldChargerPower,
	FieldChargeEnergyAdded,
	FieldTimeToFullCharge,
	FieldRatedRangeKm,
	FieldUsableBattery,
	FieldDisplayName,
	FieldModel,
	FieldChargeLimitSoc,
	FieldUpdateVersion,
	FieldSpeed,
	FieldOutsideTemp,
	FieldOdometer,
	FieldActiveRoute,
	FieldCenterDisplayState,
}

// knownFields is a fast lookup set for filtering incoming messages.
var knownFields = func() map[TopicField]bool {
	m := make(map[TopicField]bool, len(allFields))
	for _, f := range allFields {
		m[f] = true
	}
	return m
}()

// TopicsForCar returns the list of MQTT topics to subscribe to for a given car ID.
// The namespace parameter is the raw MQTT_NAMESPACE value (e.g. "my_ns" or "").
func TopicsForCar(carID, namespace string) []string {
	nsPart := ""
	if namespace != "" {
		nsPart = "/" + namespace
	}
	topics := make([]string, 0, len(allFields))
	for _, f := range allFields {
		topics = append(topics, fmt.Sprintf("teslamate%s/cars/%s/%s", nsPart, carID, f))
	}
	return topics
}

// ParseTopic extracts the car ID and field from a topic string.
// Supports both namespaced (teslamate/ns/cars/{id}/{field}) and
// non-namespaced (teslamate/cars/{id}/{field}) topic formats.
func ParseTopic(topic string) (carID string, field TopicField, ok bool) {
	parts := strings.Split(topic, "/")

	// Non-namespaced: teslamate/cars/{id}/{field} (4 parts)
	if len(parts) == 4 && parts[0] == "teslamate" && parts[1] == "cars" {
		return parts[2], TopicField(parts[3]), true
	}

	// Namespaced: teslamate/{namespace}/cars/{id}/{field} (5 parts)
	if len(parts) == 5 && parts[0] == "teslamate" && parts[2] == "cars" {
		return parts[3], TopicField(parts[4]), true
	}

	return "", "", false
}
