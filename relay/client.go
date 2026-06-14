package relay

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

type Client struct {
	relayURL  string
	userToken string
	version   string
	http      *http.Client
	logger    *slog.Logger
}

func NewClient(relayURL, userToken, version string, logger *slog.Logger) *Client {
	return &Client{
		relayURL:  relayURL,
		userToken: userToken,
		version:   version,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

func (c *Client) SendEvent(payload EventPayload) error {
	// Idempotency key: minted ONCE per logical event, before the retry loop
	// below, so every retry of this event carries the same event_id and the
	// relay can drop replays with zero side effects. Without it, a response
	// lost after the relay had already processed the POST made the retry a
	// second drive_started — which respawned the Live Activity and put twin
	// LAs on the lock screen (2026-06-11 incident).
	if payload.EventID == "" {
		payload.EventID = newEventID()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := c.sign(body, timestamp)

	var lastErr error
	backoff := time.Second

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			c.logger.Warn("retrying relay request",
				"attempt", attempt+1,
				"backoff", backoff,
				"event_type", payload.EventType,
			)
			time.Sleep(backoff)
			backoff *= 2
		}

		req, err := http.NewRequest("POST", c.relayURL+"/v1/events", bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-HedgieMate-Signature", signature)
		req.Header.Set("X-HedgieMate-Timestamp", timestamp)
		req.Header.Set("X-HedgieMate-User-Token", c.userToken)
		// Reported so the relay can flag outdated notifiers (in-app banner only).
		// Harmless to older relays — an unknown header is ignored.
		req.Header.Set("X-HedgieMate-Notifier-Version", c.version)

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.logger.Debug("event sent successfully",
				"event_type", payload.EventType,
				"car_id", payload.CarID,
				"status", resp.StatusCode,
			)
			return nil
		}

		lastErr = fmt.Errorf("unexpected status code: %d", resp.StatusCode)

		// Don't retry on client errors (except 429)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
			return lastErr
		}
	}

	return fmt.Errorf("all retries exhausted: %w", lastErr)
}

// newEventID returns a random RFC 4122 v4 UUID. crypto/rand cannot fail on
// supported platforms; on the theoretical error path we fall back to a
// timestamp id instead of aborting the send — a missing/weak event_id only
// downgrades dedup to the relay's 15s heuristic, it never loses the event.
func newEventID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("t-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (c *Client) sign(body []byte, timestamp string) string {
	mac := hmac.New(sha256.New, []byte(c.userToken))
	mac.Write(body)
	mac.Write([]byte(timestamp))
	return hex.EncodeToString(mac.Sum(nil))
}
