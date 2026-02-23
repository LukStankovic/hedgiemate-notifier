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
}

// TopicsForCar returns the list of MQTT topics to subscribe to for a given car ID.
func TopicsForCar(carID string) []string {
	topics := make([]string, 0, len(allFields))
	for _, f := range allFields {
		topics = append(topics, fmt.Sprintf("teslamate/cars/%s/%s", carID, f))
	}
	return topics
}

// ParseTopic extracts the car ID and field from a topic string.
// Returns empty strings if the topic does not match the expected pattern.
func ParseTopic(topic string) (carID string, field TopicField, ok bool) {
	// Expected format: teslamate/cars/{id}/{field}
	parts := strings.SplitN(topic, "/", 4)
	if len(parts) != 4 || parts[0] != "teslamate" || parts[1] != "cars" {
		return "", "", false
	}
	return parts[2], TopicField(parts[3]), true
}
