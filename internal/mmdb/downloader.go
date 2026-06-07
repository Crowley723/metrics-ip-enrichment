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
// For files that exist but are past maxAge, a HEAD request is issued first to
// check whether MaxMind has published a newer build. HEAD requests do not count
// against the daily download limit.
func (d *Downloader) EnsureFresh(ctx context.Context, maxAge time.Duration) error {
	for _, e := range d.editions {
		stale, localMod, err := isStale(e.dest, maxAge)
		if err != nil {
			return err
		}
		if !stale {
			slog.Info("mmdb up to date", "edition", e.name)
			continue
		}

		var remoteMod time.Time
		if !localMod.IsZero() {
			// File exists but is past maxAge — check remote before downloading.
			remoteMod, err = d.headLastModified(ctx, e)
			if err != nil {
				slog.Warn("could not check remote version, proceeding with download", "edition", e.name, "error", err)
			} else if !remoteMod.After(localMod) {
				// MaxMind hasn't published a newer build — bump mtime to reset the timer.
				now := time.Now()
				_ = os.Chtimes(e.dest, now, now)
				slog.Info("mmdb current (remote unchanged)", "edition", e.name, "remote_built", remoteMod.Format(time.RFC3339))
				continue
			}
		}

		slog.Info("downloading mmdb", "edition", e.name)
		downloaded, err := d.download(ctx, e)
		if err != nil {
			return fmt.Errorf("download %s: %w", e.name, err)
		}
		// Set mtime to the remote build date so future HEAD comparisons are accurate.
		if !downloaded.IsZero() {
			_ = os.Chtimes(e.dest, downloaded, downloaded)
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

// headLastModified issues a HEAD request and returns the remote Last-Modified
// time. HEAD requests do not count against MaxMind's daily download limit.
func (d *Downloader) headLastModified(ctx context.Context, e edition) (time.Time, error) {
	url := fmt.Sprintf("%s?license_key=%s&edition_id=%s&suffix=tar.gz",
		downloadBaseURL, d.licenseKey, e.name)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return time.Time{}, err
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("HEAD %s: unexpected status %d", e.name, resp.StatusCode)
	}
	lm := resp.Header.Get("Last-Modified")
	if lm == "" {
		return time.Time{}, fmt.Errorf("HEAD %s: no Last-Modified header", e.name)
	}
	t, err := http.ParseTime(lm)
	if err != nil {
		return time.Time{}, fmt.Errorf("HEAD %s: parse Last-Modified %q: %w", e.name, lm, err)
	}
	return t, nil
}

// download fetches and extracts the edition archive, returning the remote
// Last-Modified time so the caller can stamp the local file accordingly.
func (d *Downloader) download(ctx context.Context, e edition) (time.Time, error) {
	url := fmt.Sprintf("%s?license_key=%s&edition_id=%s&suffix=tar.gz",
		downloadBaseURL, d.licenseKey, e.name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return time.Time{}, err
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var remoteMod time.Time
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		if t, err := http.ParseTime(lm); err == nil {
			remoteMod = t
		}
	}

	tmp := e.dest + ".tmp"
	if err := extractMMDB(resp.Body, e.name, tmp); err != nil {
		os.Remove(tmp)
		return time.Time{}, err
	}
	if err := os.Rename(tmp, e.dest); err != nil {
		return time.Time{}, err
	}
	return remoteMod, nil
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

// isStale reports whether the file at path is missing or older than maxAge.
// It also returns the file's modification time (zero if the file is missing).
func isStale(path string, maxAge time.Duration) (bool, time.Time, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return true, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, err
	}
	age := time.Since(info.ModTime())
	stale := age > maxAge
	if stale {
		slog.Info("mmdb stale", "path", path, "age", strings.TrimSpace(age.Round(time.Hour).String()))
	}
	return stale, info.ModTime(), nil
}
