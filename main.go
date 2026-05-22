package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/Crowley723/metrics-ip-enrichment/internal/config"
	"github.com/Crowley723/metrics-ip-enrichment/internal/enrich"
	"github.com/Crowley723/metrics-ip-enrichment/internal/health"
	"github.com/Crowley723/metrics-ip-enrichment/internal/job"
	"github.com/Crowley723/metrics-ip-enrichment/internal/mimir"
)

func main() {
	cfgPath := flag.String("c", "", "path to config file (required)")
	flag.Parse()

	if *cfgPath == "" {
		slog.Error("config file required: use -c <path>")
		os.Exit(1)
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	registry := enrich.DefaultRegistry
	if err := config.Validate(cfg, registry.KnownTypes()); err != nil {
		slog.Error("invalid config", "error", err)
		os.Exit(1)
	}

	paths := enrich.MMDBPaths{
		City: cfg.MMDB.City,
		ASN:  cfg.MMDB.ASN,
	}
	auth := mimir.BasicAuth{Username: cfg.Mimir.Username, Password: cfg.Mimir.Password}
	queryClient := mimir.NewQueryClient(cfg.Mimir.QueryURL, cfg.Mimir.OrgID, auth)
	writeClient := mimir.NewWriteClient(cfg.Mimir.PushURL, cfg.Mimir.OrgID, auth)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return health.Serve(ctx)
	})

	for _, j := range cfg.Jobs {
		j := j
		g.Go(func() error {
			return job.Run(ctx, j, queryClient, writeClient, registry, paths)
		})
	}

	slog.Info("enricher started", "jobs", len(cfg.Jobs))
	if err := g.Wait(); err != nil {
		slog.Error("enricher exited with error", "error", err)
		os.Exit(1)
	}
	slog.Info("enricher stopped")
}
