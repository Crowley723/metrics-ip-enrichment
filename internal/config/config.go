package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Mimir MimirConfig `yaml:"mimir"`
	MMDB  MMDBConfig  `yaml:"mmdb"`
	Jobs  []JobConfig `yaml:"jobs"`
}

type MimirConfig struct {
	QueryURL string `yaml:"query_url"`
	PushURL  string `yaml:"push_url"`
	OrgID    string `yaml:"org_id"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type MMDBConfig struct {
	City string `yaml:"city"`
	ASN  string `yaml:"asn"`
}

type JobConfig struct {
	Name         string        `yaml:"name"`
	SourceQuery  string        `yaml:"source_query"`
	OutputMetric string        `yaml:"output_metric"`
	IPLabel      string        `yaml:"ip_label"`
	Interval     time.Duration `yaml:"interval"`
	Enrichers    []string      `yaml:"enrichers"`
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return &cfg, nil
}

// Validate checks all fields eagerly, returning a joined error with every problem found.
// registry is a set of known enricher type names.
func Validate(cfg *Config, registry map[string]bool) error {
	var errs []error

	if err := validateURL("mimir.query_url", cfg.Mimir.QueryURL); err != nil {
		errs = append(errs, err)
	}
	if err := validateURL("mimir.push_url", cfg.Mimir.PushURL); err != nil {
		errs = append(errs, err)
	}
	if cfg.Mimir.OrgID == "" {
		errs = append(errs, errors.New("mimir.org_id is required"))
	}

	if err := validateFile("mmdb.city", cfg.MMDB.City); err != nil {
		errs = append(errs, err)
	}
	if err := validateFile("mmdb.asn", cfg.MMDB.ASN); err != nil {
		errs = append(errs, err)
	}

	if len(cfg.Jobs) == 0 {
		errs = append(errs, errors.New("jobs: at least one job is required"))
	}
	for i, job := range cfg.Jobs {
		prefix := fmt.Sprintf("jobs[%d](%s)", i, job.Name)
		if job.Name == "" {
			errs = append(errs, fmt.Errorf("%s: name is required", prefix))
		}
		if job.SourceQuery == "" {
			errs = append(errs, fmt.Errorf("%s: source_query is required", prefix))
		}
		if job.OutputMetric == "" {
			errs = append(errs, fmt.Errorf("%s: output_metric is required", prefix))
		}
		if job.IPLabel == "" {
			errs = append(errs, fmt.Errorf("%s: ip_label is required", prefix))
		}
		if job.Interval <= 0 {
			errs = append(errs, fmt.Errorf("%s: interval must be > 0", prefix))
		}
		if len(job.Enrichers) == 0 {
			errs = append(errs, fmt.Errorf("%s: enrichers list is empty", prefix))
		}
		for _, e := range job.Enrichers {
			if !registry[e] {
				errs = append(errs, fmt.Errorf("%s: unknown enricher type %q", prefix, e))
			}
		}
	}

	return errors.Join(errs...)
}

func validateURL(field, raw string) error {
	if raw == "" {
		return fmt.Errorf("%s is required", field)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: invalid URL: %w", field, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s: URL must have scheme and host", field)
	}
	return nil
}

func validateFile(field, path string) error {
	if path == "" {
		return fmt.Errorf("%s is required", field)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%s: cannot open %q: %w", field, path, err)
	}
	f.Close()
	return nil
}
