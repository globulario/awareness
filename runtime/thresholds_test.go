package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultThresholds_CpuWarn(t *testing.T) {
	mt := &MetricThresholds{}
	s := MetricSample{Name: "node_cpu_percent", Value: 91, Unit: "percent", NodeID: "node1", ServiceID: "node"}
	w, sev := mt.Evaluate(s)
	if sev != "warning" {
		t.Errorf("expected warning severity, got %q", sev)
	}
	if !strings.Contains(w, "warning") {
		t.Errorf("expected warning in message, got %q", w)
	}
	if !strings.Contains(w, "node_cpu_percent") {
		t.Errorf("expected metric name in message, got %q", w)
	}
}

func TestDefaultThresholds_CpuCritical(t *testing.T) {
	mt := &MetricThresholds{}
	s := MetricSample{Name: "node_cpu_percent", Value: 98, Unit: "percent", NodeID: "node1", ServiceID: "node"}
	w, sev := mt.Evaluate(s)
	if sev != "critical" {
		t.Errorf("expected critical severity, got %q", sev)
	}
	if !strings.Contains(w, "critical") {
		t.Errorf("expected critical in message, got %q", w)
	}
}

func TestDefaultThresholds_DiskBelow(t *testing.T) {
	mt := &MetricThresholds{}
	s := MetricSample{Name: "node_disk_percent", Value: 50, Unit: "percent", NodeID: "node1", ServiceID: "node"}
	w, sev := mt.Evaluate(s)
	if sev != "" {
		t.Errorf("expected no severity for low disk, got %q (warning: %q)", sev, w)
	}
}

func TestDefaultThresholds_MemoryWarn(t *testing.T) {
	mt := &MetricThresholds{}
	s := MetricSample{Name: "node_memory_percent", Value: 92, Unit: "percent", NodeID: "node1", ServiceID: "node"}
	_, sev := mt.Evaluate(s)
	if sev != "warning" {
		t.Errorf("expected warning, got %q", sev)
	}
}

func TestServiceSpecificThreshold_EtcdDisk(t *testing.T) {
	// Create a temp thresholds file with etcd-specific disk threshold.
	dir := t.TempDir()
	knowledgeDir := filepath.Join(dir, "knowledge")
	if err := os.MkdirAll(knowledgeDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `
thresholds:
  default:
    disk_percent:
      warn: 90
      critical: 95
  etcd:
    disk_percent:
      warn: 70
      critical: 80
`
	if err := os.WriteFile(filepath.Join(knowledgeDir, "metric_thresholds.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	mt := LoadMetricThresholds(dir)

	// etcd disk at 72% should warn (threshold 70) not use default 90.
	s := MetricSample{Name: "node_disk_percent", Value: 72, Unit: "percent", NodeID: "etcd-node", ServiceID: "etcd"}
	w, sev := mt.Evaluate(s)
	if sev != "warning" {
		t.Errorf("expected warning for etcd disk at 72%% (etcd threshold=70), got %q (msg: %q)", sev, w)
	}
	if !strings.Contains(w, "threshold_src=etcd") {
		t.Errorf("expected threshold_src=etcd in message, got %q", w)
	}

	// A non-etcd service disk at 72% should NOT warn (default threshold=90).
	s2 := MetricSample{Name: "node_disk_percent", Value: 72, Unit: "percent", NodeID: "other-node", ServiceID: "other"}
	_, sev2 := mt.Evaluate(s2)
	if sev2 != "" {
		t.Errorf("expected no warning for non-etcd service at 72%%, got %q", sev2)
	}
}

func TestThresholdWarning_IncludesThresholdSource(t *testing.T) {
	mt := &MetricThresholds{}
	s := MetricSample{Name: "node_cpu_percent", Value: 95, Unit: "percent", NodeID: "n1", ServiceID: "node"}
	w, _ := mt.Evaluate(s)
	if !strings.Contains(w, "threshold_src=builtin") {
		t.Errorf("expected threshold_src=builtin in message, got %q", w)
	}
}

func TestNormalizeMetricKey(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"node_cpu_percent", "cpu_percent"},
		{"node_memory_percent", "memory_percent"},
		{"node_disk_percent", "disk_percent"},
		{"etcd_fsync_latency_ms", "fsync_latency_ms"},
		{"workflow_failed_runs_15m", "failed_runs_15m"},
		{"blocked_runs", "blocked_runs"},
		{"reconcile_lag_seconds", "reconcile_lag_seconds"},
		{"etcd_leader_changes_1h", "leader_changes_1h"},
		{"custom_metric", "custom_metric"},
	}
	for _, c := range cases {
		got := normalizeMetricKey(c.input)
		if got != c.expected {
			t.Errorf("normalizeMetricKey(%q) = %q, want %q", c.input, got, c.expected)
		}
	}
}

func TestLoadMetricThresholds_MissingFile(t *testing.T) {
	// Should return empty thresholds without panicking.
	mt := LoadMetricThresholds("/nonexistent/path")
	if mt == nil {
		t.Fatal("expected non-nil MetricThresholds even on missing file")
	}
	// Should still use builtin defaults.
	s := MetricSample{Name: "node_cpu_percent", Value: 95, Unit: "percent"}
	_, sev := mt.Evaluate(s)
	if sev != "warning" {
		t.Errorf("expected warning from builtin defaults, got %q", sev)
	}
}

// TestLoadMetricThresholds_ServiceSpecificOverridesDefault verifies that
// a service-specific threshold overrides the default.
func TestLoadMetricThresholds_ServiceSpecificOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	knowledgeDir := filepath.Join(dir, "knowledge")
	if err := os.MkdirAll(knowledgeDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `
thresholds:
  default:
    disk_percent:
      warn: 90
      critical: 95
  etcd:
    disk_percent:
      warn: 75
      critical: 82
`
	if err := os.WriteFile(filepath.Join(knowledgeDir, "metric_thresholds.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	mt := LoadMetricThresholds(dir)

	// etcd disk at 76% should warn (etcd threshold 75), not use default 90.
	s := MetricSample{Name: "node_disk_percent", Value: 76, Unit: "percent", NodeID: "etcd-node", ServiceID: "etcd"}
	w, sev := mt.Evaluate(s)
	if sev != "warning" {
		t.Errorf("expected warning for etcd disk at 76%% (etcd threshold=75), got %q (msg: %q)", sev, w)
	}
	if !strings.Contains(w, "threshold_src=etcd") {
		t.Errorf("expected threshold_src=etcd in message, got %q", w)
	}

	// etcd disk at 82% should be critical.
	s2 := MetricSample{Name: "node_disk_percent", Value: 82, Unit: "percent", NodeID: "etcd-node", ServiceID: "etcd"}
	_, sev2 := mt.Evaluate(s2)
	if sev2 != "critical" {
		t.Errorf("expected critical for etcd disk at 82%% (critical threshold=82), got %q", sev2)
	}

	// node disk at 91% should warn (default threshold=90).
	s3 := MetricSample{Name: "node_disk_percent", Value: 91, Unit: "percent", NodeID: "n1", ServiceID: "node"}
	_, sev3 := mt.Evaluate(s3)
	if sev3 != "warning" {
		t.Errorf("expected warning for node disk at 91%% (default threshold=90), got %q", sev3)
	}
}

// TestLoadMetricThresholds_MissingYAMLDegradesGracefully verifies that loading
// from a path with no file returns usable defaults without panic.
func TestLoadMetricThresholds_MissingYAMLDegradesGracefully(t *testing.T) {
	mt := LoadMetricThresholds("/nonexistent/path/xyz")
	if mt == nil {
		t.Fatal("expected non-nil MetricThresholds on missing file")
	}
	// Should use builtin defaults — CPU at 95% should warn.
	s := MetricSample{Name: "node_cpu_percent", Value: 95, Unit: "percent", NodeID: "n1", ServiceID: "node"}
	_, sev := mt.Evaluate(s)
	if sev != "warning" {
		t.Errorf("expected warning from builtin defaults on missing YAML, got %q", sev)
	}
}

func TestFormatFloat(t *testing.T) {
	cases := []struct {
		f    float64
		want string
	}{
		{0, "0"},
		{90, "90"},
		{97, "97"},
		{90.5, "90.5"},
		{-1, "-1"},
	}
	for _, c := range cases {
		got := formatFloat(c.f)
		if got != c.want {
			t.Errorf("formatFloat(%v) = %q, want %q", c.f, got, c.want)
		}
	}
}
