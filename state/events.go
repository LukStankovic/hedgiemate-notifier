package state

import (
	"log/slog"
	"sync"
	"time"

	"github.com/hedgiemate/notifier/relay"
)

// debounceDuration coalesces rapid repeats of the SAME (carID, eventType)
// before sending — anti-flap for MQTT/TeslaMate state thrashing. The timer
// resets on each repeat, so it absorbs an oscillation of any total length as
// long as consecutive same-type edges arrive within this window.
//
// Lowered 5s→3s (2026-06): every state-transition push paid the full window as
// latency, and production data (7d/22 users) showed the flaps it guards are
// rare (~1/user/6d), confined to old notifiers without event_id, and ALL
// same-type — which the relay's independent 15s session-aware dedup catches
// regardless of this window. The dangerous start→end→start session-boundary
// oscillation (which the relay dedup would pass as a new session) was not
// observed at all. 3s keeps margin for genuine sub-3s thrashing while cutting
// 2s off every transition notification.
const debounceDuration = 3 * time.Second

// EventEmitter handles debouncing and dispatching events to the relay.
type EventEmitter struct {
	relayClient *relay.Client
	logger      *slog.Logger

	mu     sync.Mutex
	timers map[string]*time.Timer // key: carID + ":" + eventType
}

func NewEventEmitter(relayClient *relay.Client, logger *slog.Logger) *EventEmitter {
	return &EventEmitter{
		relayClient: relayClient,
		logger:      logger,
		timers:      make(map[string]*time.Timer),
	}
}

// Emit schedules an event to be sent after the debounce window.
// If the same event type fires again within the window, the timer is reset.
func (e *EventEmitter) Emit(payload relay.EventPayload) {
	key := payload.CarID + ":" + payload.EventType

	e.mu.Lock()
	defer e.mu.Unlock()

	if existing, ok := e.timers[key]; ok {
		existing.Stop()
	}

	e.logger.Debug("event debounce started", "event_type", payload.EventType, "car_id", payload.CarID)

	e.timers[key] = time.AfterFunc(debounceDuration, func() {
		e.mu.Lock()
		delete(e.timers, key)
		e.mu.Unlock()

		e.logger.Info("sending event", "event_type", payload.EventType, "car_id", payload.CarID)
		if err := e.relayClient.SendEvent(payload); err != nil {
			e.logger.Error("failed to send event", "event_type", payload.EventType, "car_id", payload.CarID, "error", err)
		}
	})
}

// EmitImmediate sends an event immediately without debouncing.
// Used for periodic live activity updates that are already rate-limited.
func (e *EventEmitter) EmitImmediate(payload relay.EventPayload) {
	e.logger.Info("sending event (immediate)", "event_type", payload.EventType, "car_id", payload.CarID)
	go func() {
		if err := e.relayClient.SendEvent(payload); err != nil {
			e.logger.Error("failed to send event", "event_type", payload.EventType, "car_id", payload.CarID, "error", err)
		}
	}()
}

// Stop cancels all pending debounce timers.
func (e *EventEmitter) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	for key, timer := range e.timers {
		timer.Stop()
		delete(e.timers, key)
	}
}
