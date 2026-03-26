package relay

import (
	"bytes"
	"crypto/hmac"
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
	http      *http.Client
	logger    *slog.Logger
}

func NewClient(relayURL, userToken string, logger *slog.Logger) *Client {
	return &Client{
		relayURL:  relayURL,
		userToken: userToken,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

func (c *Client) SendEvent(payload EventPayload) error {
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

func (c *Client) sign(body []byte, timestamp string) string {
	mac := hmac.New(sha256.New, []byte(c.userToken))
	mac.Write(body)
	mac.Write([]byte(timestamp))
	return hex.EncodeToString(mac.Sum(nil))
}
