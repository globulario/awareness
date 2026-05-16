package runtime

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MetricLevel represents warn vs. critical threshold.
type MetricLevel struct {
	Warn     float64 `yaml:"warn"`
	Critical float64 `yaml:"critical"`
}

// ServiceThresholds holds per-metric thresholds for a service.
type ServiceThresholds map[string]MetricLevel // metric name → thresholds

// MetricThresholdConfig is the top-level structure of metric_thresholds.yaml.
type MetricThresholdConfig struct {
	Thresholds map[string]ServiceThresholds `yaml:"thresholds"`
}

// defaultThresholds is the hard-coded fallback when no YAML is loaded.
var defaultThresholds = ServiceThresholds{
	"cpu_percent":    {Warn: 90, Critical: 97},
	"memory_percent": {Warn: 90, Critical: 97},
	"disk_percent":   {Warn: 90, Critical: 95},
}

// MetricThresholds is a loaded threshold configuration.
type MetricThresholds struct {
	cfg *MetricThresholdConfig
}

// LoadMetricThresholds loads from knowledge/metric_thresholds.yaml under docsDir.
// Returns a no-op instance (using hardcoded defaults) if the file cannot be read.
func LoadMetricThresholds(docsDir string) *MetricThresholds {
	if docsDir == "" {
		return &MetricThresholds{}
	}
	path := filepath.Join(docsDir, "knowledge", "metric_thresholds.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return &MetricThresholds{}
	}
	var cfg MetricThresholdConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return &MetricThresholds{}
	}
	return &MetricThresholds{cfg: &cfg}
}

// Evaluate checks a metric sample against thresholds and returns a warning string
// and severity ("warning", "critical", or "").
func (mt *MetricThresholds) Evaluate(sample MetricSample) (warning string, severity string) {
	metricKey := normalizeMetricKey(sample.Name)
	level, src := mt.lookup(sample.ServiceID, metricKey)
	if level == nil {
		return "", ""
	}
	var sev string
	if sample.Value >= level.Critical {
		sev = "critical"
	} else if sample.Value >= level.Warn {
		sev = "warning"
	} else {
		return "", ""
	}
	return buildMetricWarning(sample, *level, src, sev), sev
}

func (mt *MetricThresholds) lookup(serviceID, metricKey string) (*MetricLevel, string) {
	if mt.cfg != nil {
		// Try service-specific first.
		if svcThresh, ok := mt.cfg.Thresholds[serviceID]; ok {
			if lvl, ok := svcThresh[metricKey]; ok {
				return &lvl, serviceID
			}
		}
		// Fall back to default section.
		if defThresh, ok := mt.cfg.Thresholds["default"]; ok {
			if lvl, ok := defThresh[metricKey]; ok {
				return &lvl, "default"
			}
		}
	}
	// Hardcoded fallback.
	if lvl, ok := defaultThresholds[metricKey]; ok {
		return &lvl, "builtin"
	}
	return nil, ""
}

func normalizeMetricKey(name string) string {
	name = strings.ToLower(name)
	// Map common suffixes to canonical keys.
	switch {
	case strings.Contains(name, "cpu") && strings.Contains(name, "percent"):
		return "cpu_percent"
	case strings.Contains(name, "memory") && strings.Contains(name, "percent"):
		return "memory_percent"
	case strings.Contains(name, "disk") && strings.Contains(name, "percent"):
		return "disk_percent"
	case strings.Contains(name, "fsync") && strings.Contains(name, "latency"):
		return "fsync_latency_ms"
	case strings.Contains(name, "failed") && strings.Contains(name, "run"):
		return "failed_runs_15m"
	case strings.Contains(name, "blocked") && strings.Contains(name, "run"):
		return "blocked_runs"
	case strings.Contains(name, "reconcile") && strings.Contains(name, "lag"):
		return "reconcile_lag_seconds"
	case strings.Contains(name, "leader") && strings.Contains(name, "change"):
		return "leader_changes_1h"
	default:
		return name
	}
}

func buildMetricWarning(s MetricSample, level MetricLevel, threshSrc, severity string) string {
	return "metric " + severity + ": " + s.Name + "=" + formatFloat(s.Value) + s.Unit +
		" node=" + s.NodeID + " service=" + s.ServiceID +
		" (warn=" + formatFloat(level.Warn) + " critical=" + formatFloat(level.Critical) + " threshold_src=" + threshSrc + ")"
}

func formatFloat(f float64) string {
	if f == float64(int(f)) {
		return itoa(int(f))
	}
	// One decimal place.
	return ftoa(f)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	if i < 0 {
		return "-" + itoa(-i)
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

func ftoa(f float64) string {
	// Simple 1-decimal formatter.
	i := int(f)
	d := int((f - float64(i)) * 10)
	if d < 0 {
		d = -d
	}
	return itoa(i) + "." + itoa(d)
}
