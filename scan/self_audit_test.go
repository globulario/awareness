package scan_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/globulario/awareness/scan"
)

// TestAwarenessSelfAudit asserts that the awareness source tree itself
// contains zero critical violations of the rules the awareness scanner
// enforces on everything else.
//
// Motivating incident — 2026-05-09 commit d052a0e8 introduced
// fmt.Sprintf("127.0.0.1:%d", p.Port) into awareness/evidence/collector.go.
// The scanner's hard_rule.no_localhost rule would have flagged it as
// critical; pre-commit was expected to gate it. Five days later commit
// 667a984a removed the violation after live verification on ryzen surfaced
// every Scylla/etcd/MinIO listener as missing.
//
// This meta-test closes the loop: whatever the developer-side pre-commit
// configuration looks like, a critical violation in the awareness tree
// fails CI before the bundle ships. The bug shape that hurt the cluster
// for five days cannot return silently.
//
// Scope: production awareness Go files only.
//   - *_test.go is excluded — the scan_allowlist.yaml under
//     docs/awareness/knowledge already covers that pattern with
//     "test fixtures may use loopback addresses".
//   - vendor and generated *pb.go directories are skipped.
//
// On failure, every finding is printed with file:line, knowledge_id, and
// safe alternative, so the maintainer can decide: real bug → fix;
// intentional → add to scan_allowlist.yaml with a reason.
func TestAwarenessSelfAudit_NoCriticalViolations(t *testing.T) {
	root := awarenessSourceRoot(t)
	allowlist := scan.LoadAllowlist(awarenessDocsDir(t))

	type criticalFinding struct {
		file        string
		line        int
		patternID   string
		knowledgeID string
		snippet     string
		safe        string
	}
	var crits []criticalFinding

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || base == ".git" || strings.HasSuffix(base, "pb") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		findings, ferr := scan.ScanGoFile(path, nil)
		if ferr != nil {
			t.Logf("scan %s: %v", path, ferr)
			return nil
		}
		for _, f := range findings {
			if f.Severity != "critical" {
				continue
			}
			// Skip findings the operator-controlled allowlist suppresses.
			// Each suppressed entry must carry a Reason in scan_allowlist.yaml.
			if scan.AllowlistMatch(f, allowlist) != nil {
				continue
			}
			crits = append(crits, criticalFinding{
				file:        f.File,
				line:        f.Line,
				patternID:   f.PatternID,
				knowledgeID: f.KnowledgeID,
				snippet:     f.Snippet,
				safe:        f.SafeAlternative,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(crits) == 0 {
		return
	}

	t.Errorf("awareness source tree has %d critical scan violation(s):", len(crits))
	for _, c := range crits {
		// Display the path relative to the awareness root so the location
		// is readable regardless of CI checkout layout.
		rel, err := filepath.Rel(root, c.file)
		if err != nil {
			rel = c.file
		}
		t.Errorf("  awareness/%s:%d  [%s / %s]  %q\n    safe: %s",
			rel, c.line, c.patternID, c.knowledgeID, c.snippet, c.safe)
	}
	t.Logf("If a finding is a legitimate exception, add it to " +
		"docs/awareness/knowledge/scan_allowlist.yaml with a reason. " +
		"Otherwise fix the violation.")
}

// awarenessSourceRoot returns the absolute path to the awareness package
// directory (parent of scan/). Using runtime.Caller keeps the test
// resilient to the cwd `go test` chooses.
func awarenessSourceRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), ".."))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Fatalf("awareness root %s missing or not a directory: %v", root, err)
	}
	return root
}

// awarenessDocsDir returns the absolute path to docs/awareness, which
// houses the scan allowlist. Resolved from the test file's location so
// the test runs regardless of where `go test` was launched.
func awarenessDocsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	// scan/self_audit_test.go is at golang/awareness/scan/; docs are at
	// repo_root/docs/awareness/.
	docs, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "docs", "awareness"))
	if err != nil {
		t.Fatal(err)
	}
	return docs
}
