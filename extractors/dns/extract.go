// Package dns extracts DNS topology knowledge from docs/awareness/knowledge/dns_zones.yaml
// and emits dns_zone, dns_record, service_endpoint, and domain_spec nodes into
// the awareness graph.
//
// Source tier: cluster_configuration
//
// This extractor captures INTENDED DNS topology (from static YAML knowledge).
// Live DNS state is tracked separately by the live-snapshot collector.
//
// Emitted edges:
//   - dns_record_resolves_to        : dns_record → service_endpoint (where resolvable)
//   - service_endpoint_advertised_by: service_endpoint → globular_service
//   - service_endpoint_covered_by_cert: service_endpoint → certificate (stub if absent)
//   - domain_spec_declares_record   : domain_spec → dns_record
//   - dns_record_risks_invariant    : dns_record → invariant
package dns

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
)

const sourceTierClusterConfig = "cluster_configuration"

// CollectorHealth reports the result of an extraction pass.
type CollectorHealth struct {
	CollectorID  string
	SourceTier   string
	Status       string // "ok" | "partial" | "skipped" | "error"
	NodesEmitted int
	Error        string
}

// dnsZonesFile mirrors the structure of dns_zones.yaml.
type dnsZonesFile struct {
	DNSZones         []yamlDNSZone         `yaml:"dns_zones"`
	ServiceEndpoints []yamlServiceEndpoint `yaml:"service_endpoints"`
	DomainSpecs      []yamlDomainSpec      `yaml:"domain_specs"`
}

type yamlDNSZone struct {
	ID      string          `yaml:"id"`
	Kind    string          `yaml:"kind"`
	Summary string          `yaml:"summary"`
	Records []yamlDNSRecord `yaml:"records"`
}

type yamlDNSRecord struct {
	Name             string   `yaml:"name"`
	Type             string   `yaml:"type"`
	ResolvesTo       string   `yaml:"resolves_to"`
	RisksInvariants  []string `yaml:"risks_invariants"`
}

type yamlServiceEndpoint struct {
	ID             string   `yaml:"id"`
	FQDN           string   `yaml:"fqdn"`
	Port           int      `yaml:"port"`
	Protocol       string   `yaml:"protocol"`
	TLS            bool     `yaml:"tls"`
	Zone           string   `yaml:"zone"`
	CoveredByCert  string   `yaml:"covered_by_cert"`
	AdvertisedBy   string   `yaml:"advertised_by"`
	RisksInvariants []string `yaml:"risks_invariants"`
}

type yamlDomainSpec struct {
	ID          string   `yaml:"id"`
	FQDN        string   `yaml:"fqdn"`
	Zone        string   `yaml:"zone"`
	Kind        string   `yaml:"kind"`
	ACMEEnabled bool     `yaml:"acme_enabled"`
	CertID      string   `yaml:"cert_id"`
	Records     []string `yaml:"records"`
}

// Extract reads dns_zones.yaml from docsAwarenessDir and emits DNS topology
// nodes and edges into g. Missing file is silently skipped.
func Extract(ctx context.Context, g *graph.Graph, docsAwarenessDir string) (CollectorHealth, error) {
	h := CollectorHealth{
		CollectorID: "dns",
		SourceTier:  sourceTierClusterConfig,
	}
	if docsAwarenessDir == "" {
		h.Status = "skipped"
		return h, nil
	}

	path := filepath.Join(docsAwarenessDir, "knowledge", "dns_zones.yaml")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		h.Status = "skipped"
		return h, nil
	}
	if err != nil {
		h.Status = "error"
		h.Error = fmt.Sprintf("read %s: %v", path, err)
		return h, fmt.Errorf("dns.Extract: %w", err)
	}

	var f dnsZonesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		h.Status = "error"
		h.Error = fmt.Sprintf("parse %s: %v", path, err)
		return h, fmt.Errorf("dns.Extract: parse: %w", err)
	}

	var emitted int

	for _, z := range f.DNSZones {
		n, err := extractZone(ctx, g, z)
		if err != nil {
			return h, fmt.Errorf("dns.Extract: zone %s: %w", z.ID, err)
		}
		emitted += n
	}

	for _, ep := range f.ServiceEndpoints {
		n, err := extractServiceEndpoint(ctx, g, ep)
		if err != nil {
			return h, fmt.Errorf("dns.Extract: endpoint %s: %w", ep.ID, err)
		}
		emitted += n
	}

	for _, ds := range f.DomainSpecs {
		n, err := extractDomainSpec(ctx, g, ds)
		if err != nil {
			return h, fmt.Errorf("dns.Extract: domain_spec %s: %w", ds.ID, err)
		}
		emitted += n
	}

	h.Status = "ok"
	h.NodesEmitted = emitted
	return h, nil
}

func extractZone(ctx context.Context, g *graph.Graph, z yamlDNSZone) (int, error) {
	if z.ID == "" {
		return 0, nil
	}
	zoneID := "dns_zone:" + z.ID
	if err := g.AddNode(ctx, graph.Node{
		ID:      zoneID,
		Type:    graph.NodeTypeDNSZone,
		Name:    z.ID,
		Summary: z.Summary,
		Metadata: map[string]any{
			"kind":       z.Kind,
			"source":     "dns_zones.yaml",
			"source_tier": sourceTierClusterConfig,
		},
	}); err != nil {
		return 0, err
	}
	emitted := 1

	for _, rec := range z.Records {
		n, err := extractRecord(ctx, g, zoneID, rec)
		if err != nil {
			return emitted, fmt.Errorf("record %s: %w", rec.Name, err)
		}
		emitted += n
	}
	return emitted, nil
}

func extractRecord(ctx context.Context, g *graph.Graph, zoneID string, rec yamlDNSRecord) (int, error) {
	if rec.Name == "" {
		return 0, nil
	}
	recID := "dns_record:" + rec.Name
	if err := g.AddNode(ctx, graph.Node{
		ID:   recID,
		Type: graph.NodeTypeDNSRecord,
		Name: rec.Name,
		Metadata: map[string]any{
			"type":       rec.Type,
			"resolves_to": rec.ResolvesTo,
			"source_tier": sourceTierClusterConfig,
		},
	}); err != nil {
		return 0, err
	}
	emitted := 1

	// zone → record (the zone declares this record).
	if err := g.AddEdge(ctx, graph.Edge{Src: zoneID, Kind: graph.EdgeDomainSpecDeclaresRecord, Dst: recID}); err != nil {
		return emitted, err
	}

	// Record → invariants it risks.
	for _, invID := range rec.RisksInvariants {
		if invID == "" {
			continue
		}
		fullInvID := "invariant:" + invID
		// Ensure the invariant stub exists so the edge target is valid.
		_ = g.AddNode(ctx, graph.Node{
			ID:      fullInvID,
			Type:    graph.NodeTypeInvariant,
			Name:    invID,
			Summary: "(stub)",
		})
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  recID,
			Kind: graph.EdgeDNSRecordRisksInvariant,
			Dst:  fullInvID,
		}); err != nil {
			return emitted, err
		}
	}
	return emitted, nil
}

func extractServiceEndpoint(ctx context.Context, g *graph.Graph, ep yamlServiceEndpoint) (int, error) {
	if ep.ID == "" {
		return 0, nil
	}
	epID := "service_endpoint:" + ep.ID
	if err := g.AddNode(ctx, graph.Node{
		ID:   epID,
		Type: graph.NodeTypeServiceEndpoint,
		Name: ep.ID,
		Metadata: map[string]any{
			"fqdn":        ep.FQDN,
			"port":        ep.Port,
			"protocol":    ep.Protocol,
			"tls":         ep.TLS,
			"zone":        ep.Zone,
			"source_tier": sourceTierClusterConfig,
		},
	}); err != nil {
		return 0, err
	}
	emitted := 1

	// Link to zone.
	if ep.Zone != "" {
		zoneID := "dns_zone:" + ep.Zone
		_ = g.AddNode(ctx, graph.Node{ID: zoneID, Type: graph.NodeTypeDNSZone, Name: ep.Zone})
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  epID,
			Kind: graph.EdgeServiceEndpointAdvertisedBy,
			Dst:  zoneID,
		}); err != nil {
			return emitted, err
		}
	}

	// Link to the Globular service that advertises this endpoint.
	if ep.AdvertisedBy != "" {
		svcID := "globular_service:" + ep.AdvertisedBy
		_ = g.AddNode(ctx, graph.Node{
			ID:   svcID,
			Type: graph.NodeTypeGlobularService,
			Name: ep.AdvertisedBy,
		})
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  epID,
			Kind: graph.EdgeServiceEndpointAdvertisedBy,
			Dst:  svcID,
		}); err != nil {
			return emitted, err
		}
	}

	// Link to certificate stub.
	if ep.CoveredByCert != "" {
		certID := "certificate:" + ep.CoveredByCert
		_ = g.AddNode(ctx, graph.Node{
			ID:   certID,
			Type: graph.NodeTypeCertificate,
			Name: ep.CoveredByCert,
		})
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  epID,
			Kind: graph.EdgeServiceEndpointCoveredByCert,
			Dst:  certID,
		}); err != nil {
			return emitted, err
		}
	}

	// Link to risks invariants.
	for _, invID := range ep.RisksInvariants {
		if invID == "" {
			continue
		}
		fullInvID := "invariant:" + invID
		_ = g.AddNode(ctx, graph.Node{
			ID:      fullInvID,
			Type:    graph.NodeTypeInvariant,
			Name:    invID,
			Summary: "(stub)",
		})
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  epID,
			Kind: graph.EdgeDNSRecordRisksInvariant,
			Dst:  fullInvID,
		}); err != nil {
			return emitted, err
		}
	}
	return emitted, nil
}

func extractDomainSpec(ctx context.Context, g *graph.Graph, ds yamlDomainSpec) (int, error) {
	if ds.ID == "" {
		return 0, nil
	}
	dsID := "domain_spec:" + ds.ID
	if err := g.AddNode(ctx, graph.Node{
		ID:   dsID,
		Type: graph.NodeTypeDomainSpec,
		Name: ds.FQDN,
		Metadata: map[string]any{
			"fqdn":         ds.FQDN,
			"zone":         ds.Zone,
			"kind":         ds.Kind,
			"acme_enabled": ds.ACMEEnabled,
			"cert_id":      ds.CertID,
			"source_tier":  sourceTierClusterConfig,
		},
	}); err != nil {
		return 0, err
	}
	emitted := 1

	// Link to cert stub.
	if ds.CertID != "" {
		certID := "certificate:" + ds.CertID
		_ = g.AddNode(ctx, graph.Node{
			ID:   certID,
			Type: graph.NodeTypeCertificate,
			Name: ds.CertID,
		})
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  dsID,
			Kind: graph.EdgeServiceEndpointCoveredByCert,
			Dst:  certID,
		}); err != nil {
			return emitted, err
		}
	}

	// domain_spec → dns_records it declares.
	for _, recName := range ds.Records {
		if recName == "" {
			continue
		}
		recID := "dns_record:" + recName
		_ = g.AddNode(ctx, graph.Node{
			ID:   recID,
			Type: graph.NodeTypeDNSRecord,
			Name: recName,
		})
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  dsID,
			Kind: graph.EdgeDomainSpecDeclaresRecord,
			Dst:  recID,
		}); err != nil {
			return emitted, err
		}
	}
	return emitted, nil
}
