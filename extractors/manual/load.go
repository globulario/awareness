// Package manual loads hand-authored awareness truth files into the graph.
// Files are discovered by walking docsDir recursively. Each YAML file's
// top-level key determines which loader handles it; unknown keys are skipped
// so config, proposal, and incident files can coexist in the same tree.
package manual

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
)

// YAMLRole classifies a YAML file under docs/awareness/ by how it contributes
// (or not) to the awareness graph. Used by the assurance freshness layer to
// distinguish "I don't know what this file is" from "I know exactly what it
// is and that it doesn't affect graph staleness."
//
// As of P1-3 (claude_awareness_open_requirements.md), a YAML file may declare
// its role explicitly via a top-level `awareness_role: graph|config|seed|none`
// key. When the declaration is present and valid, it wins over the top-key
// heuristic — see ClassifyYAML. The heuristic remains the fallback for legacy
// files without a declaration.
type YAMLRole string

const (
	// YAMLRoleGraph: top-level key is in dispatchTable. The file builds graph
	// nodes and edges via the manual loader. Edits to it must mark the graph
	// stale; the graph build's own age tracker handles the staleness signal.
	YAMLRoleGraph YAMLRole = "graph"
	// YAMLRoleSeed: graph-contributing, but loaded by an extractor OUTSIDE
	// the manual loader — failuregraph seeds (top key `id`), doctor
	// detector_mapping (top key `detector_mappings`), contracts, etc. Behaves
	// identically to YAMLRoleGraph for staleness; the distinct label exists
	// so reports can show which subsystem owns each file.
	YAMLRoleSeed YAMLRole = "seed"
	// YAMLRoleConfig: top-level key is in configOnlyKeys. The file is loaded
	// by some subsystem but does not contribute graph nodes/edges. Edits do
	// NOT need to mark the graph stale. Counted as informational.
	YAMLRoleConfig YAMLRole = "config"
	// YAMLRoleNone: the file is intentionally ignored by every awareness
	// subsystem. No graph contribution, no staleness signal, no informational
	// alarm. Use this for scratch notes, draft proposals, README-style
	// content that happens to live under docs/awareness/.
	YAMLRoleNone YAMLRole = "none"
	// YAMLRoleUnknown: no explicit declaration AND no top-key match. We
	// can't classify the file without human input. Caps trust at stale_unknown
	// per the assurance freshness rules.
	YAMLRoleUnknown YAMLRole = "unknown"
)

// validDeclaredRoles is the set of roles an author may write into a
// top-level `awareness_role:` key. YAMLRoleUnknown is intentionally
// excluded — "unknown" is only the fallback when no declaration exists AND
// the heuristic can't classify.
var validDeclaredRoles = map[YAMLRole]bool{
	YAMLRoleGraph:  true,
	YAMLRoleSeed:   true,
	YAMLRoleConfig: true,
	YAMLRoleNone:   true,
}

// ClassifyYAML returns the role for a YAML file's bytes. Priority order:
//
//  1. An explicit top-level `awareness_role: graph|config|seed|none`
//     declaration wins. Invalid values are ignored (fall through).
//  2. Top-key heuristic via ClassifyYAMLByTopKey.
//  3. YAMLRoleUnknown if neither produces a classification.
//
// Returns a non-nil error only when the bytes parse but `awareness_role:` is
// present with a non-string or unrecognised value — the caller should
// surface it as a warning. In every other failure mode (unreadable file,
// missing top key) the function falls back silently so legacy files keep
// working unchanged.
func ClassifyYAML(data []byte) (YAMLRole, error) {
	if role, ok, err := parseAwarenessRoleDeclaration(data); ok {
		return role, err
	} else if err != nil {
		// Declaration was present but malformed. Surface the error but still
		// fall back to the heuristic so a typo doesn't block the whole report.
		fallback := classifyByTopKey(data)
		return fallback, err
	}
	return classifyByTopKey(data), nil
}

// parseAwarenessRoleDeclaration reads the top-level `awareness_role:` field
// from a YAML file.
//
//   - ok=true,  err=nil          → field present and valid
//   - ok=false, err=non-nil      → field present but invalid value/type
//   - ok=false, err=nil          → field absent (or doc parse failed —
//     deliberate: a wholly-unparseable file is not P1-3's concern)
func parseAwarenessRoleDeclaration(data []byte) (YAMLRole, bool, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return YAMLRoleUnknown, false, nil
	}
	raw, present := doc["awareness_role"]
	if !present {
		return YAMLRoleUnknown, false, nil
	}
	s, isString := raw.(string)
	if !isString {
		return YAMLRoleUnknown, false, fmt.Errorf("awareness_role must be a string, got %T", raw)
	}
	role := YAMLRole(strings.TrimSpace(strings.ToLower(s)))
	if !validDeclaredRoles[role] {
		return YAMLRoleUnknown, false, fmt.Errorf("invalid awareness_role %q (want graph|config|seed|none)", role)
	}
	return role, true, nil
}

// classifyByTopKey is the heuristic fallback used by ClassifyYAML. It looks
// at the first classifying top-level key — skipping the meta
// `awareness_role` key when present, since that key declares intent and is
// not itself a content classifier.
func classifyByTopKey(data []byte) YAMLRole {
	topKey, err := firstClassifyingKey(data)
	if err != nil || topKey == "" {
		return YAMLRoleUnknown
	}
	return ClassifyYAMLByTopKey(topKey)
}

// firstClassifyingKey returns the first top-level key in data that is NOT
// the meta `awareness_role` declaration. If the file has only an
// awareness_role declaration with no other key, returns "" (no
// classifying content to anchor on).
func firstClassifyingKey(data []byte) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return "", nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return "", nil
	}
	// YAML mapping nodes store [key, value, key, value, ...] in Content.
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		if key == "awareness_role" {
			continue
		}
		return key, nil
	}
	return "", nil
}

// GraphYAMLKeys returns the set of top-level YAML keys whose files are
// graph-contributing (have a registered loader).
func GraphYAMLKeys() map[string]bool {
	out := make(map[string]bool, len(dispatchTable))
	for k := range dispatchTable {
		out[k] = true
	}
	return out
}

// ConfigYAMLKeys returns the set of top-level YAML keys whose files are
// explicitly config-only (loaded by some subsystem but not graph-contributing).
func ConfigYAMLKeys() map[string]bool {
	out := make(map[string]bool, len(configOnlyKeys))
	for k := range configOnlyKeys {
		out[k] = true
	}
	return out
}

// externallyHandledGraphKeys lists top-level YAML keys that are graph-
// contributing but loaded by extractors OUTSIDE the manual package (the
// doctor mapping extractor, contract loaders, etc.). Edits to such files
// must mark the graph stale, but the manual loader's dispatchTable does
// not know how to load them — by design.
//
// Add a key here when a new subsystem starts shipping graph-contributing
// YAMLs that don't go through the manual loader. Failuregraph seeds live
// in seedKeys instead so they get the distinct YAMLRoleSeed label.
var externallyHandledGraphKeys = map[string]bool{
	"detector_mappings": true, // detector_mapping.yaml — loaded by doctor mapping extractor
}

// seedKeys lists top-level YAML keys whose files are graph-contributing
// AND classified specifically as seeds (failuregraph seeds, runbook seeds,
// etc.). Functionally identical to externallyHandledGraphKeys for
// staleness, but the distinct role label lets reports identify which
// subsystem owns the file.
var seedKeys = map[string]bool{
	"id": true, // failuregraph_seeds/*.yaml — loaded by failurelearning.RebuildFromSeeds
}

// ClassifyYAMLByTopKey returns the role for a top-level YAML key. Use this
// when you've already parsed the key (e.g. inside a file walker that needs
// to read content anyway). Prefer ClassifyYAML when you have the file
// bytes — it honours explicit `awareness_role:` declarations first.
func ClassifyYAMLByTopKey(topKey string) YAMLRole {
	if dispatchTable[topKey] != nil {
		return YAMLRoleGraph
	}
	if externallyHandledGraphKeys[topKey] {
		return YAMLRoleGraph
	}
	if seedKeys[topKey] {
		return YAMLRoleSeed
	}
	if configOnlyKeys[topKey] {
		return YAMLRoleConfig
	}
	return YAMLRoleUnknown
}

// dispatchTable maps top-level YAML keys to the loader that handles them.
var dispatchTable = map[string]func(context.Context, *graph.Graph, string) error{
	"invariants":      LoadInvariants,
	"failure_modes":   LoadFailureModes,
	"forbidden_fixes": LoadForbiddenFixes,
	"services":        LoadServices,
	"patterns":        LoadPatterns,
	"design_patterns": LoadDesignPatterns,
	"decision_rules":  LoadDecisionRules,
	"causal_rules":    LoadCausalRules,
}

// configOnlyKeys are top-level YAML keys that identify files intentionally not
// loaded into the graph. These are config, pipeline state, or operational data
// files that coexist in the awareness tree but have no graph representation.
// Files with these keys are excluded from the WalkUnindexed report so that
// health_pulse does not flag them as coverage gaps.
var configOnlyKeys = map[string]bool{
	"aliases":      true, // context_aliases.yaml — loaded by alias matcher
	"suppressions": true, // audit_suppressions.yaml — suppression config
	"fix_cases":    true, // fix_cases.yaml — loaded by fixledger
	"guardrails":   true, // guardrails.yaml — loaded by enforcement
	"files":        true, // high_risk_files.yaml — hook config
	"rules":        true, // learning_rules.yaml — pipeline config
	"trust":        true, // knowledge/path_weights.yaml — scoring config
	"allowlist":    true, // knowledge/scan_allowlist.yaml — scanner config
	"queries":      true, // knowledge/metric_queries.yaml — Prometheus query templates
	"thresholds":   true, // knowledge/metric_thresholds.yaml — metric thresholds
	"playbooks":    true, // knowledge/agent_playbooks.yaml — agent playbooks
	"dns_zones":    true, // knowledge/dns_zones.yaml — read by dns extractor, not manual loader
	"incident_id":  true, // incidents/*.yaml — dynamic incident records
	"proposal":     true, // proposals/*.yaml — proposal pipeline records
	"last_updated": true, // status_tracker.yaml — operational status
	"version":      true, // contracts/*.yaml — data contract docs, no graph loader
}

// UnindexedFile describes a YAML file whose top-level key has no registered loader.
// These files are silently skipped by LoadAll and their content does not reach the graph.
type UnindexedFile struct {
	Path   string // path relative to docsDir, or absolute if docsDir is empty
	TopKey string // the unrecognized top-level YAML key
}

// WalkUnindexed returns all YAML files under docsDir whose top-level key is not
// registered in the dispatch table. The caller can inspect these to decide whether
// new loaders are needed or whether the files are intentionally config-only.
func WalkUnindexed(docsDir string) ([]UnindexedFile, error) {
	var out []UnindexedFile
	err := filepath.WalkDir(docsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		key, err := topLevelKey(data)
		if err != nil || key == "" {
			return nil
		}
		if _, known := dispatchTable[key]; !known {
			if configOnlyKeys[key] {
				return nil // intentional config-only file — not a coverage gap
			}
			if externallyHandledGraphKeys[key] {
				return nil // graph-contributing but loaded by another extractor — not a coverage gap
			}
			if seedKeys[key] {
				return nil // seed file (failuregraph seeds etc.) — not a coverage gap
			}
			rel, err := filepath.Rel(docsDir, path)
			if err != nil {
				rel = path
			}
			out = append(out, UnindexedFile{Path: rel, TopKey: key})
		}
		return nil
	})
	return out, err
}

// LoadAll walks docsDir recursively and loads every YAML file whose top-level
// key matches a known loader. Unknown top-level keys are silently skipped.
// Missing docsDir is silently skipped.
func LoadAll(ctx context.Context, g *graph.Graph, docsDir string) error {
	return filepath.WalkDir(docsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: skip
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		if loadErr := dispatchFile(ctx, g, path); loadErr != nil {
			return fmt.Errorf("manual loader %s: %w", path, loadErr)
		}
		return nil
	})
}

// dispatchFile reads path, detects its top-level YAML key, and calls the
// matching loader. Files with unknown keys are silently skipped.
func dispatchFile(ctx context.Context, g *graph.Graph, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // missing or unreadable: skip
	}

	key, err := topLevelKey(data)
	if err != nil || key == "" {
		return nil // unparseable or empty: skip
	}

	loader, ok := dispatchTable[key]
	if !ok {
		return nil // unknown type: skip
	}
	return loader(ctx, g, path)
}

// TopLevelKey returns the first key in the top-level YAML mapping of data.
// Exported so callers (e.g. the assurance freshness layer) can classify
// already-loaded YAML content without re-reading the file from disk.
func TopLevelKey(data []byte) (string, error) { return topLevelKey(data) }

// topLevelKey returns the first key in the top-level YAML mapping of data.
func topLevelKey(data []byte) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return "", nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode || len(root.Content) == 0 {
		return "", nil
	}
	return root.Content[0].Value, nil
}
