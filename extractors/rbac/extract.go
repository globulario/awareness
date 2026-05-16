// Package rbac extracts RBAC policy metadata from the Globular policy directory
// and emits role, permission, and binding nodes into the awareness graph.
//
// Source tier: installed_metadata
//
// Safety rules (CRITICAL — never relax):
//   - Only read .json files in the policy dir
//   - NEVER read files with "token", "secret", "credential", "key", "jwt" in the name
//   - Redact any JSON field named token, password, secret, credential, key
//   - Missing directories return CollectorHealth{Status:"skipped"}, not an error
package rbac

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/awareness/graph"
)

// DefaultPolicyDir is the canonical Globular RBAC policy directory.
var DefaultPolicyDir = "/var/lib/globular/policy/rbac"

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

// sensitiveFilePatterns are filename substrings that disqualify a JSON file
// from being read. This prevents accidentally reading credential files that
// may be co-located with the RBAC policy.
var sensitiveFilePatterns = []string{
	"token", "secret", "credential", "key", "jwt", "password",
}

// sensitiveJSONFields are JSON object keys whose values must be redacted before
// storing in the graph. This is a defence-in-depth measure.
var sensitiveJSONFields = map[string]bool{
	"token":      true,
	"password":   true,
	"secret":     true,
	"credential": true,
	"key":        true,
	"jwt":        true,
	"api_key":    true,
	"auth_token": true,
}

// isSensitiveFile returns true if the filename should never be read.
func isSensitiveFile(name string) bool {
	lower := strings.ToLower(name)
	for _, pat := range sensitiveFilePatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

// clusterRolesFile is the canonical RBAC policy filename.
const clusterRolesFile = "cluster-roles.json"

// rbacFileV2 mirrors the on-disk format of cluster-roles.json (version 2.0).
// Each role maps to a flat list of permission strings in the form
// "service.resource.action" (e.g. "workflow.read", "repository.artifact.list").
type rbacFileV2 struct {
	Version string              `json:"version"`
	Roles   map[string][]string `json:"roles"`
}

// Extract reads the RBAC policy file and emits role, permission, and policy
// file nodes into the graph.
//
// If policyDir does not exist, returns CollectorHealth{Status:"skipped"}.
func Extract(ctx context.Context, g *graph.Graph, policyDir string) (CollectorHealth, error) {
	health := CollectorHealth{
		CollectorID: "rbac",
		SourceTier:  sourceTierInstalled,
	}

	if _, err := os.Stat(policyDir); os.IsNotExist(err) {
		health.Status = "skipped"
		health.Error = fmt.Sprintf("rbac policy dir not found: %s", policyDir)
		return health, nil
	}

	collectedAt := time.Now().Unix()
	parseErrors := 0

	err := filepath.WalkDir(policyDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()

		// Only process .json files.
		if !strings.HasSuffix(name, ".json") {
			return nil
		}

		// SAFETY: skip files with sensitive name patterns.
		if isSensitiveFile(name) {
			health.Notes = append(health.Notes,
				fmt.Sprintf("skipped sensitive-named file: %s", name))
			return nil
		}

		n, err := indexPolicyFile(ctx, g, path, name, collectedAt)
		if err != nil {
			parseErrors++
			health.Notes = append(health.Notes,
				fmt.Sprintf("skip %s: %v", name, err))
			return nil
		}
		health.NodesEmitted += n
		return nil
	})
	if err != nil {
		health.Status = "failed"
		health.Error = err.Error()
		return health, err
	}

	if parseErrors > 0 && health.NodesEmitted == 0 {
		health.Status = "failed"
		health.Error = fmt.Sprintf("%d parse errors, 0 nodes indexed", parseErrors)
	} else if parseErrors > 0 {
		health.Status = "partial"
	} else {
		health.Status = "ok"
	}
	return health, nil
}

// indexPolicyFile reads a single RBAC policy JSON file and emits nodes.
// Returns the number of nodes emitted.
func indexPolicyFile(ctx context.Context, g *graph.Graph, path, name string, collectedAt int64) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	// Decode into a raw map first so we can redact sensitive fields before
	// doing structured parsing.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return 0, fmt.Errorf("json unmarshal: %w", err)
	}
	redactSensitiveFields(raw)

	// Re-marshal the sanitised map into structured form.
	sanitised, err := json.Marshal(raw)
	if err != nil {
		return 0, fmt.Errorf("re-marshal: %w", err)
	}

	var policy rbacFileV2
	if err := json.Unmarshal(sanitised, &policy); err != nil {
		return 0, fmt.Errorf("parse policy: %w", err)
	}

	if len(policy.Roles) == 0 {
		return 0, nil
	}

	emitted := 0

	// Emit the policy file node.
	fileNodeID := "rbac_policy_file:" + name
	if err := g.AddNode(ctx, graph.Node{
		ID:   fileNodeID,
		Type: graph.NodeTypeRBACPolicyFile,
		Name: name,
		Path: path,
		Summary: fmt.Sprintf("RBAC policy file: %s (version %s, %d roles)",
			name, policy.Version, len(policy.Roles)),
		Metadata: map[string]any{
			"source_tier":  sourceTierInstalled,
			"collected_at": collectedAt,
			"version":      policy.Version,
			"role_count":   len(policy.Roles),
		},
	}); err != nil {
		return 0, fmt.Errorf("AddNode policy file: %w", err)
	}
	emitted++

	for roleName, permissions := range policy.Roles {
		roleNodeID := "rbac_role:" + roleName

		if err := g.AddNode(ctx, graph.Node{
			ID:   roleNodeID,
			Type: graph.NodeTypeRBACRole,
			Name: roleName,
			Path: path,
			Summary: fmt.Sprintf("RBAC role: %s (%d permissions)",
				roleName, len(permissions)),
			Metadata: map[string]any{
				"source_tier":      sourceTierInstalled,
				"collected_at":     collectedAt,
				"permission_count": len(permissions),
				"policy_file":      name,
			},
		}); err != nil {
			return emitted, fmt.Errorf("AddNode role %s: %w", roleName, err)
		}
		emitted++

		// Wire role → policy file.
		_ = g.AddEdge(ctx, graph.Edge{
			Src:  fileNodeID,
			Kind: graph.EdgeOwns,
			Dst:  roleNodeID,
			Metadata: map[string]any{
				"source_tier": sourceTierInstalled,
			},
		})

		// Emit permission nodes. Each permission string has the form
		// "service.resource.action" (e.g. "workflow.read",
		// "repository.artifact.list"). We parse it into components for
		// structured metadata.
		for _, permStr := range permissions {
			permNodeID := buildPermissionID(roleName, permStr)

			parts := strings.SplitN(permStr, ".", 3)
			service, resource, verb := "", "", permStr
			switch len(parts) {
			case 1:
				verb = parts[0]
			case 2:
				service = parts[0]
				verb = parts[1]
			case 3:
				service = parts[0]
				resource = parts[1]
				verb = parts[2]
			}

			permMeta := map[string]any{
				"source_tier":  sourceTierInstalled,
				"collected_at": collectedAt,
				"permission":   permStr,
				"role":         roleName,
				"service":      service,
				"resource":     resource,
				"verb":         verb,
			}

			if err := g.AddNode(ctx, graph.Node{
				ID:   permNodeID,
				Type: graph.NodeTypeRBACPermission,
				Name: permStr,
				Path: path,
				Summary: fmt.Sprintf("permission: %s (role: %s)",
					permStr, roleName),
				Metadata: permMeta,
			}); err != nil {
				// Non-fatal: log and continue.
				fmt.Fprintf(os.Stderr, "rbac: AddNode permission %s: %v\n",
					permNodeID, err)
				continue
			}
			emitted++

			_ = g.AddEdge(ctx, graph.Edge{
				Src:  roleNodeID,
				Kind: graph.EdgeRoleGrantsPermission,
				Dst:  permNodeID,
				Metadata: map[string]any{
					"source_tier": sourceTierInstalled,
					"permission":  permStr,
				},
			})
		}
	}

	return emitted, nil
}

// buildPermissionID returns a stable graph node ID for a role+permission pair.
// IDs are truncated at 200 characters to stay within reasonable bounds.
func buildPermissionID(roleName, permStr string) string {
	id := "rbac_permission:" + roleName + ":" + permStr
	if len(id) > 200 {
		id = id[:200]
	}
	return id
}

// redactSensitiveFields recursively walks a decoded JSON map and replaces
// the values of sensitive fields with the string "[REDACTED]".
// This is applied before any content is stored in the graph.
func redactSensitiveFields(m map[string]any) {
	for k, v := range m {
		if sensitiveJSONFields[strings.ToLower(k)] {
			m[k] = "[REDACTED]"
			continue
		}
		switch child := v.(type) {
		case map[string]any:
			redactSensitiveFields(child)
		case []any:
			redactSensitiveSlice(child)
		}
	}
}

func redactSensitiveSlice(s []any) {
	for _, item := range s {
		if m, ok := item.(map[string]any); ok {
			redactSensitiveFields(m)
		}
	}
}
