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
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nilum-br/teia/internal/api"
	"github.com/nilum-br/teia/internal/config"
	"github.com/nilum-br/teia/internal/engine"
	exp "github.com/nilum-br/teia/internal/export"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source/declared"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `teia maps service communication and tells you where it is blind.

Usage:
  teia serve    [--config teia.yaml]     run discovery continuously and serve the API and UI
  teia discover [--config teia.yaml]     run one discovery round and print the report

The config path defaults to $TEIA_CONFIG, then ./teia.yaml.
  teia export matrix [--config teia.yaml] [--lang pt-BR] [--declared-only]
                                         flow matrix (CSV) for firewall and security reviews
  teia validate FILE...                  check flows files
  teia version
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
	case "export":
		err = export(os.Args[2:])
	case "validate":
		err = validate(os.Args[2:])
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

func newEngine(cfg *config.Config, log *slog.Logger) *engine.Engine {
	return engine.New(cfg.BuildSources(), engine.Options{
		Interval: cfg.Interval,
		Timeout:  cfg.Timeout,
		Aliases:  cfg.Aliases,
		Ignore:   cfg.Ignore,
	}, log)
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	path := fs.String("config", defaultConfig(), "config file (env TEIA_CONFIG)")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	log := newLogger()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	eng := newEngine(cfg, log)
	go eng.Run(ctx)

	srv := &http.Server{Addr: cfg.Listen, Handler: api.Handler(eng), ReadHeaderTimeout: 5 * time.Second}
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

func discover(args []string) error {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	path := fs.String("config", defaultConfig(), "config file (env TEIA_CONFIG)")
	output := fs.String("output", "text", "text or json")
	failOn := fs.String("fail-on", "", "exit 1 if a finding of this severity or worse exists: critical, warning, info")
	_ = fs.Parse(args)

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

	if *failOn != "" {
		threshold := model.Severity(*failOn).Rank()
		for _, f := range snap.Findings {
			if f.Severity.Rank() <= threshold {
				return fmt.Errorf("found %s finding(s) at or above %q", f.Kind, *failOn)
			}
		}
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
		rate := ""
		if e.RequestRate > 0 {
			rate = fmt.Sprintf("%.2f req/s", e.RequestRate)
		}
		fmt.Fprintf(w, "  %-6s %-40s %-11s %s\n", e.State, e.From+" -> "+e.To, e.Protocol, rate)
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
