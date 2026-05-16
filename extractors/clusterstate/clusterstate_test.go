package clusterstate_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/awareness/extractors/clusterstate"
	"github.com/globulario/awareness/graph"
)

func openTestGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hashStr(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// --- systemd tests ---

func TestSystemdCollector_SkipsMissingUnitsGracefully(t *testing.T) {
	orig := clusterstate.SystemdDir
	clusterstate.SystemdDir = "/nonexistent/systemd/system"
	defer func() { clusterstate.SystemdDir = orig }()

	g := openTestGraph(t)
	health, err := clusterstate.CollectSystemd(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != "skipped" {
		t.Errorf("expected status=skipped, got %q", health.Status)
	}
}

func TestSystemdCollector_ParsesUnitFile(t *testing.T) {
	systemdDir := t.TempDir()
	orig := clusterstate.SystemdDir
	clusterstate.SystemdDir = systemdDir
	defer func() { clusterstate.SystemdDir = orig }()

	content := "[Service]\nExecStart=/usr/lib/globular/bin/minio server /data\n"
	writeFile(t, filepath.Join(systemdDir, "globular-minio.service"), content)
	// Write matching sidecar.
	writeFile(t, filepath.Join(systemdDir, "globular-minio.service.sha256"), hashStr(content)+"\n")

	g := openTestGraph(t)
	health, err := clusterstate.CollectSystemd(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("expected status=ok, got %q: %s", health.Status, health.Error)
	}
	if health.NodesEmitted == 0 {
		t.Error("expected nodes emitted")
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, graph.NodeTypeSystemdUnit)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range nodes {
		if n.ID == "unit:globular-minio.service" {
			found = true
			if n.Metadata["source_tier"] != "systemd_runtime" {
				t.Errorf("expected source_tier=systemd_runtime, got %v", n.Metadata["source_tier"])
			}
		}
	}
	if !found {
		t.Error("unit:globular-minio.service not found in graph")
	}
}

func TestSystemdCollector_DetectsSidecarMismatch(t *testing.T) {
	systemdDir := t.TempDir()
	orig := clusterstate.SystemdDir
	clusterstate.SystemdDir = systemdDir
	defer func() { clusterstate.SystemdDir = orig }()

	writeFile(t, filepath.Join(systemdDir, "globular-minio.service"), "[Service]\nExecStart=/bin/minio\n")
	// Intentionally wrong sidecar hash.
	writeFile(t, filepath.Join(systemdDir, "globular-minio.service.sha256"), "deadbeefdeadbeefdeadbeefdeadbeef\n")

	g := openTestGraph(t)
	_, err := clusterstate.CollectSystemd(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, graph.NodeTypeSystemdUnit)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.ID == "unit:globular-minio.service" {
			match, _ := n.Metadata["sidecar_match"].(bool)
			if match {
				t.Error("expected sidecar_match=false for mismatched sidecar")
			}
		}
	}
}

func TestSystemdCollector_ReadsDropInOverride(t *testing.T) {
	systemdDir := t.TempDir()
	orig := clusterstate.SystemdDir
	clusterstate.SystemdDir = systemdDir
	defer func() { clusterstate.SystemdDir = orig }()

	writeFile(t, filepath.Join(systemdDir, "globular-minio.service"), "[Service]\nExecStart=/bin/minio\n")
	writeFile(t, filepath.Join(systemdDir, "globular-minio.service.d", "distributed.conf"), "[Service]\nEnvironmentFile=/etc/minio.env\n")

	g := openTestGraph(t)
	_, err := clusterstate.CollectSystemd(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, graph.NodeTypeSystemdUnit)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.ID == "unit:globular-minio.service" {
			dropin := n.Metadata["dropin_count"]
			// SQLite round-trips integers as int64; JSON as float64.
			var count float64
			switch v := dropin.(type) {
			case int64:
				count = float64(v)
			case float64:
				count = v
			case int:
				count = float64(v)
			}
			if count < 1 {
				t.Errorf("expected dropin_count >= 1, got %v", dropin)
			}
		}
	}
}

func TestSystemdCollector_EmitsActiveStateNode(t *testing.T) {
	systemdDir := t.TempDir()
	orig := clusterstate.SystemdDir
	clusterstate.SystemdDir = systemdDir
	defer func() { clusterstate.SystemdDir = orig }()

	writeFile(t, filepath.Join(systemdDir, "globular-workflow.service"), "[Service]\nExecStart=/bin/workflow\n")

	g := openTestGraph(t)
	health, err := clusterstate.CollectSystemd(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("expected ok, got %q: %s", health.Status, health.Error)
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, graph.NodeTypeSystemdUnit)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range nodes {
		if n.ID == "unit:globular-workflow.service" {
			found = true
		}
	}
	if !found {
		t.Error("unit:globular-workflow.service not found")
	}
}

// --- varlib tests ---

func TestVarLibScanner_SkipsPrivateKeys(t *testing.T) {
	varLibDir := t.TempDir()
	orig := clusterstate.VarLibDir
	clusterstate.VarLibDir = varLibDir
	defer func() { clusterstate.VarLibDir = orig }()

	pkiDir := filepath.Join(varLibDir, "pki", "issued", "services")
	writeFile(t, filepath.Join(pkiDir, "service.key"), "FAKE PRIVATE KEY — MUST NOT BE READ")
	writeFile(t, filepath.Join(pkiDir, "service.crt"), generateTestCert(t, []string{"globular.internal"}, nil))

	g := openTestGraph(t)
	health, err := clusterstate.CollectVarLib(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("expected ok, got %q: %s", health.Status, health.Error)
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, "pki_certificate")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Name == "service.key" {
			t.Error("private key file was indexed — safety violation")
		}
	}
	// The .crt file should have been indexed.
	if len(nodes) == 0 {
		t.Error("expected at least one pki_certificate node for the .crt file")
	}
}

func TestVarLibScanner_ParsesCertExpiry(t *testing.T) {
	varLibDir := t.TempDir()
	orig := clusterstate.VarLibDir
	clusterstate.VarLibDir = varLibDir
	defer func() { clusterstate.VarLibDir = orig }()

	pkiDir := filepath.Join(varLibDir, "pki", "issued", "services")
	writeFile(t, filepath.Join(pkiDir, "service.crt"), generateTestCert(t, []string{"globular.internal"}, nil))

	g := openTestGraph(t)
	_, err := clusterstate.CollectVarLib(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, "pki_certificate")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("no pki_certificate nodes found")
	}
	if nodes[0].Metadata["days_remaining"] == nil {
		t.Error("days_remaining not set on cert node")
	}
	if nodes[0].Metadata["expiry"] == nil {
		t.Error("expiry not set on cert node")
	}
}

func TestVarLibScanner_ParsesCertSANs(t *testing.T) {
	varLibDir := t.TempDir()
	orig := clusterstate.VarLibDir
	clusterstate.VarLibDir = varLibDir
	defer func() { clusterstate.VarLibDir = orig }()

	pkiDir := filepath.Join(varLibDir, "pki", "issued", "services")
	writeFile(t, filepath.Join(pkiDir, "service.crt"),
		generateTestCert(t, []string{"globular.internal", "*.globular.io"}, []net.IP{net.ParseIP("10.0.0.100")}))

	g := openTestGraph(t)
	_, err := clusterstate.CollectVarLib(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, "pki_certificate")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("no cert nodes found")
	}
	sans, _ := nodes[0].Metadata["sans"].(string)
	for _, want := range []string{"globular.internal", "*.globular.io", "10.0.0.100"} {
		if !strings.Contains(sans, want) {
			t.Errorf("expected SAN %q in %q", want, sans)
		}
	}
}

func TestVarLibScanner_ParsesMinioEnvVars(t *testing.T) {
	varLibDir := t.TempDir()
	origVL := clusterstate.VarLibDir
	clusterstate.VarLibDir = varLibDir
	defer func() { clusterstate.VarLibDir = origVL }()

	systemdDir := t.TempDir()
	origSD := clusterstate.SystemdDir
	clusterstate.SystemdDir = systemdDir
	defer func() { clusterstate.SystemdDir = origSD }()

	minioEnvPath := filepath.Join(systemdDir, "globular-minio.service.d", "minio.env")
	writeFile(t, minioEnvPath, "MINIO_NODE_IP=10.0.0.63\nMINIO_DATA_DIR=/var/lib/globular/minio\n")

	g := openTestGraph(t)
	_, err := clusterstate.CollectVarLib(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, "config_file")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range nodes {
		if n.Name == "minio.env" {
			found = true
			if n.Metadata["minio_env_minio_node_ip"] == nil {
				t.Errorf("expected minio_env_minio_node_ip in metadata, got: %v", n.Metadata)
			}
		}
	}
	if !found {
		t.Error("minio.env config node not found")
	}
}

func TestVarLibScanner_EmitsReceiptNodes(t *testing.T) {
	varLibDir := t.TempDir()
	orig := clusterstate.VarLibDir
	clusterstate.VarLibDir = varLibDir
	defer func() { clusterstate.VarLibDir = orig }()

	receiptsDir := filepath.Join(varLibDir, "packages", "receipts")
	writeFile(t, filepath.Join(receiptsDir, "minio.json"), `{
		"name": "minio",
		"version": "1.2.20",
		"build_id": "abc123",
		"build_number": 171,
		"installed_at": "2026-05-08T10:00:00Z",
		"checksum": "sha256:abc"
	}`)

	g := openTestGraph(t)
	_, err := clusterstate.CollectVarLib(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, "installed_artifact")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range nodes {
		if n.ID == "receipt:minio" {
			found = true
			if n.Metadata["build_id"] != "abc123" {
				t.Errorf("expected build_id=abc123, got %v", n.Metadata["build_id"])
			}
		}
	}
	if !found {
		t.Error("receipt:minio node not found")
	}
}

// generateTestCert creates a self-signed PEM certificate for testing.
func generateTestCert(t *testing.T, dnsNames []string, ips []net.IP) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-cert"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		DNSNames:     dnsNames,
		IPAddresses:  ips,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
