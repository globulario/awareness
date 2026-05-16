package pki_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/awareness/extractors/pki"
	"github.com/globulario/awareness/graph"
)

// openTestGraph opens an in-memory awareness graph for testing.
func openTestGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("graph.OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

// generateSelfSignedCert creates a self-signed X.509 certificate and returns
// the DER-encoded cert bytes and the private key. The key is NEVER written to
// disk in these tests — only the cert is written.
func generateSelfSignedCert(t *testing.T, tmpl *x509.Certificate) (certPEM []byte, key *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	return certPEM, key
}

// writeCertFile writes PEM bytes to a file in dir and returns the path.
func writeCertFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("writeCertFile %s: %v", name, err)
	}
	return path
}

// TestCertExtractor_SkipsPrivateKeys verifies that files with private key
// suffixes are never opened or parsed, even if they exist in the PKI directory.
func TestCertExtractor_SkipsPrivateKeys(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	g := openTestGraph(t)

	// Write a synthetic "private key" file. We use dummy bytes — the extractor
	// must not open it at all, so the content doesn't matter.
	keyNames := []string{
		"ca.key",
		"service.key",
		"node_private",
		"secret.pem.private",
		"auth.token",
		"session.jwt",
	}
	for _, name := range keyNames {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("SHOULD NEVER BE READ"), 0600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	h, err := pki.Extract(ctx, g, dir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if h.Status != "ok" && h.Status != "skipped" {
		// "ok" is acceptable because the directory exists but contains no certs;
		// the extractor should not emit nodes or error.
		t.Errorf("unexpected status %q (want ok or skipped)", h.Status)
	}
	if h.NodesEmitted != 0 {
		t.Errorf("NodesEmitted = %d, want 0 (private key files must be skipped)", h.NodesEmitted)
	}

	// Confirm zero certificate nodes in the graph.
	nodes, err := g.FindNodesByType(ctx, graph.NodeTypeCertificate)
	if err != nil {
		t.Fatalf("FindNodesByType: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("got %d certificate nodes, want 0", len(nodes))
	}
}

// TestCertExtractor_ParsesCertMetadata verifies that a self-signed cert written
// to the temp dir is parsed and emitted with correct subject, SAN, and node ID.
func TestCertExtractor_ParsesCertMetadata(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	g := openTestGraph(t)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-ca"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		IsCA:         false,
		DNSNames:     []string{"test.example.com", "*.test.example.com"},
		IPAddresses:  []net.IP{net.ParseIP("10.0.0.1")},
	}
	certPEM, _ := generateSelfSignedCert(t, tmpl)
	writeCertFile(t, dir, "test.crt", certPEM)

	h, err := pki.Extract(ctx, g, dir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if h.Status != "ok" {
		t.Errorf("status = %q, want ok; notes: %v", h.Status, h.Notes)
	}
	if h.NodesEmitted < 1 {
		t.Errorf("NodesEmitted = %d, want >= 1", h.NodesEmitted)
	}

	// Verify certificate node exists.
	node, err := g.FindNode(ctx, "certificate:test.crt")
	if err != nil {
		t.Fatalf("FindNode: %v", err)
	}
	if node == nil {
		t.Fatal("certificate node not found")
	}
	if node.Type != graph.NodeTypeCertificate {
		t.Errorf("node type = %q, want %q", node.Type, graph.NodeTypeCertificate)
	}

	// Verify SAN nodes.
	sanDNS, err := g.FindNode(ctx, "cert_san:test.example.com")
	if err != nil {
		t.Fatalf("FindNode cert_san:test.example.com: %v", err)
	}
	if sanDNS == nil {
		t.Error("cert_san:test.example.com node not found")
	}

	sanWild, err := g.FindNode(ctx, "cert_san:*.test.example.com")
	if err != nil {
		t.Fatalf("FindNode cert_san:*.test.example.com: %v", err)
	}
	if sanWild == nil {
		t.Error("cert_san:*.test.example.com node not found")
	}

	sanIP, err := g.FindNode(ctx, "cert_san:10.0.0.1")
	if err != nil {
		t.Fatalf("FindNode cert_san:10.0.0.1: %v", err)
	}
	if sanIP == nil {
		t.Error("cert_san:10.0.0.1 node not found")
	}
}

// TestCertExtractor_DetectsExpiryWarning verifies that a certificate expiring
// within 30 days causes an expiry warning node to be emitted.
func TestCertExtractor_DetectsExpiryWarning(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	g := openTestGraph(t)

	// Cert expiring in 10 days.
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "expiring-soon"},
		NotBefore:    time.Now().Add(-24 * time.Hour),
		NotAfter:     time.Now().Add(10 * 24 * time.Hour), // 10 days
		IsCA:         false,
	}
	certPEM, _ := generateSelfSignedCert(t, tmpl)
	writeCertFile(t, dir, "expiring.crt", certPEM)

	h, err := pki.Extract(ctx, g, dir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if h.Status != "ok" {
		t.Errorf("status = %q, want ok", h.Status)
	}

	// An expiry warning node must exist.
	warnNode, err := g.FindNode(ctx, "cert_expiry_warning:expiring.crt")
	if err != nil {
		t.Fatalf("FindNode cert_expiry_warning:expiring.crt: %v", err)
	}
	if warnNode == nil {
		t.Fatal("expiry warning node not emitted for cert expiring in 10 days")
	}
	if warnNode.Type != graph.NodeTypeCertExpiryWarning {
		t.Errorf("node type = %q, want %q", warnNode.Type, graph.NodeTypeCertExpiryWarning)
	}
}

// TestCertExtractor_DetectsMissingSAN verifies that a cert with no SANs still
// emits a certificate node, but emits zero SAN edges.
func TestCertExtractor_DetectsMissingSAN(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	g := openTestGraph(t)

	// Cert with no SANs.
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: "no-san-cert"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		IsCA:         false,
		// No DNSNames, no IPAddresses
	}
	certPEM, _ := generateSelfSignedCert(t, tmpl)
	writeCertFile(t, dir, "noSAN.crt", certPEM)

	h, err := pki.Extract(ctx, g, dir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if h.Status != "ok" {
		t.Errorf("status = %q, want ok; notes: %v", h.Status, h.Notes)
	}

	// Certificate node must exist.
	node, err := g.FindNode(ctx, "certificate:noSAN.crt")
	if err != nil {
		t.Fatalf("FindNode: %v", err)
	}
	if node == nil {
		t.Fatal("certificate node not found for no-SAN cert")
	}

	// Zero CertSAN nodes should exist.
	sanNodes, err := g.FindNodesByType(ctx, graph.NodeTypeCertSAN)
	if err != nil {
		t.Fatalf("FindNodesByType CertSAN: %v", err)
	}
	if len(sanNodes) != 0 {
		t.Errorf("got %d CertSAN nodes, want 0 for a cert with no SANs", len(sanNodes))
	}
}

// TestCertExtractor_SkippedWhenDirMissing verifies graceful skip when the PKI
// directory does not exist.
func TestCertExtractor_SkippedWhenDirMissing(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	h, err := pki.Extract(ctx, g, "/nonexistent/pki/path/12345")
	if err != nil {
		t.Fatalf("Extract returned error for missing dir: %v", err)
	}
	if h.Status != "skipped" {
		t.Errorf("status = %q, want skipped", h.Status)
	}
	if h.NodesEmitted != 0 {
		t.Errorf("NodesEmitted = %d, want 0", h.NodesEmitted)
	}
}
