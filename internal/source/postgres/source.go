// Package postgres explores a PostgreSQL server from the inside: it reads
// pg_stat_activity to find every client connected to it, including clients
// that emit no telemetry at all (batch jobs, legacy apps, people with psql).
//
// Without the pg_read_all_stats privilege (granted through pg_monitor)
// PostgreSQL hides where other sessions come from, so the explorer reports
// exactly that grant as the next step.
package postgres

import (
	"context"
	"errors"
	"fmt"
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

	obs.Nodes = append(obs.Nodes, source.NodeObservation{
		ID:           s.cfg.Node,
		Kind:         model.KindDatabase,
		SelfReported: privileged,
	})
	if !privileged {
		obs.Degraded = true
		obs.Message = fmt.Sprintf("role %s lacks pg_read_all_stats", user)
		obs.Findings = append(obs.Findings, s.privilegeFinding(user, hidden))
	}
	return obs, nil
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
