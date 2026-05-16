package enforce

import (
	"bufio"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/globulario/awareness/graph"
)

// ValidateRequiredTests checks that every test declared via //globular:tested_by
// exists in the graph as a test node created by the Go test extractor.
//
// A tested_by edge that points to a non-existent test node → ERROR (test missing).
// The test node exists but has no source file path → WARNING (unverified location).
func ValidateRequiredTests(ctx context.Context, g *graph.Graph) []Finding {
	return ValidateRequiredTestsWithRepo(ctx, g, "")
}

// ValidateRequiredTestsWithRepo checks required tests and optionally resolves
// missing graph test paths by scanning *_test.go functions under repoRoot.
func ValidateRequiredTestsWithRepo(ctx context.Context, g *graph.Graph, repoRoot string) []Finding {
	if g == nil {
		return nil
	}

	// Collect all tested_by edges.
	edges, err := g.EdgesByKind(ctx, graph.EdgeTestedBy)
	if err != nil {
		return []Finding{{
			Code:     "TEST_QUERY_ERROR",
			Severity: SeverityError,
			Message:  "failed to query tested_by edges: " + err.Error(),
		}}
	}

	var findings []Finding
	resolvedTests := discoverTestsByName(repoRoot)
	missingPathSymbolsByTest := make(map[string][]string)
	for _, e := range edges {
		testNode, err := g.FindNode(ctx, e.Dst)
		if err != nil {
			findings = append(findings, Finding{
				Code:     "REQUIRED_TEST_LOOKUP_ERROR",
				Severity: SeverityWarning,
				Symbol:   e.Src,
				Message:  "tested_by lookup failed for " + e.Dst + ": " + err.Error(),
			})
			continue
		}
		if testNode == nil {
			findings = append(findings, Finding{
				Code:     CodeRequiredTestMissing,
				Severity: SeverityError,
				Symbol:   e.Src,
				Message:  "tested_by target '" + e.Dst + "' does not exist in the graph — add a test function named " + stripPrefix(e.Dst, "test:"),
			})
			continue
		}
		if testNode.Path == "" {
			name := testNode.Name
			if strings.TrimSpace(name) == "" {
				name = stripPrefix(e.Dst, "test:")
			}
			if resolvedTests[name] != "" {
				continue
			}
			missingPathSymbolsByTest[name] = append(missingPathSymbolsByTest[name], e.Src)
		}
	}

	// Emit one warning per missing test target (instead of one per tested_by edge)
	// so large YAML fan-out does not create noisy duplicate warnings.
	var missingTestNames []string
	for testName := range missingPathSymbolsByTest {
		missingTestNames = append(missingTestNames, testName)
	}
	sort.Strings(missingTestNames)
	for _, testName := range missingTestNames {
		symbols := uniqueSortedStrings(missingPathSymbolsByTest[testName])
		symbol := ""
		if len(symbols) > 0 {
			symbol = symbols[0]
		}
		msg := "tested_by target '" + testName + "' declared but not yet implemented — add func " + testName + "(t *testing.T) to a *_test.go file"
		if len(symbols) > 1 {
			msg += "; referenced by " + strings.Join(symbols, ", ")
		}
		findings = append(findings, Finding{
			Code:     "REQUIRED_TEST_NO_PATH",
			Severity: SeverityWarning,
			Symbol:   symbol,
			Message:  msg,
		})
	}

	return findings
}

func discoverTestsByName(repoRoot string) map[string]string {
	out := make(map[string]string)
	root := strings.TrimSpace(repoRoot)
	if root == "" {
		return out
	}
	testFnRe := regexp.MustCompile(`^func\s+(Test[[:alnum:]_]+)\s*\(`)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == ".globular" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			m := testFnRe.FindStringSubmatch(line)
			if len(m) != 2 {
				continue
			}
			name := m[1]
			if out[name] == "" {
				out[name] = path
			}
		}
		return nil
	})
	return out
}

// stripPrefix removes a prefix from s. Returns s unchanged if prefix not present.
func stripPrefix(s, prefix string) string {
	if len(s) > len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func uniqueSortedStrings(in []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
