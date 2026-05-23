package mmdb

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const downloadBaseURL = "https://download.maxmind.com/app/geoip_download"

type edition struct {
	name string
	dest string
}

type Downloader struct {
	licenseKey string
	editions   []edition
	httpClient *http.Client
}

func New(licenseKey, cityPath, asnPath string) *Downloader {
	return &Downloader{
		licenseKey: licenseKey,
		editions: []edition{
			{name: "GeoLite2-City", dest: cityPath},
			{name: "GeoLite2-ASN", dest: asnPath},
		},
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

// EnsureFresh downloads any edition whose file is missing or older than maxAge.
func (d *Downloader) EnsureFresh(ctx context.Context, maxAge time.Duration) error {
	for _, e := range d.editions {
		stale, err := isStale(e.dest, maxAge)
		if err != nil {
			return err
		}
		if !stale {
			slog.Info("mmdb up to date", "edition", e.name)
			continue
		}
		slog.Info("downloading mmdb", "edition", e.name)
		if err := d.download(ctx, e); err != nil {
			return fmt.Errorf("download %s: %w", e.name, err)
		}
		slog.Info("mmdb downloaded", "edition", e.name)
	}
	return nil
}

// Run blocks until ctx is cancelled, refreshing on each interval tick.
func (d *Downloader) Run(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := d.EnsureFresh(ctx, interval); err != nil {
				slog.Error("mmdb refresh failed", "error", err)
			}
		}
	}
}

func (d *Downloader) download(ctx context.Context, e edition) error {
	url := fmt.Sprintf("%s?license_key=%s&edition_id=%s&suffix=tar.gz",
		downloadBaseURL, d.licenseKey, e.name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	tmp := e.dest + ".tmp"
	if err := extractMMDB(resp.Body, e.name, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, e.dest)
}

func extractMMDB(r io.Reader, editionName, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	target := editionName + ".mmdb"
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) != target {
			continue
		}
		if err := writeFile(tr, dest); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("%s not found in archive", target)
}

func writeFile(r io.Reader, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func isStale(path string, maxAge time.Duration) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	age := time.Since(info.ModTime())
	stale := age > maxAge
	if stale {
		slog.Info("mmdb stale", "path", path, "age", strings.TrimSpace(age.Round(time.Hour).String()))
	}
	return stale, nil
}
