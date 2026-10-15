// Command teia maps who talks to whom, compares it with what teams declared
// and tells you what to do next to see more.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nilum-br/teia/internal/api"
	"github.com/nilum-br/teia/internal/config"
	"github.com/nilum-br/teia/internal/engine"
	exp "github.com/nilum-br/teia/internal/export"
	"github.com/nilum-br/teia/internal/history"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source/declared"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `teia maps service communication and tells you where it is blind.

Usage:
  teia serve    [--config teia.yaml] [--ui-dir DIR]
                                         run discovery continuously and serve the API and UI
  teia discover [--config teia.yaml] [--output text|json] [--fail-on SEVERITY]
                                         run one discovery round and print the report
  teia detect   [--prometheus URL] [--tempo URL] [--loki URL] [--kubernetes] [--docker]
                                         probe what the platform already runs and print a starting config
  teia export matrix [--config teia.yaml] [--lang pt-BR] [--declared-only]
                                         flow matrix (CSV) for firewall and security reviews
  teia diff --before OLD.json --after NEW.json [--format text|json]
                                         compare saved topology, contract and health states
  teia diff --snapshot GRAPH.json [--flows FILE] [--format text|json|flows]
            [--accept 'caller->callee=reason']
                                         review observed additions; emit only explicitly accepted flows
  teia validate FILE...                  check flows files
  teia review propose --snapshot GRAPH.json --from CALLER --to CALLEE --rationale TEXT
            or: --snapshot GRAPH.json --request REQUEST.json
            [--format json|markdown|flows|bundle] [--output FILE]
                                         prepare a local contract proposal; does not open a PR
  teia version

The config path defaults to $TEIA_CONFIG, then ./teia.yaml. --ui-dir (env
TEIA_UI_DIR) serves the UI from a directory instead of the embedded copy, so
edits to internal/api/ui show on the next browser reload.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
	case "discover":
		err = discover(os.Args[2:])
	case "detect":
		err = detect(os.Args[2:])
	case "export":
		err = export(os.Args[2:])
	case "validate":
		err = validate(os.Args[2:])
	case "diff":
		err = diff(os.Args[2:])
	case "review":
		err = reviewCommand(os.Args[2:])
	case "version", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// defaultConfig lets images set the config path once, so commands run inside
// a container need no path argument.
func defaultConfig() string {
	if p := os.Getenv("TEIA_CONFIG"); p != "" {
		return p
	}
	return "teia.yaml"
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func newEngine(cfg *config.Config, log *slog.Logger, stores ...*history.Store) *engine.Engine {
	var archive *history.Store
	if len(stores) > 0 {
		archive = stores[0]
	}
	return engine.New(cfg.BuildSources(), engine.Options{
		History:         archive,
		Interval:        cfg.Interval,
		Timeout:         cfg.Timeout,
		Aliases:         cfg.Aliases,
		Ignore:          cfg.Ignore,
		Retention:       cfg.Retention,
		Health:          cfg.HealthThresholds(),
		HealthOverrides: cfg.HealthOverrides(),
		Notifiers:       cfg.Notifiers(),
		EvidenceLinks:   cfg.EvidenceLinks,
	}, log)
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	path := fs.String("config", defaultConfig(), "config file (env TEIA_CONFIG)")
	uiDir := fs.String("ui-dir", os.Getenv("TEIA_UI_DIR"), "serve the UI from this directory instead of the embedded copy (env TEIA_UI_DIR)")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	ui, err := uiFiles(*uiDir)
	if err != nil {
		return err
	}
	log := newLogger()
	if ui != nil {
		log.Info("serving the UI from disk; edits show on the next reload", "dir", *uiDir)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	archive, err := history.Open(cfg.History)
	if err != nil {
		return fmt.Errorf("opening observation history: %w", err)
	}
	eng := newEngine(cfg, log, archive)
	go eng.Run(ctx)

	if cfg.Auth.Token == "" {
		log.Warn("the API has no auth.token: anyone who can reach this address can read the map", "addr", cfg.Listen)
	}
	srv := newServer(cfg.Listen, api.Handler(eng, api.Options{
		History:        archive,
		Token:          cfg.Auth.Token,
		AllowedOrigins: cfg.CORS.AllowedOrigins,
		MaxStreams:     cfg.MaxStreams,
		Log:            log,
		UI:             ui,
	}))
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("teia listening", "addr", cfg.Listen, "version", version)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// uiFiles opens the --ui-dir directory; empty means the embedded UI (nil).
// A directory without index.html is an error, since the UI would only 404.
func uiFiles(dir string) (fs.FS, error) {
	if dir == "" {
		return nil, nil
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		return nil, fmt.Errorf("--ui-dir %s: %w (point it at internal/api/ui in a checkout)", dir, err)
	}
	return os.DirFS(dir), nil
}

// newServer builds the HTTP server. Shutdown cancels every request context,
// so open event streams end instead of holding Shutdown until its deadline.
func newServer(addr string, h http.Handler) *http.Server {
	base, cancel := context.WithCancel(context.Background())
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return base },
	}
	srv.RegisterOnShutdown(cancel)
	return srv
}

func discover(args []string) error {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	path := fs.String("config", defaultConfig(), "config file (env TEIA_CONFIG)")
	output := fs.String("output", "text", "text or json")
	failOn := fs.String("fail-on", "", "exit 1 if a finding of this severity or worse exists: critical, warning, info")
	_ = fs.Parse(args)

	threshold, err := parseFailOn(*failOn)
	if err != nil {
		return err
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	snap := newEngine(cfg, log).Refresh(context.Background())

	switch *output {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(snap); err != nil {
			return err
		}
	default:
		printReport(os.Stdout, snap)
	}

	return failOnFindings(snap.Findings, threshold)
}

// parseFailOn validates --fail-on: a typo must not rank as info and fail
// every run.
func parseFailOn(v string) (model.Severity, error) {
	switch s := model.Severity(v); s {
	case "", model.SeverityCritical, model.SeverityWarning, model.SeverityInfo:
		return s, nil
	}
	return "", fmt.Errorf("--fail-on must be critical, warning or info, got %q", v)
}

// failOnFindings fails when a finding is at least as urgent as threshold;
// an empty threshold never fails.
func failOnFindings(findings []model.Finding, threshold model.Severity) error {
	if threshold == "" {
		return nil
	}
	count, worst := 0, threshold
	for _, f := range findings {
		if f.Severity.Rank() <= threshold.Rank() {
			count++
			if f.Severity.Rank() < worst.Rank() {
				worst = f.Severity
			}
		}
	}
	if count > 0 {
		return fmt.Errorf("%d finding(s) at or above %s (most urgent: %s)", count, threshold, worst)
	}
	return nil
}

func printReport(w io.Writer, s model.Snapshot) {
	c := s.Coverage
	fmt.Fprintf(w, "Coverage   visibility %s   declared %s   observed %s   (%d nodes, %d edges)\n\n",
		pct(c.Visibility), pct(c.Declared), pct(c.Observed), c.Nodes, c.Edges)

	fmt.Fprintln(w, "Sources")
	for _, src := range s.Sources {
		fmt.Fprintf(w, "  %-9s %-22s %3d nodes %3d edges  %s\n", src.State, src.Name, src.Nodes, src.Edges, src.Message)
	}

	fmt.Fprintln(w, "\nEdges")
	for _, e := range s.Edges {
		var traffic []string
		if e.RequestRate > 0 {
			traffic = append(traffic, fmt.Sprintf("%.2f req/s", e.RequestRate), fmt.Sprintf("%.1f%% errors", e.ErrorRate*100))
		}
		if e.LatencyP95Ms > 0 {
			traffic = append(traffic, "p95 "+model.FormatMs(e.LatencyP95Ms))
		}
		for _, r := range e.Reasons {
			traffic = append(traffic, "<- "+r.Summary)
		}
		fmt.Fprintf(w, "  %-6s %-40s %-11s %s\n", e.State, e.From+" -> "+e.To, e.Protocol, strings.Join(traffic, "  "))
	}

	fmt.Fprintf(w, "\nNext steps (%d)\n", len(s.Findings))
	for i, f := range s.Findings {
		fmt.Fprintf(w, "\n%2d. [%s/%s] %s\n", i+1, f.Severity, f.Kind, f.Title)
		if f.Detail != "" {
			fmt.Fprintf(w, "    %s\n", f.Detail)
		}
		if f.Fix != nil {
			fmt.Fprintf(w, "    -> %s\n", f.Fix.Summary)
			if f.Fix.Snippet != "" {
				for _, line := range strings.Split(strings.TrimRight(f.Fix.Snippet, "\n"), "\n") {
					fmt.Fprintf(w, "       %s\n", line)
				}
			}
		}
	}
}

// pct rounds half up, like the UI does.
func pct(v float64) string { return fmt.Sprintf("%3.0f%%", math.Round(v*100)) }

func export(args []string) error {
	if len(args) == 0 || args[0] != "matrix" {
		return errors.New("usage: teia export matrix [--config teia.yaml] [--lang en|pt-BR] [--declared-only]")
	}
	fs := flag.NewFlagSet("export matrix", flag.ExitOnError)
	path := fs.String("config", defaultConfig(), "config file (env TEIA_CONFIG)")
	lang := fs.String("lang", "en", "header language: en or pt-BR")
	declaredOnly := fs.Bool("declared-only", false, "only intended communication (what a firewall request should contain)")
	_ = fs.Parse(args[1:])

	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	snap := newEngine(cfg, slog.New(slog.NewTextHandler(io.Discard, nil))).Refresh(context.Background())
	return exp.Matrix(os.Stdout, snap, exp.MatrixOptions{Lang: *lang, DeclaredOnly: *declaredOnly})
}

func validate(files []string) error {
	if len(files) == 0 {
		return errors.New("usage: teia validate FILE...")
	}
	failed := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		docs, err := declared.Parse(data)
		if err != nil {
			fmt.Printf("FAIL %s: %v\n", file, err)
			failed++
			continue
		}
		if len(docs) == 0 {
			fmt.Printf("SKIP %s: no Flows document\n", file)
			continue
		}
		ok := true
		for _, doc := range docs {
			for _, verr := range declared.Validate(doc) {
				fmt.Printf("FAIL %s (%s): %v\n", file, doc.Metadata.Name, verr)
				ok = false
			}
		}
		if ok {
			fmt.Printf("ok   %s\n", file)
		} else {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d file(s) failed validation", failed)
	}
	return nil
}
