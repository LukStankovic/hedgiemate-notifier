package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	UserToken          string
	MQTTHost           string
	MQTTPort           string
	MQTTUsername       string
	MQTTPassword       string
	MQTTClientID       string
	CarIDs             []string
	RelayURL           string
	BatteryLowThresh   int
	BatteryHighThresh  int
	LogLevel           string
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

	carIDsStr := envOrDefault("CAR_IDS", "1")
	carIDs := strings.Split(carIDsStr, ",")
	for i := range carIDs {
		carIDs[i] = strings.TrimSpace(carIDs[i])
	}

	relayURL := envOrDefault("RELAY_URL", "https://relay.hedgiemate.com")
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

	return &Config{
		UserToken:         userToken,
		MQTTHost:          mqttHost,
		MQTTPort:          mqttPort,
		MQTTUsername:       mqttUsername,
		MQTTPassword:       mqttPassword,
		MQTTClientID:       mqttClientID,
		CarIDs:            carIDs,
		RelayURL:          relayURL,
		BatteryLowThresh:  batteryLow,
		BatteryHighThresh: batteryHigh,
		LogLevel:          logLevel,
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
