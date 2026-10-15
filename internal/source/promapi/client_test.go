package promapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQuery(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		want    map[string]float64 // series "a" label -> value
		wantErr string
	}{
		{
			name: "vector",
			body: `{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"a":"x"},"value":[0,"1.5"]},{"metric":{"a":"y"},"value":[0,"0"]}]}}`,
			want: map[string]float64{"x": 1.5, "y": 0},
		},
		{
			// A ratio over zero traffic is NaN; a division by zero is ±Inf.
			name: "non-finite samples are dropped",
			body: `{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"a":"nan"},"value":[0,"NaN"]},{"metric":{"a":"inf"},"value":[0,"+Inf"]},
				{"metric":{"a":"-inf"},"value":[0,"-Inf"]},{"metric":{"a":"ok"},"value":[0,"2"]},
				{"metric":{"a":"garbage"},"value":[0,"two"]}]}}`,
			want: map[string]float64{"ok": 2},
		},
		{
			name: "empty result",
			body: `{"status":"success","data":{"resultType":"vector","result":[]}}`,
			want: map[string]float64{},
		},
		{
			name:    "non-200",
			status:  http.StatusServiceUnavailable,
			body:    `{"status":"error","error":"too many queries"}`,
			wantErr: "HTTP 503",
		},
		{
			name:    "error status",
			body:    `{"status":"error","errorType":"bad_data","error":"parse error at char 4"}`,
			wantErr: "query failed: parse error at char 4",
		},
		{
			name:    "matrix",
			body:    `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[0,"1"]]}]}}`,
			wantErr: "query returned matrix",
		},
		{
			// A scalar's result is a bare [time, value] pair, not a list of
			// series: it must be rejected by type, not fail to decode.
			name:    "scalar",
			body:    `{"status":"success","data":{"resultType":"scalar","result":[0,"1"]}}`,
			wantErr: "query returned scalar",
		},
		{
			name:    "loki streams",
			body:    `{"status":"success","data":{"resultType":"streams","result":[{"stream":{},"values":[["0","line"]]}]}}`,
			wantErr: "query returned streams",
		},
		{
			name:    "not json",
			body:    `<html>gateway</html>`,
			wantErr: "decoding answer",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if c.status != 0 {
					w.WriteHeader(c.status)
				}
				fmt.Fprint(w, c.body)
			}))
			defer srv.Close()
			samples, err := New(srv.URL, "/api/v1/query", nil).Query(context.Background(), "up")
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]float64{}
			for _, s := range samples {
				got[s.Metric["a"]] = s.Value
			}
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("samples = %v, want %v", got, c.want)
			}
		})
	}
}

func TestQueryAtPinsPrometheusAndLokiWithoutChangingLaterQueries(t *testing.T) {
	at := time.Date(2026, 10, 15, 12, 0, 0, 123456789, time.FixedZone("client", -3*3600))
	for _, path := range []string{"/api/v1/query", "/loki/api/v1/query"} {
		t.Run(path, func(t *testing.T) {
			var times []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != path || r.URL.Query().Get("query") != `sum(rate(requests{service="a+b"}[5m]))` {
					t.Errorf("query encoding/path changed: %s", r.URL)
				}
				times = append(times, r.URL.Query().Get("time"))
				fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
			}))
			defer srv.Close()
			client := New(srv.URL, path, nil)
			query := `sum(rate(requests{service="a+b"}[5m]))`
			if _, err := client.QueryAt(context.Background(), query, at); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Query(context.Background(), query); err != nil {
				t.Fatal(err)
			}
			if len(times) != 2 || times[0] != at.UTC().Format(time.RFC3339Nano) || times[1] != "" {
				t.Fatalf("evaluation time leaked between calls: %v", times)
			}
		})
	}
}

func TestQueryDenied(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		_, err := New(srv.URL, "/api/v1/query", nil).Query(context.Background(), "up")
		srv.Close()
		var denied *DeniedError
		if !errors.As(err, &denied) || denied.Status != status {
			t.Errorf("HTTP %d: err = %v, want a DeniedError", status, err)
		}
	}
}

func TestRedactURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://prom.example:9090/prefix", "https://prom.example:9090/prefix"},
		{"https://teia:s3cret@prom.example/api", "https://teia:xxxxx@prom.example/api"},
		{"https://teia@prom.example", "https://teia@prom.example"},
		// Unparsable: only the host is left, whatever the password holds.
		{"https://teia:s3cret@prom.example:port/x", "prom.example:port"},
		{"https://teia:s3/cr@et@prom.example:port/x", "prom.example:port"},
		// No scheme: "teia" would be read as one and the rest as opaque.
		{"teia:s3cret@prom.example:9090", "prom.example:9090"},
	}
	for _, c := range cases {
		if got := RedactURL(c.in); got != c.want {
			t.Errorf("RedactURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestErrorsDoNotLeakURLPasswords(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	for _, base := range []string{
		// Transport error: net/http quotes the URL.
		strings.Replace(closed.URL, "http://", "http://teia:s3cret@", 1),
		// Parse errors: the URL is quoted whole, the parser may quote a
		// piece of the password.
		"http://teia:s3cret@prom.example:port",
		"http://teia:s3/cret@prom.example",
	} {
		_, err := New(base, "/api/v1/query", nil).Query(context.Background(), "up")
		if err == nil {
			t.Fatalf("%s: want an error", base)
		}
		if strings.Contains(err.Error(), "s3") {
			t.Errorf("error leaks the password: %v", err)
		}
	}
}
