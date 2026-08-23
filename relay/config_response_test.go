package relay

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeApplier records what the transport handed it.
type fakeApplier struct {
	mu       sync.Mutex
	versions []int
	values   map[string]string
}

func (f *fakeApplier) Apply(version int, values map[string]string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.versions = append(f.versions, version)
	f.values = values
	return true
}

func (f *fakeApplier) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.versions)
}

func testClient(url string, applier ConfigApplier) *Client {
	c := NewClient(url, "hm_test", "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if applier != nil {
		c.SetConfigApplier(applier)
	}
	return c
}

func serve(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestConfigFromResponseIsApplied(t *testing.T) {
	srv := serve(t, `{"status":"logged","config":{"version":7,"values":{"drive.tick_s":"10"}}}`)
	applier := &fakeApplier{}

	if err := testClient(srv.URL, applier).SendEvent(EventPayload{EventType: "drive_started", CarID: "1"}); err != nil {
		t.Fatalf("send event: %v", err)
	}
	if applier.calls() != 1 {
		t.Fatalf("applier called %d times, want 1", applier.calls())
	}
	if applier.versions[0] != 7 {
		t.Fatalf("version = %d, want 7", applier.versions[0])
	}
	if applier.values["drive.tick_s"] != "10" {
		t.Fatalf("values = %v, want drive.tick_s=10", applier.values)
	}
}

// Config is a convenience; delivering the event is the job. None of these
// responses may turn a delivered event into a failed one.
func TestBadConfigNeverFailsTheEvent(t *testing.T) {
	bodies := map[string]string{
		"empty body":          ``,
		"not json":            `<html>gateway</html>`,
		"truncated json":      `{"status":"logged","config":{"vers`,
		"no config field":     `{"status":"logged"}`,
		"config is null":      `{"status":"logged","config":null}`,
		"config wrong type":   `{"status":"logged","config":"soon"}`,
		"values wrong type":   `{"config":{"version":3,"values":"nope"}}`,
		"oversized body":      `{"status":"` + strings.Repeat("x", maxResponseBytes*2) + `"}`,
		"legacy status alone": `{"status":"duplicate"}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			srv := serve(t, body)
			applier := &fakeApplier{}
			if err := testClient(srv.URL, applier).SendEvent(EventPayload{EventType: "drive_started", CarID: "1"}); err != nil {
				t.Fatalf("send event failed on %q: %v", name, err)
			}
			if applier.calls() != 0 {
				t.Fatalf("applier was called %d times for %q, want 0", applier.calls(), name)
			}
		})
	}
}

// A relay that returns config to a notifier with no applier wired up (or an
// operator who never calls SetConfigApplier) must behave exactly as before.
func TestNoApplierStillDrainsBody(t *testing.T) {
	srv := serve(t, `{"status":"logged","config":{"version":9,"values":{"drive.tick_s":"10"}}}`)
	if err := testClient(srv.URL, nil).SendEvent(EventPayload{EventType: "drive_started", CarID: "1"}); err != nil {
		t.Fatalf("send event: %v", err)
	}
}

// The version gate lives in the store, but the transport must hand every
// response over rather than deciding for itself.
func TestEveryResponseIsOffered(t *testing.T) {
	srv := serve(t, `{"config":{"version":1,"values":{"drive.tick_s":"10"}}}`)
	applier := &fakeApplier{}
	client := testClient(srv.URL, applier)

	for i := 0; i < 3; i++ {
		if err := client.SendEvent(EventPayload{EventType: "live_activity_driving_update", CarID: "1"}); err != nil {
			t.Fatalf("send event %d: %v", i, err)
		}
	}
	if applier.calls() != 3 {
		t.Fatalf("applier called %d times, want 3", applier.calls())
	}
}
