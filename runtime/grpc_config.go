package runtime

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// GrpcSourceConfig holds connection settings for a gRPC runtime source.
// If Insecure is true, plain-text transport is used (local dev/test only).
// If Insecure is false (default), mTLS is required when CACert is set;
// otherwise system TLS is used.
type GrpcSourceConfig struct {
	Addr       string // required: host:port
	Insecure   bool   // if true, use insecure transport (emit warning in source health)
	CACert     string // path to CA cert PEM (for mTLS)
	ClientCert string // path to client cert PEM (for mTLS)
	ClientKey  string // path to client key PEM (for mTLS)
	ServerName string // TLS server name override
}

// dialOptions returns the gRPC dial options for this config.
// Returns an error if mTLS is requested but certs cannot be loaded.
// The second return value is the transport label: "insecure", "tls", or "mtls".
func (c GrpcSourceConfig) dialOptions() ([]grpc.DialOption, string, error) {
	if c.Insecure {
		return []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, "insecure", nil
	}
	if c.CACert == "" {
		// No cert provided — default to system TLS (no client cert).
		tlsCfg := &tls.Config{ServerName: c.ServerName, MinVersion: tls.VersionTLS12}
		return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg))}, "tls", nil
	}
	// mTLS.
	caPEM, err := os.ReadFile(c.CACert)
	if err != nil {
		return nil, "", fmt.Errorf("load CA cert %s: %w", c.CACert, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, "", fmt.Errorf("parse CA cert %s: no valid PEM block", c.CACert)
	}
	tlsCfg := &tls.Config{RootCAs: pool, ServerName: c.ServerName, MinVersion: tls.VersionTLS12}
	if c.ClientCert != "" && c.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(c.ClientCert, c.ClientKey)
		if err != nil {
			return nil, "", fmt.Errorf("load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg))}, "mtls", nil
}
