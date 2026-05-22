package job

import (
	"context"
	"log/slog"
	"net"
	"sort"
	"time"

	"github.com/Crowley723/metrics-ip-enrichment/internal/config"
	"github.com/Crowley723/metrics-ip-enrichment/internal/enrich"
	"github.com/Crowley723/metrics-ip-enrichment/internal/mimir"
	"github.com/prometheus/prometheus/prompb"
)

// Run executes the query→enrich→push loop for a single job until ctx is cancelled.
func Run(
	ctx context.Context,
	job config.JobConfig,
	query *mimir.QueryClient,
	rw *mimir.WriteClient,
	registry enrich.Registry,
	paths enrich.MMDBPaths,
) error {
	log := slog.With("job", job.Name)
	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runCycle(ctx, job, query, rw, registry, paths, log)
		}
	}
}

func runCycle(
	ctx context.Context,
	job config.JobConfig,
	query *mimir.QueryClient,
	rw *mimir.WriteClient,
	registry enrich.Registry,
	paths enrich.MMDBPaths,
	log *slog.Logger,
) {
	enrichers, err := buildEnrichers(job.Enrichers, registry, paths)
	if err != nil {
		log.Error("failed to open enrichers", "error", err)
		return
	}
	defer closeAll(enrichers, log)

	samples, err := query.Instant(ctx, job.SourceQuery)
	if err != nil {
		log.Error("query failed, skipping cycle", "error", err)
		return
	}

	ts := make([]prompb.TimeSeries, 0, len(samples))
	for _, s := range samples {
		ip := net.ParseIP(s.Labels[job.IPLabel])
		if ip == nil {
			log.Warn("could not parse IP, enriching with unknown", "label", job.IPLabel, "value", s.Labels[job.IPLabel])
		}

		labels := make(map[string]string, len(s.Labels)+8)
		for k, v := range s.Labels {
			labels[k] = v
		}
		labels["__name__"] = job.OutputMetric

		for _, e := range enrichers {
			for k, v := range e.Labels(ip) {
				labels[k] = v
			}
		}

		ts = append(ts, buildTimeSeries(labels, s.Value, s.Timestamp))
	}

	if len(ts) == 0 {
		log.Info("no samples in this cycle, nothing to push")
		return
	}

	if err := rw.Write(ctx, ts); err != nil {
		log.Error("remote_write failed", "error", err)
		return
	}
	log.Info("cycle complete", "series_pushed", len(ts))
}

func buildEnrichers(names []string, registry enrich.Registry, paths enrich.MMDBPaths) ([]enrich.Enricher, error) {
	result := make([]enrich.Enricher, 0, len(names))
	for _, name := range names {
		ctor := registry[name]
		e, err := ctor(paths)
		if err != nil {
			// Close any already-opened enrichers before returning.
			for _, opened := range result {
				opened.Close()
			}
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

func closeAll(enrichers []enrich.Enricher, log *slog.Logger) {
	for _, e := range enrichers {
		if err := e.Close(); err != nil {
			log.Warn("enricher close error", "enricher", e.Name(), "error", err)
		}
	}
}

func buildTimeSeries(labels map[string]string, value float64, ts time.Time) prompb.TimeSeries {
	pbLabels := make([]prompb.Label, 0, len(labels))
	for k, v := range labels {
		pbLabels = append(pbLabels, prompb.Label{Name: k, Value: v})
	}
	// Mimir requires labels sorted by name.
	sort.Slice(pbLabels, func(i, j int) bool {
		return pbLabels[i].Name < pbLabels[j].Name
	})

	return prompb.TimeSeries{
		Labels: pbLabels,
		Samples: []prompb.Sample{
			{Value: value, Timestamp: ts.UnixMilli()},
		},
	}
}
