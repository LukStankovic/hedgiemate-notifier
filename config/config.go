package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	UserToken         string
	MQTTHost          string
	MQTTPort          string
	MQTTUsername      string
	MQTTPassword      string
	MQTTClientID      string
	MQTTTLS           bool
	MQTTNamespace     string
	CarIDs            []string
	RelayURL          string
	BatteryLowThresh  int
	BatteryHighThresh int
	LogLevel          string
	DistanceUnit      string
	// ServerID is the UUID of the corresponding ServerConnection inside the
	// iOS app. Optional. When set, every outgoing event is tagged with it
	// so multi-server users see notifications open on the right (server,
	// car) on iOS. Single-server installs can leave this unset.
	ServerID string
}

func Load() (*Config, error) {
	userToken := os.Getenv("HEDGIEMATE_USER_TOKEN")
	if userToken == "" {
		return nil, fmt.Errorf("HEDGIEMATE_USER_TOKEN is required")
	}

	mqttHost := os.Getenv("MQTT_HOST")
	if mqttHost == "" {
		return nil, fmt.Errorf("MQTT_HOST is required")
	}

	mqttPort := envOrDefault("MQTT_PORT", "1883")
	mqttUsername := os.Getenv("MQTT_USERNAME")
	mqttPassword := os.Getenv("MQTT_PASSWORD")
	mqttClientID := envOrDefault("MQTT_CLIENT_ID", "hedgiemate-notifier")

	mqttTLS := strings.EqualFold(os.Getenv("MQTT_TLS"), "true")
	mqttNamespace := os.Getenv("MQTT_NAMESPACE")

	// If TLS is enabled and port is still the default non-TLS port, switch to 8883
	if mqttTLS && mqttPort == "1883" {
		mqttPort = "8883"
	}

	carIDsStr := envOrDefault("CAR_IDS", "1")
	carIDs := strings.Split(carIDsStr, ",")
	for i := range carIDs {
		carIDs[i] = strings.TrimSpace(carIDs[i])
	}

	relayURL := envOrDefault("RELAY_URL", "https://push.hedgiemate.com")
	relayURL = strings.TrimRight(relayURL, "/")

	batteryLow, err := envOrDefaultInt("BATTERY_LOW_THRESHOLD", 20)
	if err != nil {
		return nil, fmt.Errorf("invalid BATTERY_LOW_THRESHOLD: %w", err)
	}

	batteryHigh, err := envOrDefaultInt("BATTERY_HIGH_THRESHOLD", 90)
	if err != nil {
		return nil, fmt.Errorf("invalid BATTERY_HIGH_THRESHOLD: %w", err)
	}

	logLevel := envOrDefault("LOG_LEVEL", "info")

	distanceUnit := strings.ToLower(envOrDefault("DISTANCE_UNIT", "km"))
	if distanceUnit != "km" && distanceUnit != "mi" {
		distanceUnit = "km"
	}

	serverID := strings.TrimSpace(os.Getenv("SERVER_ID"))

	return &Config{
		UserToken:         userToken,
		MQTTHost:          mqttHost,
		MQTTPort:          mqttPort,
		MQTTUsername:      mqttUsername,
		MQTTPassword:      mqttPassword,
		MQTTClientID:      mqttClientID,
		MQTTTLS:           mqttTLS,
		MQTTNamespace:     mqttNamespace,
		CarIDs:            carIDs,
		RelayURL:          relayURL,
		BatteryLowThresh:  batteryLow,
		BatteryHighThresh: batteryHigh,
		LogLevel:          logLevel,
		DistanceUnit:      distanceUnit,
		ServerID:          serverID,
	}, nil
}

func envOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) (int, error) {
	val := os.Getenv(key)
	if val == "" {
		return fallback, nil
	}
	return strconv.Atoi(val)
}
