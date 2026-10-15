// Package postgres explores a PostgreSQL server from the inside: it reads
// pg_stat_activity to find every client connected to it, including clients
// that emit no telemetry at all (batch jobs, legacy apps, people with psql).
//
// Without the pg_read_all_stats privilege (granted through pg_monitor)
// PostgreSQL hides where other sessions come from, so the explorer reports
// exactly that grant as the next step.
//
// It also reads connection saturation and, with statistics privileges, lock
// blockers, oldest open transaction age and available replay acknowledgement
// delay. These current-state readings do not establish the time of a client call.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

type Config struct {
	// Node is the node ID of this database in the graph, e.g. "orders-db".
	Node string
	// DSN; ${NAME} references are expanded when the config is loaded.
	DSN string
}

type Source struct {
	name string
	cfg  Config
}

func New(name string, cfg Config) *Source {
	return &Source{name: name, cfg: cfg}
}

func (s *Source) Name() string { return s.name }
func (s *Source) Type() string { return "postgres" }

// Without pg_read_all_stats, PostgreSQL also hides backend_type of other
// sessions, so client sessions are told apart from background workers by
// having a database.
const activityQuery = `
SELECT host(client_addr), usename, count(*)
FROM pg_stat_activity
WHERE pid <> pg_backend_pid()
  AND datname IS NOT NULL
  AND (backend_type = 'client backend' OR backend_type IS NULL)
GROUP BY 1, 2`

// connectionsQuery counts client sessions against the connections ordinary
// roles may open (superusers keep a few in reserve).
const connectionsQuery = `
SELECT count(*) FILTER (WHERE datname IS NOT NULL AND (backend_type = 'client backend' OR backend_type IS NULL)),
       current_setting('max_connections')::int - current_setting('superuser_reserved_connections')::int
FROM pg_stat_activity`

// locksQuery counts, per client address, the sessions waiting on a lock for
// over lockWait, with the address of the first session blocking them. It
// needs pg_read_all_stats to see other sessions' addresses and waits.
const locksQuery = `
SELECT host(w.client_addr), host(b.client_addr), count(*)
FROM pg_stat_activity w
LEFT JOIN LATERAL (
  SELECT client_addr FROM pg_stat_activity
  WHERE pid = (pg_blocking_pids(w.pid))[1]
) b ON true
WHERE w.wait_event_type = 'Lock'
  AND w.state = 'active'
  AND w.pid <> pg_backend_pid()
  AND now() - w.query_start > interval '1 second'
GROUP BY 1, 2`

const (
	whatConnections = "connections"
	whatLocks       = "sessions waiting on locks"
)

func (s *Source) Discover(ctx context.Context) (source.Observation, error) {
	obs := source.Observation{Explores: []string{s.cfg.Node}}

	// pgx masks the password in the DSN it quotes on a parse error, but only
	// as a best effort on malformed strings, so that error is not passed on.
	cfg, err := pgx.ParseConfig(s.cfg.DSN)
	if err != nil {
		return obs, fmt.Errorf("the dsn of %s is not a valid connection string (URL or key=value; percent-encode special characters of a password in a URL)", s.cfg.Node)
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		err = redact(err, cfg.Password)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.Code == "28P01" || pgErr.Code == "28000") {
			obs.Degraded = true
			obs.Message = "authentication failed"
			obs.Findings = append(obs.Findings, s.loginFinding(mask(pgErr.Message, cfg.Password)))
			return obs, nil
		}
		return obs, fmt.Errorf("connecting to %s: %w", s.cfg.Node, err)
	}
	defer conn.Close(context.WithoutCancel(ctx))

	var user string
	var privileged bool
	err = conn.QueryRow(ctx, `SELECT current_user, pg_has_role(current_user, 'pg_read_all_stats', 'USAGE')`).Scan(&user, &privileged)
	if err != nil {
		return obs, fmt.Errorf("checking privileges on %s: %w", s.cfg.Node, err)
	}

	rows, err := conn.Query(ctx, activityQuery)
	if err != nil {
		return obs, fmt.Errorf("reading pg_stat_activity on %s: %w", s.cfg.Node, err)
	}
	defer rows.Close()

	hidden := map[string]int{}
	for rows.Next() {
		var addr, usename *string
		var sessions int
		if err := rows.Scan(&addr, &usename, &sessions); err != nil {
			return obs, err
		}
		if addr == nil {
			// Unix-socket session, or one whose origin we may not see.
			if !privileged && usename != nil && *usename != user {
				hidden[*usename] += sessions
			}
			continue
		}
		obs.Edges = append(obs.Edges, source.EdgeObservation{
			From:     source.AddressID(*addr),
			To:       s.cfg.Node,
			Protocol: "postgresql",
			Signal:   source.SignalDatabase,
		})
		obs.Nodes = append(obs.Nodes, source.NodeObservation{ID: source.AddressID(*addr), Kind: model.KindUnknown})
	}
	if err := rows.Err(); err != nil {
		return obs, err
	}

	dbNode := source.NodeObservation{ID: s.cfg.Node, Kind: model.KindDatabase, SelfReported: privileged}
	var used, limit float64
	if err := conn.QueryRow(ctx, connectionsQuery).Scan(&used, &limit); err != nil {
		return obs, fmt.Errorf("counting connections on %s: %w", s.cfg.Node, err)
	}
	if limit > 0 {
		dbNode.Measures = []model.Measure{{Cause: model.CauseSaturation, What: whatConnections, Value: used, Max: limit}}
	}
	obs.Nodes = append(obs.Nodes, dbNode)
	if privileged {
		if err := s.locks(ctx, conn, &obs); err != nil {
			return obs, err
		}
		if err := s.resources(ctx, conn, &obs); err != nil {
			obs.Degraded = true
			obs.Message = "additional transaction/replication diagnostics unavailable"
			obs.Findings = append(obs.Findings, model.Finding{Rule: "postgres-resource-diagnostics", Kind: model.FindingSource, Severity: model.SeverityInfo, Subject: s.cfg.Node,
				Title: "Transaction and replication diagnostics could not be read", Detail: redact(err, cfg.Password).Error()})
		}
	}
	if !privileged {
		obs.Degraded = true
		obs.Message = fmt.Sprintf("role %s lacks pg_read_all_stats", user)
		obs.Findings = append(obs.Findings, s.privilegeFinding(user, hidden))
	}
	return obs, nil
}

// resourceQuery reads only statistics. A missing replay_lag is unknown/not
// applicable, not zero: a primary without a replica and an idle replica may
// both have NULL here. Transaction start is not a client call timestamp.
const resourceQuery = `SELECT
 COALESCE((SELECT max(EXTRACT(EPOCH FROM statement_timestamp() - xact_start))
  FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND xact_start IS NOT NULL
  AND backend_type = 'client backend'), 0),
 (SELECT max(EXTRACT(EPOCH FROM replay_lag)) FROM pg_stat_replication)`

func (s *Source) resources(ctx context.Context, conn *pgx.Conn, obs *source.Observation) error {
	var age float64
	var replay *float64
	if err := conn.QueryRow(ctx, resourceQuery).Scan(&age, &replay); err != nil {
		return fmt.Errorf("reading resource statistics on %s: %w", s.cfg.Node, err)
	}
	obs.Readings = append(obs.Readings, source.Reading{Candidates: []string{s.cfg.Node}, Origin: "postgres pg_stat_activity transaction starts", Measures: []model.Measure{{Cause: model.CauseLag, What: "oldest open transaction age", Value: age, Unit: model.UnitSeconds}}})
	if replay != nil && *replay >= 0 {
		obs.Readings = append(obs.Readings, source.Reading{Candidates: []string{s.cfg.Node}, Origin: "postgres pg_stat_replication replay_lag", Measures: []model.Measure{{Cause: model.CauseLag, What: "replica replay acknowledgement delay", Value: *replay, Unit: model.UnitSeconds}}})
	} else {
		obs.Findings = append(obs.Findings, model.Finding{Rule: "postgres-replication-availability", Kind: model.FindingBlindSpot, Severity: model.SeverityInfo, Subject: s.cfg.Node,
			Title: "Replica replay delay is unavailable or not applicable", Detail: "pg_stat_replication supplied no non-null replay_lag. No replica, an idle/caught-up replica or missing replay feedback can cause this; no zero delay was inferred."})
	}
	return nil
}

// locks adds, to the edge of each client with sessions waiting on a lock,
// how many wait and who holds the lock.
func (s *Source) locks(ctx context.Context, conn *pgx.Conn, obs *source.Observation) error {
	rows, err := conn.Query(ctx, locksQuery)
	if err != nil {
		return fmt.Errorf("reading lock waits on %s: %w", s.cfg.Node, err)
	}
	defer rows.Close()
	for rows.Next() {
		var waiting, blocker *string
		var sessions float64
		if err := rows.Scan(&waiting, &blocker, &sessions); err != nil {
			return err
		}
		if waiting == nil {
			continue // a Unix-socket session: no address to name its client by
		}
		m := model.Measure{Cause: model.CauseLocks, What: whatLocks, Value: sessions}
		if blocker != nil {
			m.Via = source.AddressID(*blocker)
		}
		from := source.AddressID(*waiting)
		i := slices.IndexFunc(obs.Edges, func(e source.EdgeObservation) bool { return e.From == from })
		if i < 0 {
			obs.Edges = append(obs.Edges, source.EdgeObservation{From: from, To: s.cfg.Node, Protocol: "postgresql", Signal: source.SignalDatabase})
			i = len(obs.Edges) - 1
		}
		// Several blockers for one client: keep the one most sessions wait on.
		if j := slices.IndexFunc(obs.Edges[i].Measures, func(x model.Measure) bool { return x.Cause == model.CauseLocks }); j < 0 {
			obs.Edges[i].Measures = append(obs.Edges[i].Measures, m)
		} else if have := &obs.Edges[i].Measures[j]; m.Value > have.Value {
			*have = model.Measure{Cause: m.Cause, What: m.What, Value: have.Value + m.Value, Via: m.Via}
		} else {
			have.Value += m.Value
		}
	}
	return rows.Err()
}

// redact masks the password wherever err's text quotes it. pgx leaves it
// out of what it formats; this covers anything else on the way (a server or
// proxy echoing it back), and keeps the error chain for errors.As.
func redact(err error, password string) error {
	if password == "" || !strings.Contains(err.Error(), password) {
		return err
	}
	return &redactedError{msg: mask(err.Error(), password), err: err}
}

func mask(text, password string) string {
	if password == "" {
		return text
	}
	return strings.ReplaceAll(text, password, "xxxxx")
}

type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

func (s *Source) privilegeFinding(user string, hidden map[string]int) model.Finding {
	detail := fmt.Sprintf("teia is connected to %s as %q but PostgreSQL hides where other sessions come from.", s.cfg.Node, user)
	if len(hidden) > 0 {
		var parts []string
		total := 0
		for u, n := range hidden {
			parts = append(parts, fmt.Sprintf("%s (%d)", u, n))
			total += n
		}
		sort.Strings(parts)
		detail += fmt.Sprintf(" Right now %d session(s) are hidden, from roles: %s.", total, strings.Join(parts, ", "))
	}
	return model.Finding{
		Rule:     "postgres-privilege",
		Kind:     model.FindingPermission,
		Severity: model.SeverityWarning,
		Subject:  s.cfg.Node,
		Title:    fmt.Sprintf("Reached database %s but cannot see who else connects to it", s.cfg.Node),
		Detail:   detail,
		Fix: &model.Fix{
			Summary:  "Grant the built-in monitoring role. It is read-only: it exposes session metadata and statistics, not table data.",
			Language: "sql",
			Snippet:  fmt.Sprintf("GRANT pg_monitor TO %s;", pgx.Identifier{user}.Sanitize()),
		},
	}
}

func (s *Source) loginFinding(msg string) model.Finding {
	return model.Finding{
		Rule:     "postgres-login",
		Kind:     model.FindingPermission,
		Severity: model.SeverityWarning,
		Subject:  s.cfg.Node,
		Title:    fmt.Sprintf("teia cannot log into database %s", s.cfg.Node),
		Detail:   msg,
		Fix: &model.Fix{
			Summary:  "Create a dedicated login for teia with the monitoring role only, then put its password in the DSN through an environment variable.",
			Language: "sql",
			Snippet: `CREATE ROLE teia LOGIN PASSWORD 'change-me';
GRANT pg_monitor TO teia;`,
		},
	}
}
