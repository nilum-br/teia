package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nilum-br/teia/internal/model"
)

func change(t model.ChangeType, msg string) model.Change {
	return model.Change{Type: t, Subject: "x", Message: msg}
}

func TestNotifyJSON(t *testing.T) {
	var got struct {
		Source  string
		Changes []model.Change
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Team") != "platform" {
			t.Errorf("custom header missing")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
	}))
	defer ts.Close()

	w := NewWebhook("notify.webhooks[0]", ts.URL, "json", []model.ChangeType{model.ChangeEdgeAdded}, map[string]string{"X-Team": "platform"})
	err := w.Notify(context.Background(), []model.Change{change(model.ChangeEdgeAdded, "new"), change(model.ChangeNodeAdded, "skipped")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "teia" || len(got.Changes) != 1 || got.Changes[0].Message != "new" {
		t.Errorf("payload = %+v", got)
	}
}

func TestSlackText(t *testing.T) {
	many := make([]model.Change, 100)
	for i := range many {
		many[i] = change(model.ChangeEdgeAdded, fmt.Sprintf("change %d", i))
	}
	long := make([]model.Change, 40)
	for i := range long {
		long[i] = change(model.ChangeEdgeAdded, strings.Repeat("é", 400))
	}

	tests := []struct {
		name     string
		changes  []model.Change
		contains []string
		absent   []string
		lines    int
	}{
		{
			name:     "control characters are escaped",
			changes:  []model.Change{change(model.ChangeEdgeAdded, "<!channel> & <https://evil.example|click> a->b")},
			contains: []string{"&lt;!channel&gt; &amp; &lt;https://evil.example|click&gt; a-&gt;b"},
			absent:   []string{"<!channel>"},
			lines:    2,
		},
		{
			name:     "lines are capped",
			changes:  many,
			contains: []string{"*teia* · 100 change(s)", "change 29", "...and 70 more"},
			absent:   []string{"change 30\n"},
			lines:    1 + slackMaxLines + 1,
		},
		{
			name:     "size is capped",
			changes:  long,
			contains: []string{"more\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := slackText(tt.changes)
			for _, want := range tt.contains {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q in:\n%s", want, text)
				}
			}
			for _, bad := range tt.absent {
				if strings.Contains(text, bad) {
					t.Errorf("unexpected %q in:\n%s", bad, text)
				}
			}
			if tt.lines > 0 && strings.Count(text, "\n") != tt.lines {
				t.Errorf("lines = %d, want %d", strings.Count(text, "\n"), tt.lines)
			}
			if len(text) > slackMaxBytes+100 {
				t.Errorf("text is %d bytes", len(text))
			}
			if !strings.HasSuffix(text, "\n") || strings.ContainsRune(text, '�') {
				t.Errorf("text was cut badly: %q", text[len(text)-20:])
			}
		})
	}
}

func TestNotifyErrorsDoNotLeakTheURL(t *testing.T) {
	const secret = "/services/T000/B000/XXXXSECRET"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer ts.Close()
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	for _, base := range []string{ts.URL, closed.URL} {
		w := NewWebhook("notify.webhooks[1]", base+secret+"?token=abc", "slack", nil, nil)
		err := w.Notify(context.Background(), []model.Change{change(model.ChangeEdgeAdded, "x")})
		if err == nil {
			t.Fatalf("%s: want an error", base)
		}
		msg := err.Error()
		if strings.Contains(msg, "SECRET") || strings.Contains(msg, "token=abc") {
			t.Errorf("error leaks the URL: %s", msg)
		}
		if !strings.Contains(msg, "notify.webhooks[1]") || !strings.Contains(msg, strings.TrimPrefix(base, "http://")) {
			t.Errorf("error should name the webhook and its host: %s", msg)
		}
	}
}

func TestNotifyDrainsAnswers(t *testing.T) {
	var conns atomic.Int32
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(strings.Repeat("ok", 4096)))
	}))
	ts.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	ts.Start()
	defer ts.Close()

	w := NewWebhook("notify.webhooks[0]", ts.URL, "json", nil, nil)
	for range 3 {
		if err := w.Notify(context.Background(), []model.Change{change(model.ChangeEdgeAdded, "x")}); err != nil {
			t.Fatal(err)
		}
	}
	if n := conns.Load(); n != 1 {
		t.Errorf("%d connections for 3 notifications; answers are not drained", n)
	}
}

func TestRedact(t *testing.T) {
	for in, want := range map[string]string{
		"https://hooks.slack.com/services/T/B/SECRET": "https://hooks.slack.com",
		"https://user:pass@example.com:8443/x?y=z":    "https://example.com:8443",
		"not a url": "invalid URL",
	} {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlackNamesTheCluster(t *testing.T) {
	text := slackText([]model.Change{
		{Type: model.ChangeEdgeHealth, Subject: "app-a->app-b", To: "failing", Message: "app-a->app-b is failing", Cluster: "east"},
		{Type: model.ChangeEdgeHealth, Subject: "app-a->app-b", To: "healthy", Message: "app-a->app-b is healthy"},
	})
	if !strings.Contains(text, "[east] app-a-&gt;app-b is failing") || !strings.Contains(text, " app-a-&gt;app-b is healthy") || strings.Contains(text, "[] ") {
		t.Fatalf("slack text:\n%s", text)
	}
}
