package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// PrometheusMetricsSource queries a Prometheus HTTP API for metric samples.
// It loads queries from knowledge/metric_queries.yaml.
type PrometheusMetricsSource struct {
	baseURL    string // e.g. "http://10.0.0.63:9090"
	httpClient *http.Client
	queries    []metricQuery
}

type metricQuery struct {
	ID      string `yaml:"id"`
	Query   string `yaml:"query"`
	Service string `yaml:"service"`
	Unit    string `yaml:"unit"`
}

type metricQueryConfig struct {
	Queries []metricQuery `yaml:"queries"`
}

// NewPrometheusMetricsSource creates a source that queries Prometheus at baseURL.
// queriesFile is the path to metric_queries.yaml; if empty, default queries are used.
func NewPrometheusMetricsSource(baseURL, queriesFile string) (*PrometheusMetricsSource, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("prometheus source: baseURL is empty")
	}
	queries := defaultMetricQueries()
	if queriesFile != "" {
		if loaded, err := loadMetricQueries(queriesFile); err == nil && len(loaded) > 0 {
			queries = loaded
		}
	}
	return &PrometheusMetricsSource{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		queries:    queries,
	}, nil
}

// SourceInfo implements sourceIdentifier.
func (s *PrometheusMetricsSource) SourceInfo() (string, bool) { return "prometheus.http", false }

// Samples executes all configured queries and returns the combined samples.
func (s *PrometheusMetricsSource) Samples(ctx context.Context) ([]MetricSample, error) {
	var out []MetricSample
	var lastErr error
	for _, q := range s.queries {
		samples, err := s.query(ctx, q)
		if err != nil {
			lastErr = err
			continue
		}
		out = append(out, samples...)
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

func (s *PrometheusMetricsSource) query(ctx context.Context, q metricQuery) ([]MetricSample, error) {
	apiURL := s.baseURL + "/api/v1/query"
	params := url.Values{"query": {q.Query}, "time": {strconv.FormatInt(time.Now().Unix(), 10)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result promResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse prometheus response: %w", err)
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("prometheus query error: %s", result.Error)
	}

	var out []MetricSample
	for _, r := range result.Data.Result {
		if len(r.Value) < 2 {
			continue
		}
		valStr, ok := r.Value[1].(string)
		if !ok {
			continue
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		nodeID := r.Metric["instance"]
		serviceID := q.Service
		if svc, ok := r.Metric["job"]; ok {
			serviceID = svc
		}
		out = append(out, MetricSample{
			Name:      q.ID,
			NodeID:    nodeID,
			ServiceID: serviceID,
			Value:     val,
			Unit:      q.Unit,
			Labels:    r.Metric,
		})
	}
	return out, nil
}

type promResponse struct {
	Status string   `json:"status"`
	Error  string   `json:"error,omitempty"`
	Data   promData `json:"data"`
}

type promData struct {
	ResultType string       `json:"resultType"`
	Result     []promResult `json:"result"`
}

type promResult struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value"` // [timestamp, "value_string"]
}

func loadMetricQueries(path string) ([]metricQuery, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg metricQueryConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg.Queries, nil
}

func defaultMetricQueriesFromDir(docsDir string) []metricQuery {
	if docsDir == "" {
		return defaultMetricQueries()
	}
	path := filepath.Join(docsDir, "knowledge", "metric_queries.yaml")
	if q, err := loadMetricQueries(path); err == nil && len(q) > 0 {
		return q
	}
	return defaultMetricQueries()
}

func defaultMetricQueries() []metricQuery {
	return []metricQuery{
		{ID: "node_cpu_percent", Query: `100 - (avg by(instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`, Service: "node", Unit: "percent"},
		{ID: "node_memory_percent", Query: `(1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)) * 100`, Service: "node", Unit: "percent"},
		{ID: "node_disk_percent", Query: `100 * (1 - node_filesystem_avail_bytes{fstype!="tmpfs"} / node_filesystem_size_bytes{fstype!="tmpfs"})`, Service: "node", Unit: "percent"},
		{ID: "etcd_fsync_latency_ms", Query: `histogram_quantile(0.99, rate(etcd_disk_wal_fsync_duration_seconds_bucket[5m])) * 1000`, Service: "etcd", Unit: "ms"},
	}
}
