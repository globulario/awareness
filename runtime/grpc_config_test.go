package runtime

import (
	"strings"
	"testing"
)

// TestGrpcSourceConfig_InsecureDialOptions verifies that Insecure=true produces
// insecure transport options with the correct transport label.
func TestGrpcSourceConfig_InsecureDialOptions(t *testing.T) {
	cfg := GrpcSourceConfig{
		Addr:     "localhost:9999",
		Insecure: true,
	}
	opts, transport, err := cfg.dialOptions()
	if err != nil {
		t.Fatalf("dialOptions: %v", err)
	}
	if len(opts) == 0 {
		t.Error("expected at least one dial option")
	}
	if transport != "insecure" {
		t.Errorf("transport = %q, want %q", transport, "insecure")
	}
}

// TestGrpcSourceConfig_NoCAUsesSystemTLS verifies that when CACert is empty
// and Insecure is false, system TLS is used.
func TestGrpcSourceConfig_NoCAUsesSystemTLS(t *testing.T) {
	cfg := GrpcSourceConfig{
		Addr:     "example.com:443",
		Insecure: false,
		CACert:   "",
	}
	opts, transport, err := cfg.dialOptions()
	if err != nil {
		t.Fatalf("dialOptions: %v", err)
	}
	if len(opts) == 0 {
		t.Error("expected at least one dial option")
	}
	if transport != "tls" {
		t.Errorf("transport = %q, want %q", transport, "tls")
	}
}

// TestGrpcSourceConfig_MissingCACertReturnsError verifies that a non-existent
// CA cert path causes dialOptions to return an error without panicking.
func TestGrpcSourceConfig_MissingCACertReturnsError(t *testing.T) {
	cfg := GrpcSourceConfig{
		Addr:   "example.com:443",
		CACert: "/nonexistent/ca.crt",
	}
	_, _, err := cfg.dialOptions()
	if err == nil {
		t.Error("expected error for missing CA cert, got nil")
	}
	if !strings.Contains(err.Error(), "ca.crt") {
		t.Errorf("error should mention ca.crt, got: %v", err)
	}
}

// TestSourceHealth_WithTransport verifies that withTransport sets fields correctly.
func TestSourceHealth_WithTransport(t *testing.T) {
	sh := SourceHealth{
		Source:  SourceDoctor,
		Backend: "cluster_doctor.grpc",
		Healthy: true,
	}
	sh2 := sh.withTransport("insecure", "none")

	if sh2.Transport != "insecure" {
		t.Errorf("Transport = %q, want insecure", sh2.Transport)
	}
	if sh2.Auth != "none" {
		t.Errorf("Auth = %q, want none", sh2.Auth)
	}
	// Should add a production-safety warning.
	if len(sh2.Warnings) == 0 {
		t.Error("expected insecure warning in Warnings")
	}
	found := false
	for _, w := range sh2.Warnings {
		if strings.Contains(w, "insecure") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'insecure' in warning, got: %v", sh2.Warnings)
	}
}

// TestSourceHealth_WithTransportMTLS verifies that mTLS transport does not add warnings.
func TestSourceHealth_WithTransportMTLS(t *testing.T) {
	sh := SourceHealth{
		Source:  SourceDoctor,
		Backend: "cluster_doctor.grpc",
		Healthy: true,
	}
	sh2 := sh.withTransport("mtls", "service_token")

	if sh2.Transport != "mtls" {
		t.Errorf("Transport = %q, want mtls", sh2.Transport)
	}
	if len(sh2.Warnings) != 0 {
		t.Errorf("expected no warnings for mTLS transport, got: %v", sh2.Warnings)
	}
}

// testTransportSource is a minimal fake implementing sourceIdentifier and transportReporter.
type testTransportSource struct {
	transport string
}

func (f *testTransportSource) SourceInfo() (string, bool) { return "fake.grpc", false }
func (f *testTransportSource) Transport() string           { return f.transport }

// TestSourceHealthFor_TransportReporter verifies that sourceHealthFor picks up
// transport from a source that implements transportReporter.
func TestSourceHealthFor_TransportReporter(t *testing.T) {
	src := &testTransportSource{transport: "mtls"}
	sh := sourceHealthFor(SourceDoctor, src, nil)

	if sh.Transport != "mtls" {
		t.Errorf("Transport = %q, want mtls", sh.Transport)
	}
}
