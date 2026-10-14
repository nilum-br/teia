package main

// ROLE=e2e checks, from inside the lab network, that teia finds what each
// signal should reveal, then follows the advisor (GRANT pg_monitor) and
// checks that the hidden client shows up. Runs the same on any OS:
//
//	docker compose -f lab/compose.yaml run --rm e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type snapshot struct {
	Nodes []struct {
		ID         string    `json:"id"`
		Tech       string    `json:"tech"`
		Health     string    `json:"health"`
		Visibility string    `json:"visibility"`
		Reasons    []reason  `json:"reasons"`
		Measures   []measure `json:"measures"`
	} `json:"nodes"`
	Edges []struct {
		From     string    `json:"from"`
		To       string    `json:"to"`
		State    string    `json:"state"`
		Observed bool      `json:"observed"`
		Sources  []string  `json:"sources"`
		P95      float64   `json:"latencyP95Ms"`
		Reasons  []reason  `json:"reasons"`
		Measures []measure `json:"measures"`
	} `json:"edges"`
	Findings []struct {
		Rule    string `json:"rule"`
		Subject string `json:"subject"`
	} `json:"findings"`
	Coverage struct {
		Visibility float64 `json:"visibility"`
	} `json:"coverage"`
	Sources []struct {
		Name, State, Message string
	} `json:"sources"`
}

type reason struct {
	Cause string `json:"cause"`
	Via   string `json:"via"`
}

type measure struct {
	Cause string  `json:"cause"`
	What  string  `json:"what"`
	Value float64 `json:"value"`
	Max   float64 `json:"max"`
}

// measured reports whether a node ("orders-db") or an edge ("a->b") carries
// a measure of cause, against a capacity when withMax.
func (s *snapshot) measured(subject, cause string, withMax bool) bool {
	var ms []measure
	for _, n := range s.Nodes {
		if n.ID == subject {
			ms = n.Measures
		}
	}
	for _, e := range s.Edges {
		if e.From+"->"+e.To == subject {
			ms = e.Measures
		}
	}
	for _, m := range ms {
		if m.Cause == cause && m.Value > 0 && (!withMax || m.Max > 0) {
			return true
		}
	}
	return false
}

// why reports whether the node is amber or red because of cause, coming
// from via ("" for the node itself).
func (s *snapshot) why(id, cause, via string) bool {
	for _, n := range s.Nodes {
		if n.ID != id {
			continue
		}
		for _, r := range n.Reasons {
			if r.Cause == cause && r.Via == via {
				return true
			}
		}
	}
	return false
}

// edgeWhy reports whether the edge is amber or red because of cause, and
// teia knows where it comes from.
func (s *snapshot) edgeWhy(from, to, cause string) bool {
	for _, e := range s.Edges {
		if e.From != from || e.To != to {
			continue
		}
		for _, r := range e.Reasons {
			if r.Cause == cause && r.Via != "" {
				return true
			}
		}
	}
	return false
}

type check struct {
	name string
	ok   func(s *snapshot) bool
}

func (s *snapshot) edge(from, to string, sources ...string) bool {
	for _, e := range s.Edges {
		if e.From == from && e.To == to && e.Observed {
			for _, want := range sources {
				if !slices.Contains(e.Sources, want) {
					return false
				}
			}
			return true
		}
	}
	return false
}

// latency reports whether the edge has a p95 latency measured.
func (s *snapshot) latency(from, to string) bool {
	for _, e := range s.Edges {
		if e.From == from && e.To == to {
			return e.P95 > 0
		}
	}
	return false
}

func (s *snapshot) finding(rule, subject string) bool {
	for _, f := range s.Findings {
		if f.Rule == rule && (subject == "" || f.Subject == subject) {
			return true
		}
	}
	return false
}

func (s *snapshot) node(id string) bool {
	for _, n := range s.Nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

func (s *snapshot) tech(id, want string) bool {
	for _, n := range s.Nodes {
		if n.ID == id {
			return n.Tech == want
		}
	}
	return false
}

func (s *snapshot) noPlaceholderNodes() bool {
	for _, n := range s.Nodes {
		if n.ID == "user" || n.ID == "unknown" || strings.HasPrefix(n.ID, "ip:") {
			return false
		}
	}
	return true
}

func fetch(ctx context.Context, url string) (*snapshot, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/v1/snapshot", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var s snapshot
	return &s, json.NewDecoder(resp.Body).Decode(&s)
}

// waitFor polls until every check passes or the deadline expires, then
// prints the result of each.
func waitFor(ctx context.Context, url, phase string, checks []check, timeout time.Duration) (*snapshot, bool) {
	deadline := time.Now().Add(timeout)
	var last *snapshot
	for {
		s, err := fetch(ctx, url)
		if err == nil {
			last = s
			pass := true
			for _, c := range checks {
				pass = pass && c.ok(s)
			}
			if pass || time.Now().After(deadline) {
				fmt.Printf("\n%s\n", phase)
				for _, c := range checks {
					mark := "ok  "
					if !c.ok(s) {
						mark = "FAIL"
					}
					fmt.Printf("  %s %s\n", mark, c.name)
				}
				if !pass {
					for _, src := range s.Sources {
						fmt.Printf("       source %-24s %-9s %s\n", src.Name, src.State, src.Message)
					}
				}
				return s, pass
			}
		} else if time.Now().After(deadline) {
			fmt.Printf("\n%s\n  FAIL teia API unreachable: %v\n", phase, err)
			return last, false
		}
		select {
		case <-ctx.Done():
			return last, false
		case <-time.After(5 * time.Second):
		}
	}
}

func e2e(ctx context.Context) error {
	url := env("TEIA_URL", "http://teia:8080")
	dsn := env("PG_ADMIN_DSN", "postgres://postgres:postgres@orders-db:5432/orders?sslmode=disable")
	timeout, _ := time.ParseDuration(env("E2E_TIMEOUT", "6m"))

	if err := pgExec(ctx, dsn, "REVOKE pg_monitor FROM teia"); err != nil {
		return err
	}
	before, ok := waitFor(ctx, url, "Every signal contributes (before following the advisor)", []check{
		{"traces: web -> orders seen by prometheus (service graph) and tempo", func(s *snapshot) bool { return s.edge("web", "orders", "prometheus", "tempo") }},
		{"traces: orders -> orders-db (database seen from its callers)", func(s *snapshot) bool { return s.edge("orders", "orders-db") }},
		{"mesh: recommendations -> inventory, no OpenTelemetry involved", func(s *snapshot) bool { return s.edge("recommendations", "inventory", "prometheus") }},
		{"latency: p95 from the service graph (web -> orders) and from the mesh (recommendations -> inventory)", func(s *snapshot) bool {
			return s.latency("web", "orders") && s.latency("recommendations", "inventory")
		}},
		{"why: inventory is amber for its latency, and recommendations says it waits on inventory", func(s *snapshot) bool {
			return s.why("inventory", "latency", "") && s.why("recommendations", "latency", "inventory")
		}},
		{"cache: catalog-cache memory against maxmemory, from redis_exporter", func(s *snapshot) bool {
			return s.measured("catalog-cache", "saturation", true)
		}},
		{"queue: email-worker -> order-events has messages waiting and is behind, from kafka_exporter", func(s *snapshot) bool {
			return s.measured("email-worker->order-events", "backlog", false) && s.measured("email-worker->order-events", "lag", false)
		}},
		{"traces + mesh: web -> recommendations", func(s *snapshot) bool { return s.edge("web", "recommendations") }},
		{"logs: partner-sync -> catalog, named from catalog's access log", func(s *snapshot) bool { return s.edge("partner-sync", "catalog", "loki/access-logs") }},
		{"inventory: legacy-reports exists although it emits nothing", func(s *snapshot) bool { return s.node("legacy-reports") }},
		{"advisor: asks for pg_monitor on orders-db", func(s *snapshot) bool { return s.finding("postgres-privilege", "orders-db") }},
		{"no unexplained unknown callers", func(s *snapshot) bool { return !s.finding("unknown-callers", "") }},
		{"no placeholder nodes (user, unknown, raw IPs)", (*snapshot).noPlaceholderNodes},
		{"icons: postgresql, redis, kafka and mongodb recognised", func(s *snapshot) bool {
			return s.tech("orders-db", "postgresql") && s.tech("catalog-cache", "redis") && s.tech("order-events", "kafka") && s.tech("catalog-db", "mongodb")
		}},
		{"health: payments degraded or failing from its injected errors", func(s *snapshot) bool {
			for _, n := range s.Nodes {
				if n.ID == "payments" {
					return n.Health == "degraded" || n.Health == "failing"
				}
			}
			return false
		}},
	}, timeout)
	if !ok {
		return fmt.Errorf("phase 1 failed")
	}

	fmt.Println("\nFollowing the advisor: GRANT pg_monitor TO teia")
	if err := pgExec(ctx, dsn, "GRANT pg_monitor TO teia"); err != nil {
		return err
	}
	// Leave the lab as it was, so the next run starts from the same picture.
	defer func() {
		if err := pgExec(context.WithoutCancel(ctx), dsn, "REVOKE pg_monitor FROM teia"); err != nil {
			fmt.Println("  could not revoke pg_monitor again:", err)
		}
	}()

	after, ok := waitFor(ctx, url, "The hidden client shows up", []check{
		{"database: legacy-reports -> orders-db, seen only from inside postgres", func(s *snapshot) bool { return s.edge("legacy-reports", "orders-db", "postgres/orders-db") }},
		{"advisor: no longer asks for pg_monitor", func(s *snapshot) bool { return !s.finding("postgres-privilege", "") }},
		{"advisor: next step is to instrument legacy-reports", func(s *snapshot) bool { return s.finding("no-telemetry", "legacy-reports") }},
		{"visibility went up", func(s *snapshot) bool { return s.Coverage.Visibility > before.Coverage.Visibility }},
	}, 2*time.Minute)
	if !ok {
		return fmt.Errorf("phase 2 failed")
	}

	// Now that teia sees inside the database, a lock: the orders service's
	// queries wait on it, and teia should say so on that line, with who holds it.
	fmt.Println("\nA lock shows up: an exclusive lock on the orders table, held for a while")
	release, err := holdLock(ctx, dsn, "LOCK TABLE orders IN ACCESS EXCLUSIVE MODE")
	if err != nil {
		return err
	}
	_, ok = waitFor(ctx, url, "Why orders is stuck", []check{
		{"locks: orders -> orders-db waits on a lock, and teia names who holds it", func(s *snapshot) bool { return s.edgeWhy("orders", "orders-db", "locks") }},
	}, 2*time.Minute)
	release()
	if !ok {
		return fmt.Errorf("phase 3 failed")
	}
	fmt.Printf("\nvisibility %.0f%% -> %.0f%%. All checks passed.\n", before.Coverage.Visibility*100, after.Coverage.Visibility*100)
	return nil
}

// holdLock runs sql in a transaction and keeps it open until release.
func holdLock(ctx context.Context, dsn, sql string) (release func(), err error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}
	tx, err := conn.Begin(ctx)
	if err == nil {
		_, err = tx.Exec(ctx, sql)
	}
	if err != nil {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("taking the lock: %w", err)
	}
	return func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		_ = conn.Close(context.WithoutCancel(ctx))
	}, nil
}

func pgExec(ctx context.Context, dsn, sql string) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}
	defer conn.Close(context.WithoutCancel(ctx))
	_, err = conn.Exec(ctx, sql)
	return err
}

func init() {
	if os.Getenv("ROLE") == "e2e" {
		os.Setenv("TELEMETRY", "off")
		os.Setenv("ACCESS_LOG", "off")
	}
}
