// Package notify sends what changed between discovery rounds to the tools
// teams already watch: any webhook (JSON) or Slack incoming webhooks.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nilum-br/teia/internal/model"
)

// Slack limits: a message stays readable, and short of Slack's own cap,
// however many changes a round has.
const (
	slackMaxLines    = 30
	slackMaxBytes    = 3500
	slackMaxLineSize = 300
)

// drainLimit bounds how much of an answer is read so the connection can be
// reused.
const drainLimit = 64 << 10

// Webhook posts changes to one URL.
type Webhook struct {
	// Name identifies the webhook in errors and logs, e.g.
	// "notify.webhooks[0]". The URL itself never appears there: for Slack
	// it is the secret.
	Name string
	URL  string
	// Format is "json" (default) or "slack".
	Format string
	// Types limits which changes are sent; empty sends all.
	Types   []model.ChangeType
	Headers map[string]string
	client  *http.Client
}

func NewWebhook(name, url, format string, types []model.ChangeType, headers map[string]string) *Webhook {
	return &Webhook{Name: name, URL: url, Format: format, Types: types, Headers: headers, client: &http.Client{Timeout: 10 * time.Second}}
}

// String names the webhook by its name, scheme and host only.
func (w *Webhook) String() string {
	return fmt.Sprintf("%s (%s)", w.Name, Redact(w.URL))
}

// Redact keeps the scheme and host of a URL, dropping the path, query and
// credentials that may carry a secret.
func Redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "invalid URL"
	}
	return u.Scheme + "://" + u.Host
}

// Notify sends the changes the webhook is interested in, if any.
func (w *Webhook) Notify(ctx context.Context, changes []model.Change) error {
	var picked []model.Change
	for _, c := range changes {
		if w.wants(c.Type) {
			picked = append(picked, c)
		}
	}
	if len(picked) == 0 {
		return nil
	}
	var body any = map[string]any{"source": "teia", "changes": picked}
	if w.Format == "slack" {
		body = map[string]string{"text": slackText(picked)}
	}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%s: encoding: %w", w, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s: invalid URL", w) // the parse error would repeat the URL
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range w.Headers {
		req.Header.Set(k, v)
	}
	client := w.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		// *url.Error repeats the full URL; keep only what went wrong.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return fmt.Errorf("%s: %w", w, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainLimit))
		resp.Body.Close()
	}()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s answered HTTP %d", w, resp.StatusCode)
	}
	return nil
}

func (w *Webhook) wants(t model.ChangeType) bool {
	if len(w.Types) == 0 {
		return true
	}
	for _, x := range w.Types {
		if x == t {
			return true
		}
	}
	return false
}

var slackIcon = map[model.ChangeType]string{
	model.ChangeEdgeAdded:       ":new:",
	model.ChangeEdgeRemoved:     ":heavy_minus_sign:",
	model.ChangeEdgeState:       ":arrows_counterclockwise:",
	model.ChangeEdgeHealth:      ":large_yellow_circle:",
	model.ChangeNodeAdded:       ":new:",
	model.ChangeNodeRemoved:     ":heavy_minus_sign:",
	model.ChangeNodeHealth:      ":large_yellow_circle:",
	model.ChangeNodeVisibility:  ":eye:",
	model.ChangeFindingOpened:   ":mag:",
	model.ChangeFindingResolved: ":white_check_mark:",
}

// slackEscaper escapes Slack's control characters: names come from
// telemetry, and "<!channel>" or "<https://x|click>" in one must stay text.
var slackEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func slackText(changes []model.Change) string {
	var b strings.Builder
	fmt.Fprintf(&b, "*teia* · %d change(s)\n", len(changes))
	for i, c := range changes {
		icon := slackIcon[c.Type]
		switch c.To {
		case string(model.HealthFailing):
			icon = ":red_circle:"
		case string(model.HealthHealthy):
			icon = ":large_green_circle:"
		}
		message := c.Message
		if c.Cluster != "" {
			message = "[" + c.Cluster + "] " + message
		}
		line := icon + " " + slackEscaper.Replace(truncate(message, slackMaxLineSize)) + "\n"
		if i == slackMaxLines || b.Len()+len(line) > slackMaxBytes {
			fmt.Fprintf(&b, "...and %d more\n", len(changes)-i)
			break
		}
		b.WriteString(line)
	}
	return b.String()
}

// truncate cuts s to at most n bytes on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
