// Package promapi is a minimal client for instant queries against
// Prometheus-compatible APIs: Prometheus, Thanos, Mimir, VictoriaMetrics, and
// Loki metric queries (LogQL answers in the same vector format).
package promapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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
	// Scope, when set, is added to every selector of every query, in
	// Dialect: one cluster's series on a backend that keeps several.
	Scope   Scope
	Dialect Dialect
}

// New returns a client with sane defaults.
func New(baseURL, path string, headers map[string]string) *Client {
	return &Client{BaseURL: baseURL, Path: path, Headers: headers, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// Query runs an instant query and returns its vector. Samples that are not
// finite numbers (NaN from a ratio over zero traffic, ±Inf) are dropped: they
// carry no rate and cannot be encoded as JSON further down.
func (c *Client) Query(ctx context.Context, query string) ([]Sample, error) {
	return c.QueryAt(ctx, query, time.Time{})
}

// QueryAt pins evaluation to one instant across related queries. This is
// when the expression is evaluated, not the timestamp of its raw samples.
// A zero at keeps the server's default evaluation time (used by detection).
func (c *Client) QueryAt(ctx context.Context, query string, at time.Time) ([]Sample, error) {
	if len(c.Scope) > 0 {
		scoped, err := c.Scope.Apply(query, c.Dialect)
		if err != nil {
			return nil, fmt.Errorf("adding the selector %s to the query: %w", c.Scope, err)
		}
		query = scoped
	}
	params := url.Values{"query": {query}}
	if !at.IsZero() {
		params.Set("time", at.UTC().Format(time.RFC3339Nano))
	}
	u := strings.TrimRight(c.BaseURL, "/") + c.Path + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, RedactError(err)
	}
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, RedactError(err)
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
			// Decoded once the type is known: scalars and strings are
			// not lists of series.
			Result json.RawMessage `json:"result"`
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
	var result []struct {
		Metric map[string]string `json:"metric"`
		Value  [2]any            `json:"value"`
	}
	if len(parsed.Data.Result) > 0 {
		if err := json.Unmarshal(parsed.Data.Result, &result); err != nil {
			return nil, fmt.Errorf("decoding vector: %w", err)
		}
	}
	out := make([]Sample, 0, len(result))
	for _, r := range result {
		str, _ := r.Value[1].(string)
		v, err := strconv.ParseFloat(str, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		out = append(out, Sample{Metric: r.Metric, Value: v})
	}
	return out, nil
}

// RedactURL renders raw for messages and findings without the password it
// may carry (https://user:pass@host). A URL that does not parse is reduced
// to its host, since there is no telling where its password ends.
func RedactURL(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Opaque == "" {
		return u.Redacted()
	}
	host := raw
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+len("://"):]
	}
	// The host follows the last '@': a password may itself hold '@' or '/'.
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	return host
}

// RedactError keeps credentials out of a *url.Error. net/http masks the
// password on transport errors, but a URL that fails to parse is quoted
// whole, and the parser may quote a piece of a malformed password ("invalid
// port"), so that detail is dropped when the URL has userinfo.
func RedactError(err error) error {
	var uerr *url.Error
	if !errors.As(err, &uerr) {
		return err
	}
	if uerr.Op == "parse" && strings.Contains(uerr.URL, "@") {
		return fmt.Errorf("invalid URL %s", RedactURL(uerr.URL))
	}
	uerr.URL = RedactURL(uerr.URL)
	return err
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
