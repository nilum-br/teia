// Package notify sends what changed between discovery rounds to the tools
// teams already watch: any webhook (JSON) or Slack incoming webhooks.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

// Webhook posts changes to one URL.
type Webhook struct {
	URL string
	// Format is "json" (default) or "slack".
	Format string
	// Types limits which changes are sent; empty sends all.
	Types   []model.ChangeType
	Headers map[string]string
	client  *http.Client
}

func NewWebhook(url, format string, types []model.ChangeType, headers map[string]string) *Webhook {
	return &Webhook{URL: url, Format: format, Types: types, Headers: headers, client: &http.Client{Timeout: 10 * time.Second}}
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
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range w.Headers {
		req.Header.Set(k, v)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook %s answered HTTP %d", w.URL, resp.StatusCode)
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

func slackText(changes []model.Change) string {
	var b strings.Builder
	fmt.Fprintf(&b, "*teia* · %d change(s)\n", len(changes))
	for _, c := range changes {
		icon := slackIcon[c.Type]
		switch c.To {
		case string(model.HealthFailing):
			icon = ":red_circle:"
		case string(model.HealthHealthy):
			icon = ":large_green_circle:"
		}
		fmt.Fprintf(&b, "%s %s\n", icon, c.Message)
	}
	return b.String()
}
