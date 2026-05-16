package clusterstate

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/awareness/graph"
)

// VarLibDir is the default root for /var/lib/globular scanning.
// Override for testing.
var VarLibDir = "/var/lib/globular"

// privateKeySuffixes lists filename patterns that are NEVER read.
// This is a safety invariant — never relax it.
var privateKeySuffixes = []string{
	".key", "_private", ".token", ".jwt", ".pem.private",
}

// CollectVarLib scans /var/lib/globular for:
//   - PKI certificates (expiry, SANs) — NEVER private keys
//   - minio.env template var values
//   - Installed artifact receipts
//
// Missing VarLibDir returns CollectorHealth{Status:"skipped"}.
func CollectVarLib(ctx context.Context, g *graph.Graph) (CollectorHealth, error) {
	health := CollectorHealth{
		CollectorID: "varlib",
		SourceTier:  sourceTierInstalled,
	}

	if _, err := os.Stat(VarLibDir); os.IsNotExist(err) {
		health.Status = "skipped"
		health.Error = fmt.Sprintf("var lib dir not found: %s", VarLibDir)
		return health, nil
	}

	collectedAt := time.Now().Unix()

	// 1. PKI certificates.
	pkiDir := filepath.Join(VarLibDir, "pki", "issued")
	certCount, err := collectCerts(ctx, g, pkiDir, collectedAt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clusterstate/varlib: cert scan error: %v\n", err)
	}
	health.NodesEmitted += certCount

	// 2. minio.env values from drop-in override.
	minioEnvPath := filepath.Join(SystemdDir, "globular-minio.service.d", "minio.env")
	if n, err := collectMinioEnv(ctx, g, minioEnvPath, collectedAt); err == nil {
		health.NodesEmitted += n
	}

	// 3. Installed artifact receipts.
	receiptsDir := filepath.Join(VarLibDir, "packages", "receipts")
	receiptCount, err := collectReceipts(ctx, g, receiptsDir, collectedAt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clusterstate/varlib: receipt scan error: %v\n", err)
	}
	health.NodesEmitted += receiptCount

	health.Status = "ok"
	return health, nil
}

// collectCerts walks pkiDir for *.crt files and emits certificate nodes.
// Private keys are never read.
func collectCerts(ctx context.Context, g *graph.Graph, pkiDir string, collectedAt int64) (int, error) {
	if _, err := os.Stat(pkiDir); os.IsNotExist(err) {
		return 0, nil
	}

	emitted := 0
	err := filepath.WalkDir(pkiDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()

		// Safety: never read private keys.
		if isPrivateKeyFile(name) {
			return nil
		}

		if !strings.HasSuffix(name, ".crt") && !strings.HasSuffix(name, ".pem") {
			return nil
		}

		n, err := indexCertFile(ctx, g, path, collectedAt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clusterstate/varlib: skip cert %s: %v\n", path, err)
			return nil
		}
		emitted += n
		return nil
	})
	return emitted, err
}

func isPrivateKeyFile(name string) bool {
	for _, suffix := range privateKeySuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func indexCertFile(ctx context.Context, g *graph.Graph, path string, collectedAt int64) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	// Parse first PEM block.
	block, _ := pem.Decode(data)
	if block == nil {
		return 0, nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return 0, nil
	}

	daysRemaining := int(time.Until(cert.NotAfter).Hours() / 24)

	// Collect SANs.
	var sans []string
	sans = append(sans, cert.DNSNames...)
	for _, ip := range cert.IPAddresses {
		sans = append(sans, ip.String())
	}

	certID := "cert:" + path
	if err := g.AddNode(ctx, graph.Node{
		ID:      certID,
		Type:    "pki_certificate",
		Name:    filepath.Base(path),
		Path:    path,
		Summary: fmt.Sprintf("cert: %s expires in %d days", cert.Subject.CommonName, daysRemaining),
		Metadata: map[string]any{
			"source_tier":    sourceTierInstalled,
			"collected_at":   collectedAt,
			"common_name":    cert.Subject.CommonName,
			"expiry":         cert.NotAfter.Format(time.RFC3339),
			"days_remaining": daysRemaining,
			"sans":           strings.Join(sans, ","),
			"issuer":         cert.Issuer.CommonName,
		},
	}); err != nil {
		return 0, err
	}
	return 1, nil
}

// collectMinioEnv reads the minio.env drop-in file and emits a config node
// with the resolved template variable values (NodeIP, StateDir, etc.).
// Never reads credentials or secrets.
func collectMinioEnv(ctx context.Context, g *graph.Graph, path string, collectedAt int64) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, nil
	}

	vars := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Skip credential-like keys.
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") ||
			strings.Contains(upper, "TOKEN") || strings.Contains(upper, "KEY") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			k := strings.TrimSpace(line[:idx])
			v := strings.TrimSpace(line[idx+1:])
			vars[k] = v
		}
	}

	if len(vars) == 0 {
		return 0, nil
	}

	meta := map[string]any{
		"source_tier":  sourceTierInstalled,
		"collected_at": collectedAt,
	}
	for k, v := range vars {
		meta["minio_env_"+strings.ToLower(k)] = v
	}

	nodeID := "config:minio.env"
	if err := g.AddNode(ctx, graph.Node{
		ID:       nodeID,
		Type:     "config_file",
		Name:     "minio.env",
		Path:     path,
		Summary:  fmt.Sprintf("minio.env: %d vars resolved", len(vars)),
		Metadata: meta,
	}); err != nil {
		return 0, err
	}

	// Link to minio package.
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "package:minio",
		Kind: graph.EdgeConfigures,
		Dst:  nodeID,
		Metadata: map[string]any{"source_tier": sourceTierInstalled},
	})

	return 1, nil
}

// artifactReceipt mirrors the fields of an installed artifact receipt JSON.
type artifactReceipt struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	BuildID     string `json:"build_id"`
	BuildNumber int    `json:"build_number"`
	InstalledAt string `json:"installed_at"`
	Checksum    string `json:"checksum"`
}

// collectReceipts walks receiptsDir for *.json receipt files and emits
// installed artifact nodes with cross-layer edges to the artifact BOM.
func collectReceipts(ctx context.Context, g *graph.Graph, receiptsDir string, collectedAt int64) (int, error) {
	if _, err := os.Stat(receiptsDir); os.IsNotExist(err) {
		return 0, nil
	}

	emitted := 0
	err := filepath.WalkDir(receiptsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		var r artifactReceipt
		if err := json.Unmarshal(data, &r); err != nil || r.Name == "" {
			return nil
		}

		receiptID := "receipt:" + r.Name
		if err := g.AddNode(ctx, graph.Node{
			ID:   receiptID,
			Type: "installed_artifact",
			Name: r.Name,
			Path: path,
			Summary: fmt.Sprintf("%s@%s build_number=%d", r.Name, r.Version, r.BuildNumber),
			Metadata: map[string]any{
				"source_tier":  sourceTierInstalled,
				"collected_at": collectedAt,
				"version":      r.Version,
				"build_id":     r.BuildID,
				"build_number": r.BuildNumber,
				"installed_at": r.InstalledAt,
				"checksum":     r.Checksum,
			},
		}); err != nil {
			return nil
		}
		emitted++

		// Cross-layer edge: receipt → package
		pkgID := "package:" + r.Name
		_ = g.AddEdge(ctx, graph.Edge{
			Src:  receiptID,
			Kind: graph.EdgeCurrentStatusOf,
			Dst:  pkgID,
			Metadata: map[string]any{
				"source_tier": sourceTierInstalled,
				"edge_note":   "installed_artifact_to_package",
			},
		})
		return nil
	})
	return emitted, err
}
