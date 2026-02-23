package state

import (
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/hedgiemate/notifier/mqtt"
	"github.com/hedgiemate/notifier/relay"
)

// CarState holds the current known state of a single car.
type CarState struct {
	State             string
	ChargingState     string
	BatteryLevel      int
	PluggedIn         bool
	Geofence          string
	UpdateAvailable   string
	ChargerPower      float64
	ChargeEnergyAdded float64
	TimeToFullCharge  float64
	RatedRangeKm      float64
	UsableBattery     int

	// Battery threshold tracking — fire once per session
	batteryLowFired  bool
	batteryHighFired bool

	// Live Activity ticker
	liveActivityTicker *time.Ticker
	liveActivityStop   chan struct{}

	// Whether we have received initial values (skip transitions on first message)
	initialized map[mqtt.TopicField]bool
}

// Manager coordinates state machines for all monitored cars.
type Manager struct {
	mu              sync.Mutex
	cars            map[string]*CarState
	emitter         *EventEmitter
	batteryLowPct   int
	batteryHighPct  int
	logger          *slog.Logger
}

func NewManager(emitter *EventEmitter, batteryLow, batteryHigh int, logger *slog.Logger) *Manager {
	return &Manager{
		cars:           make(map[string]*CarState),
		emitter:        emitter,
		batteryLowPct:  batteryLow,
		batteryHighPct: batteryHigh,
		logger:         logger,
	}
}

// HandleMessage processes an MQTT message and emits events on state transitions.
func (m *Manager) HandleMessage(carID string, field mqtt.TopicField, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	car := m.getOrCreate(carID)

	// Check if this field has been seen before (first value = initialization, not transition)
	firstTime := !car.initialized[field]
	car.initialized[field] = true

	switch field {
	case mqtt.FieldState:
		prev := car.State
		car.State = value
		if !firstTime && prev != value {
			m.handleStateTransition(carID, car, prev, value)
		}

	case mqtt.FieldChargingState:
		prev := car.ChargingState
		car.ChargingState = value
		if !firstTime && prev != value {
			m.handleChargingStateTransition(carID, car, prev, value)
		}
		// Reset battery threshold flags when charging state changes
		if prev != value {
			car.batteryLowFired = false
			car.batteryHighFired = false
		}

	case mqtt.FieldBatteryLevel:
		level, err := strconv.Atoi(value)
		if err != nil {
			m.logger.Warn("invalid battery_level", "value", value, "error", err)
			return
		}
		car.BatteryLevel = level
		if !firstTime {
			m.checkBatteryThresholds(carID, car)
		}

	case mqtt.FieldPluggedIn:
		prev := car.PluggedIn
		car.PluggedIn = value == "true"
		if !firstTime && prev != car.PluggedIn {
			if car.PluggedIn {
				m.emit(carID, "charger_connected", car)
			} else {
				m.emit(carID, "charger_disconnected", car)
			}
		}

	case mqtt.FieldUpdateAvailable:
		prev := car.UpdateAvailable
		car.UpdateAvailable = value
		if !firstTime && prev == "" && value != "" {
			m.emit(carID, "software_update", car)
		}

	case mqtt.FieldGeofence:
		prev := car.Geofence
		car.Geofence = value
		if !firstTime && prev != value {
			if prev == "" && value != "" {
				m.emit(carID, "geofence_entered", car)
			} else if prev != "" && (value == "" || value != prev) {
				m.emit(carID, "geofence_exited", car)
				if value != "" {
					m.emit(carID, "geofence_entered", car)
				}
			}
		}

	// Enrichment fields — update state only, no transitions
	case mqtt.FieldChargerPower:
		f, err := strconv.ParseFloat(value, 64)
		if err == nil {
			car.ChargerPower = f
		}
	case mqtt.FieldChargeEnergyAdded:
		f, err := strconv.ParseFloat(value, 64)
		if err == nil {
			car.ChargeEnergyAdded = f
		}
	case mqtt.FieldTimeToFullCharge:
		f, err := strconv.ParseFloat(value, 64)
		if err == nil {
			car.TimeToFullCharge = f
		}
	case mqtt.FieldRatedRangeKm:
		f, err := strconv.ParseFloat(value, 64)
		if err == nil {
			car.RatedRangeKm = f
		}
	case mqtt.FieldUsableBattery:
		level, err := strconv.Atoi(value)
		if err == nil {
			car.UsableBattery = level
		}
	}
}

func (m *Manager) handleStateTransition(carID string, car *CarState, prev, next string) {
	if next == "driving" {
		m.emit(carID, "drive_started", car)
	}
	if prev == "driving" {
		m.emit(carID, "drive_ended", car)
	}
	if next == "asleep" {
		m.emit(carID, "vehicle_asleep", car)
	}
	if prev == "asleep" {
		m.emit(carID, "vehicle_woke", car)
	}
}

func (m *Manager) handleChargingStateTransition(carID string, car *CarState, prev, next string) {
	if next == "Charging" {
		m.emit(carID, "charging_started", car)
		m.startLiveActivity(carID, car)
	}
	if next == "Complete" {
		m.stopLiveActivity(car)
		m.emit(carID, "charging_completed", car)
	}
	if prev == "Charging" && next != "Complete" && next != "Charging" {
		m.stopLiveActivity(car)
		m.emit(carID, "charging_interrupted", car)
	}
}

func (m *Manager) checkBatteryThresholds(carID string, car *CarState) {
	if car.BatteryLevel <= m.batteryLowPct && !car.batteryLowFired {
		car.batteryLowFired = true
		m.emit(carID, "battery_low", car)
	}
	if car.BatteryLevel >= m.batteryHighPct && !car.batteryHighFired {
		car.batteryHighFired = true
		m.emit(carID, "battery_full", car)
	}
}

func (m *Manager) startLiveActivity(carID string, car *CarState) {
	m.stopLiveActivity(car)

	interval := 60 * time.Second
	if car.ChargerPower > 11.0 {
		interval = 30 * time.Second
	}

	car.liveActivityTicker = time.NewTicker(interval)
	car.liveActivityStop = make(chan struct{})

	go func() {
		for {
			select {
			case <-car.liveActivityTicker.C:
				m.mu.Lock()
				// Recheck if still charging
				if car.ChargingState != "Charging" {
					m.mu.Unlock()
					return
				}
				payload := m.buildPayload(carID, "live_activity_update", car)

				// Adjust ticker interval based on current power
				newInterval := 60 * time.Second
				if car.ChargerPower > 11.0 {
					newInterval = 30 * time.Second
				}
				if newInterval != interval {
					interval = newInterval
					car.liveActivityTicker.Reset(interval)
				}
				m.mu.Unlock()

				m.emitter.EmitImmediate(payload)

			case <-car.liveActivityStop:
				return
			}
		}
	}()
}

func (m *Manager) stopLiveActivity(car *CarState) {
	if car.liveActivityStop != nil {
		close(car.liveActivityStop)
		car.liveActivityStop = nil
	}
	if car.liveActivityTicker != nil {
		car.liveActivityTicker.Stop()
		car.liveActivityTicker = nil
	}
}

func (m *Manager) emit(carID, eventType string, car *CarState) {
	payload := m.buildPayload(carID, eventType, car)
	m.emitter.Emit(payload)
}

func (m *Manager) buildPayload(carID, eventType string, car *CarState) relay.EventPayload {
	return relay.EventPayload{
		EventType: eventType,
		CarID:     carID,
		Timestamp: time.Now().UTC(),
		Data: relay.EventData{
			BatteryLevel:      car.BatteryLevel,
			ChargerPower:      car.ChargerPower,
			ChargeEnergyAdded: car.ChargeEnergyAdded,
			TimeToFullCharge:  car.TimeToFullCharge,
			RatedRangeKm:      car.RatedRangeKm,
			UsableBattery:     car.UsableBattery,
			Geofence:          car.Geofence,
			State:             car.State,
			ChargingState:     car.ChargingState,
			PluggedIn:         car.PluggedIn,
		},
	}
}

func (m *Manager) getOrCreate(carID string) *CarState {
	car, ok := m.cars[carID]
	if !ok {
		car = &CarState{
			initialized: make(map[mqtt.TopicField]bool),
		}
		m.cars[carID] = car
	}
	return car
}

// Stop cleans up all live activity tickers.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, car := range m.cars {
		m.stopLiveActivity(car)
	}
}
