package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Webhook POSTs the event JSON to URL. On 5xx it retries up to 3 times with
// exponential backoff; 4xx is treated as terminal.
type Webhook struct {
	URL    string
	Client *http.Client
	// SlackCompat formats the body as `{"text": "..."}` so the URL can be
	// a Slack incoming-webhook endpoint.
	SlackCompat bool
}

func (w Webhook) Name() string { return "webhook" }

func (w Webhook) Notify(ctx context.Context, evt Event) error {
	if w.URL == "" {
		return fmt.Errorf("webhook: URL is empty")
	}
	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	var body []byte
	var err error
	if w.SlackCompat {
		body, err = json.Marshal(map[string]string{
			"text": fmt.Sprintf("routine=%s status=%s exit=%d %s",
				evt.Routine, evt.Status, evt.ExitCode, evt.Error),
		})
	} else {
		body, err = json.Marshal(evt)
	}
	if err != nil {
		return err
	}

	backoff := 500 * time.Millisecond
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			if attempt == 2 {
				return err
			}
			if err := sleepCtx(ctx, backoff); err != nil {
				return err
			}
			backoff *= 2
			continue
		}
		// drain & close
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		switch {
		case resp.StatusCode < 400:
			return nil
		case resp.StatusCode >= 500:
			if attempt == 2 {
				return fmt.Errorf("webhook %s: %d after retries", w.URL, resp.StatusCode)
			}
			if err := sleepCtx(ctx, backoff); err != nil {
				return err
			}
			backoff *= 2
		default:
			return fmt.Errorf("webhook %s: %d", w.URL, resp.StatusCode)
		}
	}
	return nil
}

// sleepCtx blocks for d, returning ctx.Err() promptly on cancellation.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
