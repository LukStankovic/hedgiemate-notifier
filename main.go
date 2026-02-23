package main

import (
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/hedgiemate/notifier/config"
	"github.com/hedgiemate/notifier/mqtt"
	"github.com/hedgiemate/notifier/relay"
	"github.com/hedgiemate/notifier/state"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Set up structured logger
	logLevel := parseLogLevel(cfg.LogLevel)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	logger.Info("hedgiemate-notifier starting",
		"car_ids", cfg.CarIDs,
		"mqtt_host", cfg.MQTTHost,
		"mqtt_port", cfg.MQTTPort,
		"relay_url", cfg.RelayURL,
		"battery_low_threshold", cfg.BatteryLowThresh,
		"battery_high_threshold", cfg.BatteryHighThresh,
	)

	// Initialize relay client
	relayClient := relay.NewClient(cfg.RelayURL, cfg.UserToken, logger)

	// Initialize event emitter with debounce
	emitter := state.NewEventEmitter(relayClient, logger)

	// Initialize state manager
	stateMgr := state.NewManager(emitter, cfg.BatteryLowThresh, cfg.BatteryHighThresh, logger)

	// Initialize MQTT client
	mqttClient := mqtt.NewClient(
		cfg.MQTTHost,
		cfg.MQTTPort,
		cfg.MQTTUsername,
		cfg.MQTTPassword,
		cfg.MQTTClientID,
		cfg.CarIDs,
		stateMgr.HandleMessage,
		logger,
	)

	// Connect to MQTT
	if err := mqttClient.Connect(); err != nil {
		logger.Error("failed to connect to MQTT broker", "error", err)
		os.Exit(1)
	}

	logger.Info("hedgiemate-notifier running, waiting for MQTT messages")

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan

	logger.Info("received shutdown signal", "signal", sig.String())

	// Graceful shutdown
	stateMgr.Stop()
	emitter.Stop()
	mqttClient.Disconnect()

	logger.Info("hedgiemate-notifier stopped")
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
