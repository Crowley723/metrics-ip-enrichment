package mimir

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Sample is one result series from a Mimir instant query.
type Sample struct {
	Labels    map[string]string
	Value     float64
	Timestamp time.Time
}

// BasicAuth holds optional HTTP basic auth credentials.
type BasicAuth struct {
	Username string
	Password string
}

// QueryClient performs instant queries against the Mimir query frontend.
type QueryClient struct {
	baseURL   string
	orgID     string
	basicAuth BasicAuth
	http      *http.Client
}

func NewQueryClient(baseURL, orgID string, auth BasicAuth) *QueryClient {
	return &QueryClient{
		baseURL:   baseURL,
		orgID:     orgID,
		basicAuth: auth,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
}

// Instant executes a PromQL instant query and returns all vector samples.
func (c *QueryClient) Instant(ctx context.Context, query string) ([]Sample, error) {
	endpoint := c.baseURL + "/api/v1/query"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	q := url.Values{}
	q.Set("query", query)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("X-Scope-OrgID", c.orgID)
	if c.basicAuth.Username != "" {
		req.SetBasicAuth(c.basicAuth.Username, c.basicAuth.Password)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute query: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query returned %d: %s", resp.StatusCode, body)
	}

	return parseQueryResponse(body)
}

type queryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]json.RawMessage `json:"value"`
		} `json:"result"`
	} `json:"data"`
	Error string `json:"error"`
}

func parseQueryResponse(body []byte) ([]Sample, error) {
	var r queryResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("query error: %s", r.Error)
	}
	if r.Data.ResultType != "vector" {
		return nil, fmt.Errorf("expected resultType=vector, got %q", r.Data.ResultType)
	}

	samples := make([]Sample, 0, len(r.Data.Result))
	for _, res := range r.Data.Result {
		var tsFloat float64
		if err := json.Unmarshal(res.Value[0], &tsFloat); err != nil {
			return nil, fmt.Errorf("parse timestamp: %w", err)
		}
		var valStr string
		if err := json.Unmarshal(res.Value[1], &valStr); err != nil {
			return nil, fmt.Errorf("parse value: %w", err)
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			return nil, fmt.Errorf("parse value float: %w", err)
		}
		samples = append(samples, Sample{
			Labels:    res.Metric,
			Value:     val,
			Timestamp: time.Unix(int64(tsFloat), 0),
		})
	}
	return samples, nil
}
