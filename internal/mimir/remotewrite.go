package mimir

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
)

// WriteClient sends remote_write requests to Mimir.
type WriteClient struct {
	pushURL   string
	orgID     string
	basicAuth BasicAuth
	http      *http.Client
}

func NewWriteClient(pushURL, orgID string, auth BasicAuth) *WriteClient {
	return &WriteClient{
		pushURL:   pushURL,
		orgID:     orgID,
		basicAuth: auth,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
}

// Write sends a batch of TimeSeries to Mimir via the remote_write protocol.
func (c *WriteClient) Write(ctx context.Context, ts []prompb.TimeSeries) error {
	wr := &prompb.WriteRequest{Timeseries: ts}
	data, err := wr.Marshal()
	if err != nil {
		return fmt.Errorf("marshal WriteRequest: %w", err)
	}

	compressed := snappy.Encode(nil, data)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.pushURL, bytes.NewReader(compressed))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	req.Header.Set("X-Scope-OrgID", c.orgID)
	if c.basicAuth.Username != "" {
		req.SetBasicAuth(c.basicAuth.Username, c.basicAuth.Password)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send remote_write: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("remote_write returned %d: %s", resp.StatusCode, body)
	}
	return nil
}
