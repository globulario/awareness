package evidence

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"


)

const (
	// awarenessCurrentLink mirrors the path in node_agent/awareness_bundle.go.
	awarenessCurrentLink  = "/var/lib/globular/awareness/current"
	awarenessManifestFile = "manifest.json"

	releaseIndexPath = "/var/lib/globular/release-index.json"

	// PKI paths (canonical, from CLAUDE.md).
	pkiCACertPath    = "/var/lib/globular/pki/ca.crt"
	pkiNodeCertPath  = "/var/lib/globular/pki/issued/services/service.crt"
	pkiNodeKeyPath   = "/var/lib/globular/pki/issued/services/service.key"

	// Scylla config path (distro default).
	scyllaConfigPath = "/etc/scylla/scylla.yaml"
)

// bundleManifest is the minimal subset of the manifest we need here.
type bundleManifest struct {
	Version string `json:"version"`
	BuildID string `json:"build_id"`
}

// PKIObservation records the presence and basic health of local PKI artifacts.
//
// Present and Readable are independent: a file at mode 0400 owned by another
// user is Present=true Readable=false from the current process's point of
// view. Splitting them keeps the normalizer honest — "file gone" demands
// re-issuance, "file present but unreadable" demands ownership/perm review
// (or simply means the collector is running as the wrong user).
type PKIObservation struct {
	CACertPresent   bool `json:"ca_cert_present"`
	CACertReadable  bool `json:"ca_cert_readable"`
	NodeCertPresent  bool `json:"node_cert_present"`
	NodeCertReadable bool `json:"node_cert_readable"`
	NodeKeyPresent   bool `json:"node_key_present"`
	NodeKeyReadable  bool `json:"node_key_readable"`
	// CertExpired is true when we can determine the cert has expired.
	// We do not parse ASN.1 here; the normalizer sets PKI_EXPIRED if openssl says so.
	CertExpired bool `json:"cert_expired,omitempty"`
}

// ScyllaConfigObservation holds seed list and identity fields from scylla.yaml.
type ScyllaConfigObservation struct {
	Present      bool     `json:"present"`
	Seeds        []string `json:"seeds,omitempty"`
	ListenAddress string  `json:"listen_address,omitempty"`
	RPCAddress    string  `json:"rpc_address,omitempty"`
	ClusterName   string  `json:"cluster_name,omitempty"`
}

// Collector gathers raw local node evidence.
// All operations are read-only. Errors are collected as warnings, not hard failures.
type Collector struct {
	NodeID  string
	Address string
	Phase   Phase
}

// NewCollector returns a Collector for the local node.
// If nodeID is empty, the local kernel hostname is used — every fact the
// collector emits must be attributable to a specific node, otherwise
// cross-node correlation by downstream consumers is impossible.
// address is left as-passed; nothing in the collector currently depends
// on it, and the right canonical source (etcd / nodeagent inventory)
// requires network access we can't assume Day-0.
func NewCollector(nodeID, address string, phase Phase) *Collector {
	if nodeID == "" {
		nodeID = localHostname()
	}
	return &Collector{NodeID: nodeID, Address: address, Phase: phase}
}

// localHostname returns the kernel hostname, trimmed. Empty string on error.
// This is the same identity other Globular services use to register with
// the cluster (cluster_controller resolves UUID from hostname), so the
// evidence collector emits facts under the same name the rest of the
// system already knows the node by.
func localHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(h)
}

// Collect gathers a NodeRuntimeSnapshot from local sources.
// ctx controls cancellation. It never returns a non-nil error — failures produce warnings.
func (c *Collector) Collect(ctx context.Context) *NodeRuntimeSnapshot {
	snap := &NodeRuntimeSnapshot{
		NodeID:      c.NodeID,
		Address:     c.Address,
		Phase:       c.Phase,
		CollectedAt: time.Now().UTC(),
	}

	snap.Release = c.readReleaseIndex()
	snap.AwarenessBundle = c.readBundleStatus()
	snap.PKI = c.collectPKI()
	snap.ScyllaConfig = c.readScyllaConfig()
	snap.Services = c.collectSystemdUnits(ctx)
	snap.Ports = c.collectPorts(ctx)

	return snap
}

func (c *Collector) readReleaseIndex() ReleaseInfo {
	return readReleaseIndexFrom(releaseIndexPath)
}

// readReleaseIndexFrom loads and parses a release-index.json from path.
// Exposed for testability.
func readReleaseIndexFrom(path string) ReleaseInfo {
	data, err := os.ReadFile(path)
	if err != nil {
		return ReleaseInfo{} // Present=false
	}
	return parseReleaseIndex(data)
}

// parseReleaseIndex decodes a release-index.json payload.
// The canonical field is platform_release (post-2026-05 release pipeline);
// version is kept as a legacy fallback for older release-index payloads.
// A malformed payload still returns Present=true — the file exists, it's
// just unparseable. The caller distinguishes absence from empty version.
func parseReleaseIndex(data []byte) ReleaseInfo {
	var m struct {
		PlatformRelease string `json:"platform_release"`
		Version         string `json:"version"`
		BuildID         string `json:"build_id"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return ReleaseInfo{Present: true}
	}
	v := m.PlatformRelease
	if v == "" {
		v = m.Version
	}
	return ReleaseInfo{Present: true, Version: v, BuildID: m.BuildID}
}

func (c *Collector) readBundleStatus() AwarenessBundleStatus {
	manifestPath := filepath.Join(awarenessCurrentLink, awarenessManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return AwarenessBundleStatus{Present: false, Status: "MISSING"}
	}
	var m bundleManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return AwarenessBundleStatus{Present: true, Status: "CORRUPT"}
	}
	return AwarenessBundleStatus{
		Present: true,
		Version: m.Version,
		BuildID: m.BuildID,
		Status:  "LOADED",
	}
}

func (c *Collector) collectPKI() PKIObservation {
	caP, caR := observeFile(pkiCACertPath)
	ncP, ncR := observeFile(pkiNodeCertPath)
	nkP, nkR := observeFile(pkiNodeKeyPath)
	return PKIObservation{
		CACertPresent:    caP,
		CACertReadable:   caR,
		NodeCertPresent:  ncP,
		NodeCertReadable: ncR,
		NodeKeyPresent:   nkP,
		NodeKeyReadable:  nkR,
	}
}

// observeFile checks whether a path exists on disk and is readable by the
// current process. These are two independent states — a file can exist but
// be unreadable (wrong permissions/owner). Inlined here to avoid a
// dependency on the services-module fsutil package.
func observeFile(path string) (exists, readable bool) {
	if path == "" {
		return false, false
	}
	if _, err := os.Stat(path); err == nil {
		exists = true
	}
	f, err := os.Open(path)
	if err == nil {
		readable = true
		f.Close()
	}
	return
}

// readScyllaConfig parses key fields from /etc/scylla/scylla.yaml without a YAML library.
// We only need seed_provider addresses, listen_address, rpc_address, and cluster_name.
func (c *Collector) readScyllaConfig() ScyllaConfigObservation {
	data, err := os.ReadFile(scyllaConfigPath)
	if err != nil {
		return ScyllaConfigObservation{Present: false}
	}
	obs := ScyllaConfigObservation{Present: true}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	inSeedProvider := false
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "listen_address:") {
			obs.ListenAddress = extractYAMLValue(trimmed)
		}
		if strings.HasPrefix(trimmed, "rpc_address:") {
			obs.RPCAddress = extractYAMLValue(trimmed)
		}
		if strings.HasPrefix(trimmed, "cluster_name:") {
			obs.ClusterName = strings.Trim(extractYAMLValue(trimmed), "\"'")
		}
		if strings.HasPrefix(trimmed, "seed_provider:") {
			inSeedProvider = true
		}
		if inSeedProvider && strings.HasPrefix(trimmed, "- seeds:") {
			raw := extractYAMLValue(trimmed)
			for _, s := range strings.Split(raw, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					obs.Seeds = append(obs.Seeds, s)
				}
			}
			inSeedProvider = false
		}
	}
	return obs
}

func extractYAMLValue(line string) string {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(line[idx+1:])
}

// collectSystemdUnits reads unit states via systemctl show.
// We query a fixed list of Globular-related units rather than all units.
func (c *Collector) collectSystemdUnits(ctx context.Context) []ServiceObservation {
	units := []string{
		"globular-node-agent.service",
		"globular-mcp.service",
		"globular-cluster-controller.service",
		"globular-workflow.service",
		"globular-repository.service",
		"globular-authentication.service",
		"globular-rbac.service",
		"globular-dns.service",
		"scylla-server.service",
		"minio.service",
		"sidekick.service",
		"envoy.service",
		"etcd.service",
		"keepalived.service",
	}

	var out []ServiceObservation
	for _, unit := range units {
		out = append(out, c.queryUnit(ctx, unit))
	}
	return out
}

func (c *Collector) queryUnit(ctx context.Context, unit string) ServiceObservation {
	obs := ServiceObservation{
		UnitName:    unit,
		Name:        strings.TrimSuffix(unit, ".service"),
		ActiveState: "unknown",
	}

	cmd := exec.CommandContext(ctx, "systemctl", "show",
		"--property=ActiveState,SubState,ExecMainStatus",
		"--value", unit)
	raw, err := cmd.Output()
	if err != nil {
		return obs
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) >= 1 {
		obs.ActiveState = strings.TrimSpace(lines[0])
	}
	if len(lines) >= 2 {
		obs.SubState = strings.TrimSpace(lines[1])
	}
	if len(lines) >= 3 {
		if code, err := strconv.Atoi(strings.TrimSpace(lines[2])); err == nil {
			obs.ExitCode = code
		}
	}

	if obs.ActiveState == "failed" || obs.SubState == "start-limit-hit" {
		obs.LogExcerpt = c.journalExcerpt(ctx, unit, 10)
	}
	return obs
}

// journalExcerpt returns the last n lines from journalctl for a unit.
func (c *Collector) journalExcerpt(ctx context.Context, unit string, lines int) string {
	cmd := exec.CommandContext(ctx, "journalctl", "-u", unit,
		"-n", strconv.Itoa(lines), "--no-pager", "-o", "short")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// knownPorts are the well-known Globular ports the collector reports state for.
// Bind address is irrelevant — the collector reads kernel socket tables so
// the result is true whether the service binds to 0.0.0.0, the node IP, or
// 127.0.0.1. Dialing 127.0.0.1 is wrong on this platform because services
// bind to the node IP per CLAUDE.md hard rule #3 (no localhost for remote
// addresses); a loopback dial would miss every Scylla, etcd, and MinIO
// listener on a real cluster.
var knownPorts = []int{
	9042,  // scylla CQL
	9160,  // scylla thrift
	7000,  // scylla inter-node
	7001,  // scylla inter-node TLS
	7199,  // scylla JMX
	9100,  // prometheus node exporter
	9000,  // minio API
	9001,  // minio console
	2379,  // etcd client
	2380,  // etcd peer
	10004, // workflow
	10260, // MCP
	10101, // authentication
	10104, // rbac
	10006, // dns
	12000, // cluster-controller
	11000, // node-agent
}

// procNetTCPPaths is the default source list for listening-port enumeration.
// Overridden in tests.
var procNetTCPPaths = []string{"/proc/net/tcp", "/proc/net/tcp6"}

// collectPorts reports listener state for every port in knownPorts.
// Reads the kernel socket tables from /proc/net/tcp{,6}; bind-address agnostic.
func (c *Collector) collectPorts(_ context.Context) []PortObservation {
	listening := listeningTCPPortsFromPaths(procNetTCPPaths)
	out := make([]PortObservation, 0, len(knownPorts))
	for _, p := range knownPorts {
		out = append(out, PortObservation{
			Port:      p,
			Protocol:  "tcp",
			Listening: listening[p],
		})
	}
	return out
}

// listeningTCPPortsFromPaths returns the set of TCP ports in LISTEN state.
// Missing /proc/net/tcp6 (IPv6 disabled) is tolerated; any other read error
// is silently swallowed — the caller treats absence of a port as
// "not observed listening," same as the previous dial-based check on failure.
func listeningTCPPortsFromPaths(paths []string) map[int]bool {
	out := make(map[int]bool)
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		parseListeningPorts(f, out)
		f.Close()
	}
	return out
}

// parseListeningPorts reads /proc/net/tcp-formatted lines from r and
// adds every TCP port in LISTEN state (column 4 == "0A") to out.
// Format: per-line whitespace-separated fields, column 2 is
// "HEX_LOCAL_ADDR:HEX_PORT", column 4 is the connection state.
func parseListeningPorts(r io.Reader, out map[int]bool) {
	scanner := bufio.NewScanner(r)
	scanner.Scan() // skip header
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || fields[3] != "0A" {
			continue
		}
		colon := strings.LastIndexByte(fields[1], ':')
		if colon < 0 {
			continue
		}
		port, err := strconv.ParseUint(fields[1][colon+1:], 16, 32)
		if err != nil {
			continue
		}
		out[int(port)] = true
	}
}

