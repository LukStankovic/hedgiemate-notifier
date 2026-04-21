package state

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/hedgiemate/notifier/mqtt"
	"github.com/hedgiemate/notifier/relay"
)

// activeRoutePayload matches the JSON from TeslaMate's "active_route" MQTT topic.
type activeRoutePayload struct {
	Destination         string               `json:"destination"`
	EnergyAtArrival     int                  `json:"energy_at_arrival"`
	MilesToArrival      float64              `json:"miles_to_arrival"`
	MinutesToArrival    float64              `json:"minutes_to_arrival"`
	TrafficMinutesDelay float64              `json:"traffic_minutes_delay"`
	Location            *activeRouteLocation `json:"location"`
	Error               *string              `json:"error"`
}

type activeRouteLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

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
	DisplayName       string
	ChargeLimitSoc    int
	UpdateVersion     string
	PreviousGeofence  string // previous geofence name before update
	Speed             int
	OutsideTemp       float64
	Odometer          float64

	// Navigation / active route (from JSON "active_route" topic)
	ActiveRouteDestination      string
	ActiveRouteMilesToArrival   float64
	ActiveRouteMinutesToArrival float64
	ActiveRouteEnergyAtArrival  int
	ActiveRouteTrafficDelay     float64
	ActiveRouteLatitude         float64
	ActiveRouteLongitude        float64

	// Battery threshold tracking — fire once per session
	batteryLowFired  bool
	batteryHighFired bool

	// Live Activity tickers
	liveActivityTicker *time.Ticker
	liveActivityStop   chan struct{}
	drivingTicker      *time.Ticker
	drivingStop        chan struct{}

	// Drive tracking for distance/duration
	driveStartTime     time.Time
	driveStartOdometer float64

	// Whether we have received initial values (skip transitions on first message)
	initialized map[mqtt.TopicField]bool

	// Events deferred until enrichment data (display_name, battery_level) arrives
	pendingEvents []string
}

// Manager coordinates state machines for all monitored cars.
type Manager struct {
	mu             sync.Mutex
	cars           map[string]*CarState
	emitter        *EventEmitter
	batteryLowPct  int
	batteryHighPct int
	distanceUnit   string
	logger         *slog.Logger
	cache          *nameCache
}

func NewManager(emitter *EventEmitter, batteryLow, batteryHigh int, distanceUnit string, logger *slog.Logger) *Manager {
	cache := newNameCache()
	mgr := &Manager{
		cars:           make(map[string]*CarState),
		emitter:        emitter,
		batteryLowPct:  batteryLow,
		batteryHighPct: batteryHigh,
		distanceUnit:   distanceUnit,
		logger:         logger,
		cache:          cache,
	}

	// Restore cached display names from previous runs
	for carID, name := range cache.Load() {
		car := mgr.getOrCreate(carID)
		car.DisplayName = name
		car.initialized[mqtt.FieldDisplayName] = true
		logger.Info("loaded cached display_name", "car_id", carID, "value", name)
	}

	return mgr
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
		if firstTime {
			m.logger.Info("received battery_level", "car_id", carID, "value", level)
		}
		car.BatteryLevel = level
		if !firstTime {
			m.checkBatteryThresholds(carID, car)
		}
		m.flushPendingIfEnriched(carID, car)

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
		if !firstTime && prev != "true" && value == "true" {
			m.emit(carID, "software_update", car)
		}

	case mqtt.FieldGeofence:
		prev := car.Geofence
		car.PreviousGeofence = prev // save BEFORE update
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
	case mqtt.FieldDisplayName:
		if firstTime {
			m.logger.Info("received display_name", "car_id", carID, "value", value)
		}
		car.DisplayName = value
		// Persist to file cache so name survives notifier restarts
		names := m.cache.Load()
		names[carID] = value
		m.cache.Save(names)
		m.flushPendingIfEnriched(carID, car)
	case mqtt.FieldChargeLimitSoc:
		level, err := strconv.Atoi(value)
		if err == nil {
			car.ChargeLimitSoc = level
		}
	case mqtt.FieldUpdateVersion:
		car.UpdateVersion = value
		m.flushPendingIfEnriched(carID, car)
	case mqtt.FieldSpeed:
		s, err := strconv.Atoi(value)
		if err == nil {
			car.Speed = s
		}
	case mqtt.FieldOutsideTemp:
		f, err := strconv.ParseFloat(value, 64)
		if err == nil {
			car.OutsideTemp = f
		}
	case mqtt.FieldOdometer:
		f, err := strconv.ParseFloat(value, 64)
		if err == nil {
			car.Odometer = f
		}
	case mqtt.FieldActiveRoute:
		var route activeRoutePayload
		if err := json.Unmarshal([]byte(value), &route); err != nil {
			m.logger.Warn("invalid active_route JSON", "value", value, "error", err)
			return
		}
		if route.Error != nil {
			// No active route — clear navigation state
			car.ActiveRouteDestination = ""
			car.ActiveRouteMilesToArrival = 0
			car.ActiveRouteMinutesToArrival = 0
			car.ActiveRouteEnergyAtArrival = 0
			car.ActiveRouteTrafficDelay = 0
			car.ActiveRouteLatitude = 0
			car.ActiveRouteLongitude = 0
		} else {
			car.ActiveRouteDestination = route.Destination
			car.ActiveRouteMilesToArrival = route.MilesToArrival
			car.ActiveRouteMinutesToArrival = route.MinutesToArrival
			car.ActiveRouteEnergyAtArrival = route.EnergyAtArrival
			car.ActiveRouteTrafficDelay = route.TrafficMinutesDelay
			if route.Location != nil {
				car.ActiveRouteLatitude = route.Location.Latitude
				car.ActiveRouteLongitude = route.Location.Longitude
			}
		}
	}
}

func (m *Manager) handleStateTransition(carID string, car *CarState, prev, next string) {
	if next == "driving" {
		m.emit(carID, "drive_started", car)
		m.startDrivingLiveActivity(carID, car)
	}
	if prev == "driving" {
		m.stopDrivingLiveActivity(car)
		m.emit(carID, "drive_ended", car)
	}
	if next == "suspended" {
		m.emit(carID, "vehicle_falling_asleep", car)
	}
	if next == "asleep" || next == "offline" {
		m.emit(carID, "vehicle_asleep", car)
	}
	if (prev == "asleep" || prev == "suspended" || prev == "offline") && next != "asleep" && next != "suspended" && next != "offline" {
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

func (m *Manager) startDrivingLiveActivity(carID string, car *CarState) {
	m.stopDrivingLiveActivity(car)

	car.driveStartTime = time.Now()
	car.driveStartOdometer = car.Odometer

	// Adaptive interval: 15s at highway speed, 30s otherwise
	interval := 30 * time.Second
	if car.Speed > 80 {
		interval = 15 * time.Second
	}

	car.drivingTicker = time.NewTicker(interval)
	car.drivingStop = make(chan struct{})

	go func() {
		for {
			select {
			case <-car.drivingTicker.C:
				m.mu.Lock()
				// Recheck if still driving
				if car.State != "driving" {
					m.mu.Unlock()
					return
				}
				payload := m.buildPayload(carID, "live_activity_driving_update", car)

				// Adjust ticker interval based on current speed
				newInterval := 30 * time.Second
				if car.Speed > 80 {
					newInterval = 15 * time.Second
				}
				if newInterval != interval {
					interval = newInterval
					car.drivingTicker.Reset(interval)
				}
				m.mu.Unlock()

				m.emitter.EmitImmediate(payload)

			case <-car.drivingStop:
				return
			}
		}
	}()
}

func (m *Manager) stopDrivingLiveActivity(car *CarState) {
	if car.drivingStop != nil {
		close(car.drivingStop)
		car.drivingStop = nil
	}
	if car.drivingTicker != nil {
		car.drivingTicker.Stop()
		car.drivingTicker = nil
	}
	car.driveStartTime = time.Time{}
	car.driveStartOdometer = 0
}

func (m *Manager) isEnriched(car *CarState) bool {
	return car.initialized[mqtt.FieldDisplayName] && car.initialized[mqtt.FieldBatteryLevel]
}

// isReadyFor checks whether all data needed to build a meaningful notification
// for the given event is available. For most events baseline enrichment suffices;
// software_update additionally needs the version string so the body isn't "true".
func (m *Manager) isReadyFor(eventType string, car *CarState) bool {
	if !m.isEnriched(car) {
		return false
	}
	if eventType == "software_update" {
		return car.UpdateVersion != ""
	}
	return true
}

func (m *Manager) fmtRange(km float64) string {
	if m.distanceUnit == "mi" {
		return fmt.Sprintf("%.0f mi", km*0.621371)
	}
	return fmt.Sprintf("%.0f km", km)
}

func (m *Manager) emit(carID, eventType string, car *CarState) {
	if eventType != "live_activity_update" && !m.isReadyFor(eventType, car) {
		m.logger.Warn("data not ready, deferring event",
			"event", eventType, "car_id", carID,
			"has_display_name", car.initialized[mqtt.FieldDisplayName],
			"has_battery_level", car.initialized[mqtt.FieldBatteryLevel],
			"has_update_version", car.UpdateVersion != "")
		car.pendingEvents = append(car.pendingEvents, eventType)
		go m.deferredEmit(carID, eventType, 30*time.Second)
		return
	}
	payload := m.buildPayload(carID, eventType, car)
	m.emitter.Emit(payload)
}

// flushPendingIfEnriched emits any deferred events whose required data has
// now arrived. Events that are still not ready remain pending until either
// their fields arrive or the 30s deferredEmit safety net fires.
// Called from HandleMessage when display_name, battery_level, or update_version is received.
func (m *Manager) flushPendingIfEnriched(carID string, car *CarState) {
	if len(car.pendingEvents) == 0 {
		return
	}
	var stillPending []string
	var flushed []string
	for _, eventType := range car.pendingEvents {
		if m.isReadyFor(eventType, car) {
			payload := m.buildPayload(carID, eventType, car)
			m.emitter.Emit(payload)
			flushed = append(flushed, eventType)
		} else {
			stillPending = append(stillPending, eventType)
		}
	}
	if len(flushed) > 0 {
		m.logger.Info("data arrived, flushing pending events",
			"car_id", carID, "flushed", flushed, "still_pending", stillPending)
	}
	car.pendingEvents = stillPending
}

// deferredEmit is a safety net: if enrichment data never arrives via MQTT retained
// messages, emit the event anyway after a timeout with whatever data is available.
func (m *Manager) deferredEmit(carID, eventType string, delay time.Duration) {
	time.Sleep(delay)
	m.mu.Lock()
	defer m.mu.Unlock()

	car, ok := m.cars[carID]
	if !ok {
		return
	}

	// Check if event was already flushed by flushPendingIfEnriched
	found := false
	for i, ev := range car.pendingEvents {
		if ev == eventType {
			car.pendingEvents = append(car.pendingEvents[:i], car.pendingEvents[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return // already emitted by flush
	}

	m.logger.Warn("emitting event after timeout without full enrichment",
		"event", eventType, "car_id", carID,
		"has_display_name", car.initialized[mqtt.FieldDisplayName],
		"has_battery_level", car.initialized[mqtt.FieldBatteryLevel])
	payload := m.buildPayload(carID, eventType, car)
	m.emitter.Emit(payload)
}

func (m *Manager) buildNotification(eventType, carName string, car *CarState) (titleKey string, titleArgs []string, bodyKey string, bodyArgs []string, fallbackTitle string, fallbackBody string) {
	bat := strconv.Itoa(car.BatteryLevel) + "%"
	hasGeo := car.Geofence != ""
	hasRange := car.RatedRangeKm > 0
	hasEnergy := car.ChargeEnergyAdded > 0
	hasLimit := car.ChargeLimitSoc > 0

	rangeStr := ""
	if hasRange {
		rangeStr = m.fmtRange(car.RatedRangeKm)
	}

	switch eventType {
	case "vehicle_falling_asleep":
		titleKey = "notification.vehicle_falling_asleep.title"
		titleArgs = []string{carName}
		fallbackTitle = carName + " is falling asleep"
		switch {
		case hasGeo && hasRange:
			bodyKey = "notification.vehicle_falling_asleep.body.full"
			bodyArgs = []string{bat, car.Geofence, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s · %s", bat, car.Geofence, rangeStr)
		case hasGeo:
			bodyKey = "notification.vehicle_falling_asleep.body.geofence"
			bodyArgs = []string{bat, car.Geofence}
			fallbackBody = fmt.Sprintf("%s · %s", bat, car.Geofence)
		case hasRange:
			bodyKey = "notification.vehicle_falling_asleep.body.range"
			bodyArgs = []string{bat, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s", bat, rangeStr)
		default:
			bodyKey = "notification.vehicle_falling_asleep.body"
			bodyArgs = []string{bat}
			fallbackBody = bat
		}

	case "vehicle_asleep":
		titleKey = "notification.vehicle_asleep.title"
		titleArgs = []string{carName}
		fallbackTitle = carName + " is asleep"
		switch {
		case hasGeo && hasRange:
			bodyKey = "notification.vehicle_asleep.body.full"
			bodyArgs = []string{bat, car.Geofence, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s · %s", bat, car.Geofence, rangeStr)
		case hasGeo:
			bodyKey = "notification.vehicle_asleep.body.geofence"
			bodyArgs = []string{bat, car.Geofence}
			fallbackBody = fmt.Sprintf("%s · %s", bat, car.Geofence)
		case hasRange:
			bodyKey = "notification.vehicle_asleep.body.range"
			bodyArgs = []string{bat, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s", bat, rangeStr)
		default:
			bodyKey = "notification.vehicle_asleep.body"
			bodyArgs = []string{bat}
			fallbackBody = bat
		}

	case "vehicle_woke":
		titleKey = "notification.vehicle_woke.title"
		titleArgs = []string{carName}
		fallbackTitle = carName + " woke up"
		switch {
		case hasGeo && hasRange:
			bodyKey = "notification.vehicle_woke.body.full"
			bodyArgs = []string{bat, car.Geofence, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s · %s", bat, car.Geofence, rangeStr)
		case hasGeo:
			bodyKey = "notification.vehicle_woke.body.geofence"
			bodyArgs = []string{bat, car.Geofence}
			fallbackBody = fmt.Sprintf("%s · %s", bat, car.Geofence)
		case hasRange:
			bodyKey = "notification.vehicle_woke.body.range"
			bodyArgs = []string{bat, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s", bat, rangeStr)
		default:
			bodyKey = "notification.vehicle_woke.body"
			bodyArgs = []string{bat}
			fallbackBody = bat
		}

	case "drive_started":
		titleKey = "notification.drive_started.title"
		titleArgs = []string{carName}
		fallbackTitle = carName + " started driving"
		switch {
		case hasGeo && hasRange:
			bodyKey = "notification.drive_started.body.full"
			bodyArgs = []string{car.Geofence, bat, rangeStr}
			fallbackBody = fmt.Sprintf("From %s · %s · %s", car.Geofence, bat, rangeStr)
		case hasGeo:
			bodyKey = "notification.drive_started.body.geofence"
			bodyArgs = []string{car.Geofence, bat}
			fallbackBody = fmt.Sprintf("From %s · %s", car.Geofence, bat)
		case hasRange:
			bodyKey = "notification.drive_started.body.range"
			bodyArgs = []string{bat, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s", bat, rangeStr)
		default:
			bodyKey = "notification.drive_started.body"
			bodyArgs = []string{bat}
			fallbackBody = bat
		}

	case "drive_ended":
		titleKey = "notification.drive_ended.title"
		titleArgs = []string{carName}
		fallbackTitle = carName + " parked"
		switch {
		case hasGeo && hasRange:
			bodyKey = "notification.drive_ended.body.full"
			bodyArgs = []string{car.Geofence, bat, rangeStr}
			fallbackBody = fmt.Sprintf("At %s · %s · %s", car.Geofence, bat, rangeStr)
		case hasGeo:
			bodyKey = "notification.drive_ended.body.geofence"
			bodyArgs = []string{car.Geofence, bat}
			fallbackBody = fmt.Sprintf("At %s · %s", car.Geofence, bat)
		case hasRange:
			bodyKey = "notification.drive_ended.body.range"
			bodyArgs = []string{bat, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s", bat, rangeStr)
		default:
			bodyKey = "notification.drive_ended.body"
			bodyArgs = []string{bat}
			fallbackBody = bat
		}

	case "charging_started":
		titleKey = "notification.charging_started.title"
		titleArgs = []string{carName}
		fallbackTitle = carName + " started charging"
		power := fmt.Sprintf("%.0f", car.ChargerPower)
		switch {
		case hasGeo && hasLimit:
			limit := strconv.Itoa(car.ChargeLimitSoc) + "%"
			bodyKey = "notification.charging_started.body.full"
			bodyArgs = []string{car.Geofence, bat, limit, power}
			fallbackBody = fmt.Sprintf("%s · %s → %s · %s kW", car.Geofence, bat, limit, power)
		case hasLimit:
			limit := strconv.Itoa(car.ChargeLimitSoc) + "%"
			bodyKey = "notification.charging_started.body.limit"
			bodyArgs = []string{bat, limit, power}
			fallbackBody = fmt.Sprintf("%s → %s · %s kW", bat, limit, power)
		case hasGeo:
			bodyKey = "notification.charging_started.body.geofence"
			bodyArgs = []string{car.Geofence, bat, power}
			fallbackBody = fmt.Sprintf("%s · %s · %s kW", car.Geofence, bat, power)
		default:
			bodyKey = "notification.charging_started.body"
			bodyArgs = []string{bat, power}
			fallbackBody = fmt.Sprintf("%s · %s kW", bat, power)
		}

	case "charging_completed":
		titleKey = "notification.charging_completed.title"
		titleArgs = []string{carName}
		fallbackTitle = carName + " finished charging"
		switch {
		case hasGeo && hasEnergy && hasRange:
			energy := fmt.Sprintf("%.1f", car.ChargeEnergyAdded)
			bodyKey = "notification.charging_completed.body.full"
			bodyArgs = []string{car.Geofence, bat, energy, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s · %s kWh · %s", car.Geofence, bat, energy, rangeStr)
		case hasEnergy:
			energy := fmt.Sprintf("%.1f", car.ChargeEnergyAdded)
			bodyKey = "notification.charging_completed.body.energy"
			bodyArgs = []string{bat, energy}
			fallbackBody = fmt.Sprintf("%s · %s kWh added", bat, energy)
		case hasGeo:
			bodyKey = "notification.charging_completed.body.geofence"
			bodyArgs = []string{car.Geofence, bat}
			fallbackBody = fmt.Sprintf("%s · %s", car.Geofence, bat)
		default:
			bodyKey = "notification.charging_completed.body"
			bodyArgs = []string{bat}
			fallbackBody = bat
		}

	case "charging_interrupted":
		titleKey = "notification.charging_interrupted.title"
		titleArgs = []string{carName}
		fallbackTitle = carName + " charging stopped"
		switch {
		case hasGeo && hasEnergy && hasRange:
			energy := fmt.Sprintf("%.1f", car.ChargeEnergyAdded)
			bodyKey = "notification.charging_interrupted.body.full"
			bodyArgs = []string{car.Geofence, bat, energy, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s · %s kWh · %s", car.Geofence, bat, energy, rangeStr)
		case hasEnergy:
			energy := fmt.Sprintf("%.1f", car.ChargeEnergyAdded)
			bodyKey = "notification.charging_interrupted.body.energy"
			bodyArgs = []string{bat, energy}
			fallbackBody = fmt.Sprintf("%s · %s kWh added", bat, energy)
		case hasGeo:
			bodyKey = "notification.charging_interrupted.body.geofence"
			bodyArgs = []string{car.Geofence, bat}
			fallbackBody = fmt.Sprintf("%s · %s", car.Geofence, bat)
		default:
			bodyKey = "notification.charging_interrupted.body"
			bodyArgs = []string{bat}
			fallbackBody = bat
		}

	case "battery_low":
		titleKey = "notification.battery_low.title"
		titleArgs = []string{carName}
		fallbackTitle = carName + " — Low battery"
		switch {
		case hasGeo && hasRange:
			bodyKey = "notification.battery_low.body.full"
			bodyArgs = []string{car.Geofence, bat, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s · %s", car.Geofence, bat, rangeStr)
		case hasRange:
			bodyKey = "notification.battery_low.body.range"
			bodyArgs = []string{bat, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s", bat, rangeStr)
		case hasGeo:
			bodyKey = "notification.battery_low.body.geofence"
			bodyArgs = []string{car.Geofence, bat}
			fallbackBody = fmt.Sprintf("%s · %s", car.Geofence, bat)
		default:
			bodyKey = "notification.battery_low.body"
			bodyArgs = []string{bat}
			fallbackBody = bat
		}

	case "battery_full":
		titleKey = "notification.battery_full.title"
		titleArgs = []string{carName}
		fallbackTitle = carName + " — Battery level reached"
		switch {
		case hasGeo && hasEnergy && hasRange:
			energy := fmt.Sprintf("%.1f", car.ChargeEnergyAdded)
			bodyKey = "notification.battery_full.body.full"
			bodyArgs = []string{car.Geofence, bat, energy, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s · %s kWh · %s", car.Geofence, bat, energy, rangeStr)
		case hasEnergy:
			energy := fmt.Sprintf("%.1f", car.ChargeEnergyAdded)
			bodyKey = "notification.battery_full.body.energy"
			bodyArgs = []string{bat, energy}
			fallbackBody = fmt.Sprintf("%s · %s kWh added", bat, energy)
		case hasGeo:
			bodyKey = "notification.battery_full.body.geofence"
			bodyArgs = []string{car.Geofence, bat}
			fallbackBody = fmt.Sprintf("%s · %s", car.Geofence, bat)
		default:
			bodyKey = "notification.battery_full.body"
			bodyArgs = []string{bat}
			fallbackBody = bat
		}

	case "charger_connected":
		titleKey = "notification.charger_connected.title"
		titleArgs = []string{carName}
		fallbackTitle = carName + " plugged in"
		switch {
		case hasGeo && hasLimit:
			limit := strconv.Itoa(car.ChargeLimitSoc) + "%"
			bodyKey = "notification.charger_connected.body.full"
			bodyArgs = []string{car.Geofence, bat, limit}
			fallbackBody = fmt.Sprintf("%s · %s · Limit %s", car.Geofence, bat, limit)
		case hasLimit:
			limit := strconv.Itoa(car.ChargeLimitSoc) + "%"
			bodyKey = "notification.charger_connected.body.limit"
			bodyArgs = []string{bat, limit}
			fallbackBody = fmt.Sprintf("%s · Limit %s", bat, limit)
		case hasGeo:
			bodyKey = "notification.charger_connected.body.geofence"
			bodyArgs = []string{car.Geofence, bat}
			fallbackBody = fmt.Sprintf("%s · %s", car.Geofence, bat)
		default:
			bodyKey = "notification.charger_connected.body"
			bodyArgs = []string{bat}
			fallbackBody = bat
		}

	case "charger_disconnected":
		titleKey = "notification.charger_disconnected.title"
		titleArgs = []string{carName}
		fallbackTitle = carName + " unplugged"
		switch {
		case hasGeo && hasEnergy && hasRange:
			energy := fmt.Sprintf("%.1f", car.ChargeEnergyAdded)
			bodyKey = "notification.charger_disconnected.body.full"
			bodyArgs = []string{car.Geofence, bat, energy, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s · %s kWh · %s", car.Geofence, bat, energy, rangeStr)
		case hasEnergy:
			energy := fmt.Sprintf("%.1f", car.ChargeEnergyAdded)
			bodyKey = "notification.charger_disconnected.body.energy"
			bodyArgs = []string{bat, energy}
			fallbackBody = fmt.Sprintf("%s · %s kWh added", bat, energy)
		case hasGeo:
			bodyKey = "notification.charger_disconnected.body.geofence"
			bodyArgs = []string{car.Geofence, bat}
			fallbackBody = fmt.Sprintf("%s · %s", car.Geofence, bat)
		default:
			bodyKey = "notification.charger_disconnected.body"
			bodyArgs = []string{bat}
			fallbackBody = bat
		}

	case "software_update":
		titleKey = "notification.software_update.title"
		titleArgs = []string{carName}
		fallbackTitle = carName + " — Update available"
		version := car.UpdateVersion
		if version == "" {
			version = car.UpdateAvailable
		}
		bodyKey = "notification.software_update.body"
		bodyArgs = []string{version}
		fallbackBody = version

	case "geofence_entered":
		titleKey = "notification.geofence_entered.title"
		titleArgs = []string{carName, car.Geofence}
		fallbackTitle = fmt.Sprintf("%s arrived at %s", carName, car.Geofence)
		if hasRange {
			bodyKey = "notification.geofence_entered.body.range"
			bodyArgs = []string{bat, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s", bat, rangeStr)
		} else {
			bodyKey = "notification.geofence_entered.body"
			bodyArgs = []string{bat}
			fallbackBody = bat
		}

	case "geofence_exited":
		geo := car.PreviousGeofence
		if geo == "" {
			geo = car.Geofence
		}
		titleKey = "notification.geofence_exited.title"
		titleArgs = []string{carName, geo}
		fallbackTitle = fmt.Sprintf("%s left %s", carName, geo)
		if hasRange {
			bodyKey = "notification.geofence_exited.body.range"
			bodyArgs = []string{bat, rangeStr}
			fallbackBody = fmt.Sprintf("%s · %s", bat, rangeStr)
		} else {
			bodyKey = "notification.geofence_exited.body"
			bodyArgs = []string{bat}
			fallbackBody = bat
		}
	}

	return
}

func (m *Manager) buildPayload(carID, eventType string, car *CarState) relay.EventPayload {
	carName := car.DisplayName
	if carName == "" {
		carName = "Car " + carID
	}

	titleKey, titleArgs, bodyKey, bodyArgs, fallbackTitle, fallbackBody := m.buildNotification(eventType, carName, car)

	// Calculate driving distance and duration
	var distance float64
	var duration float64
	if !car.driveStartTime.IsZero() {
		duration = time.Since(car.driveStartTime).Minutes()
		if car.driveStartOdometer > 0 && car.Odometer > car.driveStartOdometer {
			distance = car.Odometer - car.driveStartOdometer
		}
	}

	return relay.EventPayload{
		EventType:    eventType,
		CarID:        carID,
		CarName:      carName,
		Title:        fallbackTitle,
		Body:         fallbackBody,
		TitleLocKey:  titleKey,
		TitleLocArgs: titleArgs,
		BodyLocKey:   bodyKey,
		BodyLocArgs:  bodyArgs,
		Timestamp:    time.Now().UTC(),
		Data: relay.EventData{
			BatteryLevel:                 car.BatteryLevel,
			ChargerPower:                 car.ChargerPower,
			ChargeEnergyAdded:            car.ChargeEnergyAdded,
			TimeToFullCharge:             car.TimeToFullCharge,
			RatedRangeKm:                 car.RatedRangeKm,
			UsableBattery:                car.UsableBattery,
			Geofence:                     car.Geofence,
			State:                        car.State,
			ChargingState:                car.ChargingState,
			PluggedIn:                    car.PluggedIn,
			ChargeLimitSoc:               car.ChargeLimitSoc,
			UpdateVersion:                car.UpdateVersion,
			PreviousGeofence:             car.PreviousGeofence,
			IsMetric:                     m.distanceUnit == "km",
			Speed:                        car.Speed,
			OutsideTemp:                  car.OutsideTemp,
			Distance:                     distance,
			Duration:                     duration,
			ActiveRouteDestination:       car.ActiveRouteDestination,
			ActiveRouteDistanceToArrival: car.ActiveRouteMilesToArrival, // TeslaMate sends km despite field name
			ActiveRouteMinutesToArrival:  car.ActiveRouteMinutesToArrival,
			ActiveRouteEnergyAtArrival:   car.ActiveRouteEnergyAtArrival,
			ActiveRouteTrafficDelay:      car.ActiveRouteTrafficDelay,
			// Location stored in CarState but not sent to relay (privacy)
			// ActiveRouteLatitude / ActiveRouteLongitude ready when needed
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
		m.stopDrivingLiveActivity(car)
	}
}
