package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultClient is the package-level http.Client used when a Webhook
// instance does not supply its own. Keeping a shared client lets the
// underlying transport reuse TCP connections across notifications.
var defaultClient = &http.Client{Timeout: 10 * time.Second}

// Webhook POSTs the event JSON to URL. On 5xx it retries up to 3 times with
// exponential backoff; 4xx is treated as terminal.
type Webhook struct {
	URL    string
	Client *http.Client
	// SlackCompat formats the body as `{"text": "..."}` so the URL can be
	// a Slack incoming-webhook endpoint.
	SlackCompat bool
}

// Name implements Notifier.
func (w Webhook) Name() string { return "webhook" }

// Notify implements Notifier.
func (w Webhook) Notify(ctx context.Context, evt Event) error {
	if w.URL == "" {
		return fmt.Errorf("webhook: URL is empty")
	}
	client := w.Client
	if client == nil {
		client = defaultClient
	}

	var body []byte
	var err error
	if w.SlackCompat {
		body, err = json.Marshal(map[string]string{"text": slackText(evt)})
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

// slackText formats an Event into a human-readable Slack message that
// includes the outcome, duration, any error, and a tail of the run log.
func slackText(evt Event) string {
	dur := evt.Finished.Sub(evt.Started).Round(time.Second)
	var sb strings.Builder

	switch evt.Status {
	case StatusFailed, StatusTimeout:
		fmt.Fprintf(&sb, "*[%s]* %s (exit %d, %s)", evt.Routine, strings.ToUpper(evt.Status), evt.ExitCode, dur)
	default:
		fmt.Fprintf(&sb, "*[%s]* %s (%s)", evt.Routine, evt.Status, dur)
	}

	if evt.Error != "" {
		fmt.Fprintf(&sb, "\nError: %s", evt.Error)
	}

	if evt.LogTail != "" {
		fmt.Fprintf(&sb, "\n```\n%s\n```", evt.LogTail)
	}

	return sb.String()
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
