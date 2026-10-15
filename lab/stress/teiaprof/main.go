// Command teiaprof runs teia's server, as teia serve does, with Go's profiler
// on a separate localhost address, for the capacity runs in lab/stress. It is
// a measurement tool and is not shipped: teia serve has no profiler.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"github.com/nilum-br/teia/internal/api"
	"github.com/nilum-br/teia/internal/config"
	"github.com/nilum-br/teia/internal/engine"
)

func main() {
	path := flag.String("config", "teia.yaml", "config file")
	profiler := flag.String("pprof", "127.0.0.1:6060", "profiler address (keep it on localhost)")
	flag.Parse()
	cfg, err := config.Load(*path)
	if err != nil {
		slog.Error("loading config", "err", err)
		os.Exit(1)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var clusters []api.Cluster
	for _, scope := range cfg.BuildScopes() {
		eng := engine.New(scope.Sources, cfg.EngineOptions(scope.Cluster, nil), log.With("cluster", scope.Cluster))
		go eng.Run(ctx)
		clusters = append(clusters, api.Cluster{Name: scope.Cluster, Engine: eng})
	}

	debug := http.NewServeMux()
	debug.HandleFunc("/debug/pprof/", pprof.Index)
	debug.HandleFunc("/debug/pprof/profile", pprof.Profile)
	go func() { _ = http.ListenAndServe(*profiler, debug) }()

	srv := &http.Server{Addr: cfg.Listen, Handler: api.Clusters(clusters, api.Options{MaxStreams: cfg.MaxStreams, Log: log, Version: "stress"})}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		stop()
		slog.Error("serving", "err", err)
		os.Exit(1) //nolint:gocritic // stop() already ran
	}
}
