package postgres

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
)

// Point only at an isolated disposable PostgreSQL database. The fixture uses
// transaction-scoped advisory locks and dedicated test roles, never application data.
func TestResourceDiagnosticsPostgres(t *testing.T) {
	dsn := os.Getenv("TEIA_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("set TEIA_TEST_POSTGRES_URL to an isolated disposable PostgreSQL instance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	holder, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close(context.Background())
	if _, err = holder.Exec(ctx, "CREATE ROLE teia_diagnostic_monitor LOGIN PASSWORD 'test-only'; GRANT pg_monitor TO teia_diagnostic_monitor; CREATE ROLE teia_diagnostic_limited LOGIN PASSWORD 'test-only'"); err != nil {
		t.Fatal(err)
	}
	defer holder.Exec(context.Background(), "DROP ROLE teia_diagnostic_monitor; DROP ROLE teia_diagnostic_limited")
	if _, err = holder.Exec(ctx, "BEGIN; SELECT pg_advisory_xact_lock(7160915)"); err != nil {
		t.Fatal(err)
	}
	defer holder.Exec(context.Background(), "ROLLBACK")
	waiter, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, stopWait := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer waiter.Close(context.Background())
		_, _ = waiter.Exec(waitCtx, "SELECT pg_advisory_xact_lock(7160915)")
	}()
	defer func() { stopWait(); <-done }()
	// Observe the actual wait state rather than assuming the waiter was
	// scheduled immediately on a busy CI runner. Autocommit refreshes stats.
	observer, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close(context.Background())
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var ready bool
		if err := observer.QueryRow(ctx, `SELECT COALESCE(wait_event_type = 'Lock' AND statement_timestamp() - query_start > interval '1 second',false) FROM pg_stat_activity WHERE pid=$1`, waiter.PgConn().PID()).Scan(&ready); err != nil {
			t.Fatal(err)
		}
		if ready {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	monitor, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	// Keep host/port/database from the disposable fixture, with dedicated roles.
	roleDSN := func(role string) string {
		return "host=" + monitor.Host + " port=" + fmt.Sprint(monitor.Port) + " dbname=" + monitor.Database + " user=" + role + " password=test-only sslmode=disable"
	}
	src := New("postgres/db", Config{Node: "db", DSN: roleDSN("teia_diagnostic_monitor")})
	obs, err := src.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Readings) != 1 || obs.Readings[0].Measures[0].Value < 1 || obs.Readings[0].Measures[0].What != "oldest open transaction age" {
		t.Fatalf("old transaction lost: %+v", obs)
	}
	if !obs.Readings[0].OldestSampleAt.IsZero() || slices.IndexFunc(obs.Findings, func(f model.Finding) bool { return f.Rule == "postgres-replication-availability" }) < 0 {
		t.Fatalf("inapplicable replica became zero or read time became event time: %+v", obs)
	}
	now := time.Now()
	g := graph.BuildAt([]graph.Result{{Source: src, Observation: obs, CollectedAt: now}}, nil, nil, now)
	limits := graph.DefaultHealth
	limits.LagDegradedSeconds = .1
	limits.LagFailingSeconds = 1
	graph.ApplyHealth(g, limits)
	if len(g.Nodes["db"].Reasons) == 0 || len(g.Nodes["db"].Reasons[0].Evidence) != 1 {
		t.Fatalf("transaction diagnosis lost evidence: %+v", g.Nodes["db"])
	}
	locked := false
	for _, e := range g.Edges {
		for _, reason := range e.Reasons {
			if reason.Cause == model.CauseLocks && reason.Value >= 1 && len(reason.Evidence) > 0 {
				locked = true
			}
		}
	}
	if !locked {
		t.Fatalf("blocked transaction lost lock reason/evidence: %+v", g.Edges)
	}
	limited, err := New("postgres/db", Config{Node: "db", DSN: roleDSN("teia_diagnostic_limited")}).Discover(ctx)
	if err != nil || !limited.Degraded || len(limited.Readings) != 0 {
		t.Fatalf("limited role falsely sees transactions/replication: %+v %v", limited, err)
	}
}
