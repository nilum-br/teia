package api

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// openStream starts a stream request and fails the test if it is refused.
func openStream(t *testing.T, url string, header map[string]string) (*http.Response, *bufio.Reader) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url+"/api/v1/stream", nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("stream status = %d", resp.StatusCode)
	}
	return resp, bufio.NewReader(resp.Body)
}

// nextEvent reads one SSE event, or fails after a few seconds.
func nextEvent(t *testing.T, r *bufio.Reader) (event, data string) {
	t.Helper()
	type result struct{ event, data string }
	done := make(chan result, 1)
	go func() {
		var res result
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				done <- res
				return
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case strings.HasPrefix(line, "event: "):
				res.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				res.data = strings.TrimPrefix(line, "data: ")
			case line == "" && res.event != "":
				done <- res
				return
			}
		}
	}()
	select {
	case res := <-done:
		return res.event, res.data
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
		return "", ""
	}
}

func TestStreamSendsSnapshots(t *testing.T) {
	eng := newFakeEngine()
	ts := httptest.NewServer(Handler(eng, Options{}))
	defer ts.Close()

	resp, r := openStream(t, ts.URL, nil)
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content type = %q", ct)
	}
	if event, data := nextEvent(t, r); event != "snapshot" || data != string(eng.SnapshotJSON()) {
		t.Errorf("first event = %s %s", event, data)
	}
	eng.updates <- []byte(`{"round":2}`)
	if event, data := nextEvent(t, r); event != "snapshot" || data != `{"round":2}` {
		t.Errorf("update = %s %s", event, data)
	}
}

func TestStreamCap(t *testing.T) {
	ts := httptest.NewServer(Handler(newFakeEngine(), Options{MaxStreams: 1}))
	defer ts.Close()

	first, _ := openStream(t, ts.URL, nil)
	resp, err := http.Get(ts.URL + "/api/v1/stream")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Errorf("over the cap: status %d, Retry-After %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}

	// Closing the first stream frees its slot.
	first.Body.Close()
	for deadline := time.Now().Add(5 * time.Second); ; {
		resp, err := http.Get(ts.URL + "/api/v1/stream")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the slot was never freed: %d", resp.StatusCode)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The server cancels the base context on shutdown; streams must end then,
// or Shutdown waits for them until its deadline.
func TestStreamEndsWithItsContext(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ts := httptest.NewUnstartedServer(Handler(newFakeEngine(), Options{}))
	ts.Config.BaseContext = func(net.Listener) context.Context { return base }
	ts.Start()
	defer ts.Close()

	resp, r := openStream(t, ts.URL, nil)
	defer resp.Body.Close()
	nextEvent(t, r)

	cancel()
	ended := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, r)
		ended <- err
	}()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end when its context was cancelled")
	}
}

func TestStreamCORSAndAuth(t *testing.T) {
	const grafana = "https://grafana.example.com"
	ts := httptest.NewServer(Handler(newFakeEngine(), Options{Token: "s3cret", AllowedOrigins: []string{grafana}}))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/stream")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("stream without a token: status %d", resp.StatusCode)
	}

	stream, r := openStream(t, ts.URL, map[string]string{"Authorization": "Bearer s3cret", "Origin": grafana})
	defer stream.Body.Close()
	if got := stream.Header.Get("Access-Control-Allow-Origin"); got != grafana {
		t.Errorf("allow origin = %q", got)
	}
	if event, _ := nextEvent(t, r); event != "snapshot" {
		t.Errorf("event = %q", event)
	}
}
