package graph

import (
	"fmt"
	"os"
)

// bundleOnlyTables names the awareness tables whose canonical data lives in
// the signed, content-addressed bundle and is never written at runtime. They
// are populated by the bundle build process (extractors, graph construction)
// and consumed read-only at runtime.
//
// In a composite graph these tables are DROP-ed from the writable runtime
// database after migration. SQLite resolves an unqualified table reference
// against the main database first, then attached databases in attach order;
// with the table absent from main, unqualified reads land on the ATTACHed
// bundle. Indexes attached to these tables cascade-drop automatically.
//
// All other awareness tables — sessions, coordination, experience, semantic
// diff reports, etc. — remain in main and accept writes normally.
var bundleOnlyTables = []string{
	"nodes",
	"edges",
	"invariants",
	"failure_modes",
	"graph_builds",
	"context_aliases",
}

// OpenComposite opens an awareness graph composed of a writable runtime
// database with the signed bundle ATTACHed read-only. It is the correct
// constructor for the MCP and other long-running processes that consume
// the bundle as the authoritative graph while needing to write session,
// coordination, experience, and learning data alongside it.
//
// Contract:
//   - bundlePath must exist and contain the bundle's graph.db.
//   - runtimePath is opened read-write; the parent directory is created
//     if absent. The first call materialises the runtime schema; later
//     calls reuse the existing file.
//   - The bundle is opened via SQLite's URI form with mode=ro&immutable=1,
//     so even concurrent bundle reinstall under runtime cannot corrupt
//     this handle.
//   - Static graph tables (nodes, edges, invariants, failure_modes,
//     graph_builds, context_aliases) live only in the bundle. Unqualified
//     reads against those names resolve through ATTACH automatically.
//     Cross-database JOINs (e.g., experience_entries LEFT JOIN nodes)
//     work without any query change.
//   - Attempting to write a bundle-only table returns SQLite's "attempt
//     to write a readonly database" error. Writers must address runtime
//     tables.
//
// Composition order: open runtime (which migrates the full schema), drop
// the bundle-canonical tables, then ATTACH. Dropping after migrate is
// simpler than maintaining a parallel runtime-only schema string; the few
// CREATE/ALTER statements wasted on tables we immediately drop are
// idempotent and cost a negligible fraction of startup.
func OpenComposite(bundlePath, runtimePath string) (*Graph, error) {
	if bundlePath == "" {
		return nil, fmt.Errorf("awareness graph (composite): bundlePath is empty")
	}
	if runtimePath == "" {
		return nil, fmt.Errorf("awareness graph (composite): runtimePath is empty")
	}
	if _, err := os.Stat(bundlePath); err != nil {
		return nil, fmt.Errorf("awareness graph (composite): stat bundle %s: %w", bundlePath, err)
	}

	g, err := Open(runtimePath)
	if err != nil {
		return nil, fmt.Errorf("awareness graph (composite): open runtime %s: %w", runtimePath, err)
	}

	for _, t := range bundleOnlyTables {
		if _, err := g.db.Exec("DROP TABLE IF EXISTS " + t); err != nil {
			g.db.Close()
			return nil, fmt.Errorf("awareness graph (composite): drop bundle-only table %s from runtime: %w", t, err)
		}
	}

	// SQLite URI filenames are enabled by default in mattn/go-sqlite3 and
	// inside ATTACH statements; mode=ro forbids writes and immutable=1
	// promises the file will not change while attached, which lets SQLite
	// skip the WAL handshake entirely.
	attachDSN := fmt.Sprintf("file:%s?mode=ro&immutable=1", bundlePath)
	if _, err := g.db.Exec(fmt.Sprintf("ATTACH DATABASE '%s' AS bundle", attachDSN)); err != nil {
		g.db.Close()
		return nil, fmt.Errorf("awareness graph (composite): attach bundle %s: %w", bundlePath, err)
	}

	return g, nil
}
