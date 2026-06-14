package relay

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestSendEventIdempotencyKey locks the exactly-once contract on the
// producer side: the event_id is minted once per logical event BEFORE the
// retry loop, so a retry after a server error carries the SAME id (the relay
// dedupes on it), while two distinct events always carry different ids.
func TestSendEventIdempotencyKey(t *testing.T) {
	var mu sync.Mutex
	var receivedIDs []string
	var calls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload EventPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("unmarshal request: %v", err)
		}
		mu.Lock()
		receivedIDs = append(receivedIDs, payload.EventID)
		calls++
		failFirst := calls == 1
		mu.Unlock()
		if failFirst {
			// Force one retry — the client retries 5xx.
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "hm_test", "test", slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := client.SendEvent(EventPayload{EventType: "drive_started", CarID: "1"}); err != nil {
		t.Fatalf("send event: %v", err)
	}
	if err := client.SendEvent(EventPayload{EventType: "drive_ended", CarID: "1"}); err != nil {
		t.Fatalf("send second event: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(receivedIDs) != 3 {
		t.Fatalf("expected 3 requests (1 fail + retry + second event), got %d", len(receivedIDs))
	}
	if receivedIDs[0] == "" {
		t.Fatal("event_id missing from payload")
	}
	if receivedIDs[0] != receivedIDs[1] {
		t.Fatalf("retry changed the event_id: %q vs %q — relay-side dedup would miss the replay", receivedIDs[0], receivedIDs[1])
	}
	if receivedIDs[2] == receivedIDs[0] {
		t.Fatalf("two distinct events shared one event_id %q — the second would be wrongly dropped", receivedIDs[2])
	}
	if len(receivedIDs[0]) != 36 {
		t.Fatalf("event_id %q is not a UUID", receivedIDs[0])
	}
}
