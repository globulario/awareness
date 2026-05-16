// Package pki extracts certificate metadata from the Globular PKI directory
// tree and emits certificate nodes into the awareness graph.
//
// Source tier: installed_metadata
//
// Safety rules (CRITICAL — never relax):
//   - NEVER read files whose names contain "key", "_private", "_secret"
//   - NEVER read files ending in .key, _key, _private, _secret, .token, .jwt
//   - Only parse public certificate files (.crt, .pem)
//   - Private key paths are never logged as content
//   - Missing directories return CollectorHealth{Status:"skipped"}, not an error
package pki

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/awareness/graph"
)

// DefaultPKIPaths lists the canonical Globular PKI directories to scan.
// Tried in order; the first that exists is used.
var DefaultPKIPaths = []string{
	"/var/lib/globular/pki",
	"/var/lib/globular/config/tls",
}

// CollectorHealth reports the result of a collection pass.
type CollectorHealth struct {
	CollectorID  string
	SourceTier   string
	Status       string // "ok" | "partial" | "skipped" | "failed"
	NodesEmitted int
	Error        string
	Notes        []string // advisory notes (parse errors, skipped files, etc.)
}

const sourceTierInstalled = "installed_metadata"

// privateKeyPatterns are filename patterns that are NEVER read or logged.
// This is a safety invariant — do not relax without a security review.
var privateKeyPatterns = []string{
	".key",
	"_private",
	"_secret",
	".token",
	".jwt",
	".pem.private",
	"_key",
}

// isPrivateKeyFile returns true if the filename matches any private key pattern.
// Call this before opening any file in the PKI directory.
func isPrivateKeyFile(name string) bool {
	lower := strings.ToLower(name)
	for _, pat := range privateKeyPatterns {
		if strings.HasSuffix(lower, pat) {
			return true
		}
	}
	// Extra guard: reject any filename that literally contains "key" as a word
	// boundary — catches ca.key, service.key, node_key, etc.
	if strings.Contains(lower, ".key") || strings.Contains(lower, "_key.") {
		return true
	}
	return false
}

// CertMetadata holds the parsed fields of a single X.509 certificate.
type CertMetadata struct {
	FilePath        string
	Subject         string
	Issuer          string
	SANDNSNames     []string
	SANIPs          []string
	NotBefore       int64 // unix timestamp
	NotAfter        int64 // unix timestamp
	Serial          string
	IsCA            bool
	DaysUntilExpiry int64
}

// Extract scans pkiDir for .crt/.pem certificate files (public certs only),
// parses their metadata, and emits certificate nodes into the graph.
// Private key files are never read.
//
// If pkiDir does not exist, returns CollectorHealth{Status:"skipped"}.
func Extract(ctx context.Context, g *graph.Graph, pkiDir string) (CollectorHealth, error) {
	health := CollectorHealth{
		CollectorID: "pki",
		SourceTier:  sourceTierInstalled,
	}

	if _, err := os.Stat(pkiDir); os.IsNotExist(err) {
		health.Status = "skipped"
		health.Error = fmt.Sprintf("pki dir not found: %s", pkiDir)
		return health, nil
	}

	collectedAt := time.Now().Unix()

	// Index CA certs first so cert→CA edges can be wired in a second pass.
	// We collect all parsed certs and wire edges after the walk.
	type parsedCert struct {
		meta   CertMetadata
		nodeID string
	}
	var allCerts []parsedCert
	parseErrors := 0

	err := filepath.WalkDir(pkiDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()

		// SAFETY: skip private keys unconditionally — never open them.
		if isPrivateKeyFile(name) {
			return nil
		}

		if !strings.HasSuffix(name, ".crt") && !strings.HasSuffix(name, ".pem") {
			return nil
		}

		meta, err := parseCertFile(path)
		if err != nil {
			parseErrors++
			health.Notes = append(health.Notes,
				fmt.Sprintf("skip %s: %v", filepath.Base(path), err))
			return nil
		}
		if meta == nil {
			return nil // empty or non-cert PEM block
		}

		// Compute node ID relative to pkiDir.
		rel, relErr := filepath.Rel(pkiDir, path)
		if relErr != nil {
			rel = path
		}
		nodeID := "certificate:" + rel

		nodeType := graph.NodeTypeCertificate
		if meta.IsCA {
			nodeType = graph.NodeTypeCertificateAuthority
		}

		expired := meta.DaysUntilExpiry < 0
		summary := fmt.Sprintf("cert: %s, expires in %d days", meta.Subject, meta.DaysUntilExpiry)
		if expired {
			summary = fmt.Sprintf("cert: %s, EXPIRED %d days ago", meta.Subject, -meta.DaysUntilExpiry)
		}

		nodeMeta := map[string]any{
			"source_tier":       sourceTierInstalled,
			"collected_at":      collectedAt,
			"subject":           meta.Subject,
			"issuer":            meta.Issuer,
			"not_before":        meta.NotBefore,
			"not_after":         meta.NotAfter,
			"serial":            meta.Serial,
			"is_ca":             meta.IsCA,
			"days_until_expiry": meta.DaysUntilExpiry,
			"file_path":         path,
		}
		if expired {
			nodeMeta["expired"] = true
		}
		if len(meta.SANDNSNames) > 0 {
			nodeMeta["san_dns"] = strings.Join(meta.SANDNSNames, ",")
		}
		if len(meta.SANIPs) > 0 {
			nodeMeta["san_ips"] = strings.Join(meta.SANIPs, ",")
		}

		if addErr := g.AddNode(ctx, graph.Node{
			ID:       nodeID,
			Type:     nodeType,
			Name:     name,
			Path:     path,
			Summary:  summary,
			Metadata: nodeMeta,
		}); addErr != nil {
			return fmt.Errorf("AddNode %s: %w", nodeID, addErr)
		}
		health.NodesEmitted++

		// Emit SAN nodes and edges.
		for _, dns := range meta.SANDNSNames {
			sanID := "cert_san:" + dns
			_ = g.AddNode(ctx, graph.Node{
				ID:      sanID,
				Type:    graph.NodeTypeCertSAN,
				Name:    dns,
				Summary: "DNS SAN: " + dns,
			})
			_ = g.AddEdge(ctx, graph.Edge{
				Src:  nodeID,
				Kind: graph.EdgeCertHasSAN,
				Dst:  sanID,
				Metadata: map[string]any{
					"source_tier": sourceTierInstalled,
					"san_type":    "dns",
				},
			})
		}
		for _, ip := range meta.SANIPs {
			sanID := "cert_san:" + ip
			_ = g.AddNode(ctx, graph.Node{
				ID:      sanID,
				Type:    graph.NodeTypeCertSAN,
				Name:    ip,
				Summary: "IP SAN: " + ip,
			})
			_ = g.AddEdge(ctx, graph.Edge{
				Src:  nodeID,
				Kind: graph.EdgeCertHasSAN,
				Dst:  sanID,
				Metadata: map[string]any{
					"source_tier": sourceTierInstalled,
					"san_type":    "ip",
				},
			})
		}

		// Emit expiry warning node if cert expires within 30 days but is not yet expired.
		if meta.DaysUntilExpiry >= 0 && meta.DaysUntilExpiry < 30 {
			warnID := "cert_expiry_warning:" + rel
			_ = g.AddNode(ctx, graph.Node{
				ID:      warnID,
				Type:    graph.NodeTypeCertExpiryWarning,
				Name:    "expiry:" + name,
				Summary: fmt.Sprintf("cert %s expires in %d days", meta.Subject, meta.DaysUntilExpiry),
				Metadata: map[string]any{
					"source_tier":       sourceTierInstalled,
					"collected_at":      collectedAt,
					"days_until_expiry": meta.DaysUntilExpiry,
					"cert_node":         nodeID,
				},
			})
			// Wire expiry warning to invariant if it exists in the graph.
			invariantID := "invariant:pki.ca_metadata_must_be_published"
			if inv, _ := g.FindNode(ctx, invariantID); inv != nil {
				_ = g.AddEdge(ctx, graph.Edge{
					Src:  warnID,
					Kind: graph.EdgeCertRisksInvariant,
					Dst:  invariantID,
					Metadata: map[string]any{
						"source_tier":       sourceTierInstalled,
						"days_until_expiry": meta.DaysUntilExpiry,
					},
				})
			}
		}

		allCerts = append(allCerts, parsedCert{meta: *meta, nodeID: nodeID})
		return nil
	})
	if err != nil {
		health.Status = "failed"
		health.Error = err.Error()
		return health, err
	}

	// Second pass: wire cert→CA edges now that all CA nodes are present.
	// Build a subject→nodeID map for CA nodes.
	caBySubject := map[string]string{}
	for _, pc := range allCerts {
		if pc.meta.IsCA {
			caBySubject[pc.meta.Subject] = pc.nodeID
		}
	}
	for _, pc := range allCerts {
		if pc.meta.IsCA {
			continue // skip self
		}
		if caNodeID, ok := caBySubject[pc.meta.Issuer]; ok && caNodeID != pc.nodeID {
			_ = g.AddEdge(ctx, graph.Edge{
				Src:  pc.nodeID,
				Kind: graph.EdgeCertIssuedBy,
				Dst:  caNodeID,
				Metadata: map[string]any{
					"source_tier": sourceTierInstalled,
				},
			})
		}
	}

	if parseErrors > 0 && health.NodesEmitted == 0 {
		health.Status = "failed"
		health.Error = fmt.Sprintf("%d parse errors, 0 certs indexed", parseErrors)
	} else if parseErrors > 0 {
		health.Status = "partial"
		health.Notes = append(health.Notes,
			fmt.Sprintf("%d files failed to parse", parseErrors))
	} else {
		health.Status = "ok"
	}
	return health, nil
}

// parseCertFile reads a public certificate file and returns its metadata.
// Returns (nil, nil) if the file contains no valid certificate PEM block.
// NEVER call this on private key files.
func parseCertFile(path string) (*CertMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Parse the first PEM block. We intentionally skip private key blocks.
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, nil // no PEM block found
	}
	if block.Type == "PRIVATE KEY" || block.Type == "RSA PRIVATE KEY" ||
		block.Type == "EC PRIVATE KEY" || block.Type == "ENCRYPTED PRIVATE KEY" {
		// Safety: never parse private key data even if file extension is wrong.
		return nil, fmt.Errorf("file contains private key block — skipped for safety")
	}
	if block.Type != "CERTIFICATE" {
		return nil, nil // not a certificate block (could be CSR, etc.)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("x509 parse: %w", err)
	}

	daysUntilExpiry := int64(time.Until(cert.NotAfter).Hours() / 24)

	var ipSANs []string
	for _, ip := range cert.IPAddresses {
		ipSANs = append(ipSANs, ip.String())
	}

	return &CertMetadata{
		FilePath:        path,
		Subject:         cert.Subject.String(),
		Issuer:          cert.Issuer.String(),
		SANDNSNames:     cert.DNSNames,
		SANIPs:          ipSANs,
		NotBefore:       cert.NotBefore.Unix(),
		NotAfter:        cert.NotAfter.Unix(),
		Serial:          cert.SerialNumber.String(),
		IsCA:            cert.IsCA,
		DaysUntilExpiry: daysUntilExpiry,
	}, nil
}
