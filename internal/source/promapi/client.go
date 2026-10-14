// Package promapi is a minimal client for instant queries against
// Prometheus-compatible APIs: Prometheus, Thanos, Mimir, VictoriaMetrics, and
// Loki metric queries (LogQL answers in the same vector format).
package promapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Sample is one series of an instant vector.
type Sample struct {
	Metric map[string]string
	Value  float64
}

// DeniedError means the API refused the credentials teia used.
type DeniedError struct{ Status int }

func (e *DeniedError) Error() string { return fmt.Sprintf("HTTP %d", e.Status) }

// Client queries one endpoint.
type Client struct {
	BaseURL string
	// Path is the instant query path: /api/v1/query for Prometheus,
	// /loki/api/v1/query for Loki.
	Path    string
	Headers map[string]string
	HTTP    *http.Client
}

// New returns a client with sane defaults.
func New(baseURL, path string, headers map[string]string) *Client {
	return &Client{BaseURL: baseURL, Path: path, Headers: headers, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// Query runs an instant query and returns its vector.
func (c *Client) Query(ctx context.Context, query string) ([]Sample, error) {
	u := strings.TrimRight(c.BaseURL, "/") + c.Path + "?query=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &DeniedError{Status: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var parsed struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  [2]any            `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decoding answer: %w", err)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("query failed: %s", parsed.Error)
	}
	if parsed.Data.ResultType != "" && parsed.Data.ResultType != "vector" {
		return nil, fmt.Errorf("query returned %s, an instant vector is needed", parsed.Data.ResultType)
	}
	out := make([]Sample, 0, len(parsed.Data.Result))
	for _, r := range parsed.Data.Result {
		str, _ := r.Value[1].(string)
		v, err := strconv.ParseFloat(str, 64)
		if err != nil {
			continue
		}
		out = append(out, Sample{Metric: r.Metric, Value: v})
	}
	return out, nil
}

// Duration renders d the way PromQL and LogQL range selectors expect.
func Duration(d time.Duration) string {
	if d%time.Minute == 0 {
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	return strconv.Itoa(int(d/time.Second)) + "s"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
