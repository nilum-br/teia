// Command demo plays every workload of the teia lab. One binary, configured
// by environment variables, so the lab can stage the situations teia must
// handle: instrumented services, an uninstrumented third party, a datastore
// seen only from its callers and a legacy job that emits no telemetry.
//
//	ROLE           service (default) | loadgen | legacy
//	SERVICE_NAME   name reported in telemetry
//	TELEMETRY      on (default) | off
//	CALLS          downstream HTTP calls: "name=url,name=url"
//	POSTGRES_DSN   query PostgreSQL on every request (or on every tick for legacy)
//	POSTGRES_PEER  node name of that database, e.g. orders-db
//	REDIS_ADDR     ping Redis on every request
//	REDIS_PEER     node name of that cache
//	ERROR_RATE     share of requests answered with HTTP 500
//	TARGET         URL the loadgen calls
//	INTERVAL       loadgen and legacy tick, e.g. 500ms
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var log = slog.New(slog.NewTextHandler(os.Stderr, nil))

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	name := env("SERVICE_NAME", "demo")
	log = log.With("service", name)
	if env("TELEMETRY", "on") == "on" {
		shutdown, err := setupTelemetry(ctx, name)
		if err != nil {
			log.Error("telemetry setup failed", "err", err)
			os.Exit(1)
		}
		defer shutdown()
	}

	var err error
	switch env("ROLE", "service") {
	case "loadgen":
		err = loadgen(ctx)
	case "legacy":
		err = legacy(ctx)
	default:
		err = serve(ctx)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Error("stopped", "err", err)
		os.Exit(1)
	}
}

func setupTelemetry(ctx context.Context, name string) (func(), error) {
	exp, err := otlptracehttp.New(ctx) // endpoint from OTEL_EXPORTER_OTLP_ENDPOINT
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(attribute.String("service.name", name)))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
	}, nil
}

var tracer = otel.Tracer("teia-lab/demo")

type downstream struct {
	name, url string
	client    *http.Client
}

func parseCalls(spec string) []downstream {
	var out []downstream
	for _, part := range strings.Split(spec, ",") {
		name, url, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		out = append(out, downstream{
			name: name,
			url:  url,
			client: &http.Client{
				Timeout: 3 * time.Second,
				Transport: otelhttp.NewTransport(http.DefaultTransport,
					otelhttp.WithSpanOptions(trace.WithAttributes(attribute.String("peer.service", name)))),
			},
		})
	}
	return out
}

func serve(ctx context.Context) error {
	calls := parseCalls(os.Getenv("CALLS"))
	errorRate, _ := strconv.ParseFloat(env("ERROR_RATE", "0"), 64)

	var db *pgxpool.Pool
	if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
		var err error
		if db, err = pgxpool.New(ctx, dsn); err != nil {
			return err
		}
		defer db.Close()
	}
	dbPeer := env("POSTGRES_PEER", "postgres")
	redisAddr, redisPeer := os.Getenv("REDIS_ADDR"), env("REDIS_PEER", "redis")

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		for _, d := range calls {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, d.url, nil)
			resp, err := d.client.Do(req)
			if err != nil {
				log.Warn("call failed", "to", d.name, "err", err)
				continue
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		if db != nil {
			queryOrders(ctx, db, dbPeer)
		}
		if redisAddr != "" {
			pingRedis(ctx, redisAddr, redisPeer)
		}
		if rand.Float64() < errorRate {
			http.Error(w, "injected failure", http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, "ok")
	})

	srv := &http.Server{Addr: ":" + env("PORT", "8080"), Handler: otelhttp.NewHandler(mux, "request"), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	log.Info("listening", "addr", srv.Addr, "calls", len(calls))
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// queryOrders records a client span the way database instrumentation does,
// so the collector can draw an edge to a node it never hears from.
func queryOrders(ctx context.Context, db *pgxpool.Pool, peer string) {
	ctx, span := tracer.Start(ctx, "SELECT orders", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.system.name", "postgresql"),
		attribute.String("db.namespace", "orders"),
		attribute.String("peer.service", peer),
	))
	defer span.End()
	var n int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM orders").Scan(&n); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

func pingRedis(ctx context.Context, addr, peer string) {
	_, span := tracer.Start(ctx, "PING", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
		attribute.String("db.system", "redis"),
		attribute.String("db.system.name", "redis"),
		attribute.String("peer.service", peer),
	))
	defer span.End()
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := conn.Write([]byte("*1\r\n$4\r\nPING\r\n")); err == nil {
		_, err = bufio.NewReader(conn).ReadString('\n')
	}
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}
}

func loadgen(ctx context.Context) error {
	target := env("TARGET", "http://web:8080/")
	interval, _ := time.ParseDuration(env("INTERVAL", "500ms"))
	client := &http.Client{Timeout: 5 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// legacy is the job nobody instrumented: it keeps a connection to the
// database and reads from it, invisible to traces.
func legacy(ctx context.Context) error {
	interval, _ := time.ParseDuration(env("INTERVAL", "5s"))
	db, err := pgxpool.New(ctx, os.Getenv("POSTGRES_DSN"))
	if err != nil {
		return err
	}
	defer db.Close()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		var n int
		if err := db.QueryRow(ctx, "SELECT count(*) FROM orders").Scan(&n); err != nil {
			log.Warn("report query failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
