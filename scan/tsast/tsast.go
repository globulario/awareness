// Package tsast provides a tolerant line-scanner for TypeScript/React source files.
//
// It extracts frontend constructs — components, routes, backend calls, state atoms,
// hooks, permission checks — without requiring a full TypeScript compiler. Partial
// results are emitted on parse errors rather than aborting the scan.
package tsast

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Finding represents a discovered frontend construct in a TypeScript/React file.
type Finding struct {
	File       string            `json:"file"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	Line       int               `json:"line"`
	Properties map[string]string `json:"properties,omitempty"`
}

// Kind values for Finding.Kind.
const (
	KindComponent       = "component"
	KindRoute           = "route"
	KindBackendCall     = "backend_call"
	KindStateAtom       = "state_atom"
	KindHook            = "hook"
	KindPermissionCheck = "permission_check"
	KindTest            = "test"
	KindStory           = "story"
	KindLayoutSignal    = "layout_signal"
)

// ─── Compiled patterns ────────────────────────────────────────────────────────

var (
	// Components: export function/const with uppercase name, or React.FC typing.
	reExportFunc  = regexp.MustCompile(`(?:export\s+(?:default\s+)?function)\s+([A-Z][A-Za-z0-9_]*)`)
	reExportConst = regexp.MustCompile(`export\s+const\s+([A-Z][A-Za-z0-9_]*)\s*[=:(]`)
	reReactFC     = regexp.MustCompile(`const\s+([A-Z][A-Za-z0-9_]*)\s*:\s*(?:React\.)?(?:FC|FunctionComponent|ComponentType|VFC)`)
	reMemoForward = regexp.MustCompile(`(?:memo|forwardRef)\s*\(\s*([A-Z][A-Za-z0-9_]*)`)

	// Routes.
	reRoutePath    = regexp.MustCompile(`path\s*:\s*["']([^"']+)["']`)
	reRouteJSX     = regexp.MustCompile(`<Route[^>]+path=["']([^"']+)["']`)
	reRouterAdd    = regexp.MustCompile(`router\.(?:add|push|get|use)\s*\(\s*["']([^"']+)["']`)

	// State atoms.
	reStateHook = regexp.MustCompile(`\b(useState|useReducer|useMemo|useSelector|useStore|useAtom)\s*[\(<]`)
	reSignal     = regexp.MustCompile(`\b(signal|atom|writable|readable)\s*\(`)
	reZustand    = regexp.MustCompile(`create\s*\(\s*\(set\b`)

	// Backend calls.
	reNewClient  = regexp.MustCompile(`new\s+([A-Z][A-Za-z0-9]*(?:Client|Service|API|Sdk|Manager))\s*\(`)
	reFetch      = regexp.MustCompile(`\bfetch\s*\(`)
	reAxios      = regexp.MustCompile(`\baxios\s*\.`)
	reGRPC       = regexp.MustCompile(`\bgrpc\.`)
	reUseQuery   = regexp.MustCompile(`\b(useQuery|useMutation|useInfiniteQuery|useLazyQuery)\s*[(<]`)

	// Permission checks.
	rePermFunc = regexp.MustCompile(`\b(can|hasPermission|checkPermission|usePermission|useRBAC|isAllowed|isForbidden)\s*\(`)
	rePermGate = regexp.MustCompile(`<\s*(PermissionGate|RBACGuard|AuthGuard|ProtectedRoute)\b`)
	rePermProp = regexp.MustCompile(`\brbac\s*\.`)

	// Custom hooks (use + uppercase continuation).
	reHookExport = regexp.MustCompile(`(?:export\s+(?:default\s+)?(?:function|const))\s+(use[A-Z][A-Za-z0-9_]*)`)

	// Layout signals: responsive/visibility/accessibility markers.
	reLayoutSignal = regexp.MustCompile(`\b(?:hidden|overflow|truncate|sr-only|md:|lg:|xl:|sm:|aria-[a-z]|role=)`)
)

// isFrontendFile returns true for .ts, .tsx, .js, .jsx files.
func isFrontendFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx"
}

func isTestFile(path string) bool {
	base := filepath.Base(path)
	return strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") ||
		strings.HasSuffix(base, "_test.ts") || strings.HasSuffix(base, "_test.tsx")
}

func isStoryFile(path string) bool {
	base := filepath.Base(path)
	return strings.Contains(base, ".stories.") || strings.Contains(base, ".story.")
}

// ScanFile scans a single TypeScript/React file and returns all found constructs.
// If the file cannot be read, an error is returned. Partial results are never
// discarded — the scanner emits what it found before any failure.
func ScanFile(path string) ([]Finding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var findings []Finding
	seenComponents := map[string]bool{}
	hasLayoutSignal := false

	if isTestFile(path) {
		findings = append(findings, Finding{
			File: path,
			Kind: KindTest,
			Name: filepath.Base(path),
			Line: 0,
		})
	}
	if isStoryFile(path) {
		findings = append(findings, Finding{
			File: path,
			Kind: KindStory,
			Name: filepath.Base(path),
			Line: 0,
		})
	}

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// ── Components ──────────────────────────────────────────────────────────
		for _, re := range []*regexp.Regexp{reExportFunc, reExportConst, reReactFC, reMemoForward} {
			if m := re.FindStringSubmatch(line); m != nil {
				name := m[1]
				if !seenComponents[name] {
					seenComponents[name] = true
					findings = append(findings, Finding{
						File: path,
						Kind: KindComponent,
						Name: name,
						Line: lineNum,
					})
				}
			}
		}

		// ── Custom hooks ─────────────────────────────────────────────────────────
		if m := reHookExport.FindStringSubmatch(line); m != nil {
			findings = append(findings, Finding{
				File: path,
				Kind: KindHook,
				Name: m[1],
				Line: lineNum,
			})
		}

		// ── Routes ───────────────────────────────────────────────────────────────
		for _, re := range []*regexp.Regexp{reRoutePath, reRouteJSX, reRouterAdd} {
			if m := re.FindStringSubmatch(line); m != nil {
				findings = append(findings, Finding{
					File: path,
					Kind: KindRoute,
					Name: m[1],
					Line: lineNum,
				})
			}
		}

		// ── State atoms ──────────────────────────────────────────────────────────
		for _, re := range []*regexp.Regexp{reStateHook, reSignal} {
			if m := re.FindStringSubmatch(line); m != nil {
				findings = append(findings, Finding{
					File: path,
					Kind: KindStateAtom,
					Name: m[1],
					Line: lineNum,
				})
			}
		}
		if reZustand.MatchString(line) {
			findings = append(findings, Finding{
				File: path,
				Kind: KindStateAtom,
				Name: "zustand_store",
				Line: lineNum,
			})
		}

		// ── Backend calls ────────────────────────────────────────────────────────
		if m := reNewClient.FindStringSubmatch(line); m != nil {
			findings = append(findings, Finding{
				File: path,
				Kind: KindBackendCall,
				Name: m[1],
				Line: lineNum,
			})
		}
		for _, re := range []*regexp.Regexp{reUseQuery} {
			if m := re.FindStringSubmatch(line); m != nil {
				findings = append(findings, Finding{
					File: path,
					Kind: KindBackendCall,
					Name: m[1],
					Line: lineNum,
				})
			}
		}
		if reFetch.MatchString(line) {
			findings = append(findings, Finding{
				File: path,
				Kind: KindBackendCall,
				Name: "fetch",
				Line: lineNum,
			})
		}
		if reAxios.MatchString(line) {
			findings = append(findings, Finding{
				File: path,
				Kind: KindBackendCall,
				Name: "axios",
				Line: lineNum,
			})
		}
		if reGRPC.MatchString(line) {
			findings = append(findings, Finding{
				File: path,
				Kind: KindBackendCall,
				Name: "grpc",
				Line: lineNum,
			})
		}

		// ── Permission checks ────────────────────────────────────────────────────
		for _, re := range []*regexp.Regexp{rePermFunc, rePermGate, rePermProp} {
			if m := re.FindStringSubmatch(line); m != nil {
				name := ""
				if len(m) > 1 {
					name = m[1]
				}
				findings = append(findings, Finding{
					File: path,
					Kind: KindPermissionCheck,
					Name: name,
					Line: lineNum,
				})
			}
		}

		// ── Layout signals (one per file) ────────────────────────────────────────
		if !hasLayoutSignal && reLayoutSignal.MatchString(line) {
			hasLayoutSignal = true
			findings = append(findings, Finding{
				File: path,
				Kind: KindLayoutSignal,
				Name: "layout_signal",
				Line: lineNum,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		// Return partial findings with the read error.
		return findings, err
	}
	return findings, nil
}

// ScanDir recursively scans a directory for TypeScript/React frontend constructs.
// Skips node_modules, .git, dist, build, and vendor directories.
// Returns partial results on individual file errors rather than aborting.
func ScanDir(root string) ([]Finding, error) {
	var all []Finding
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == "node_modules" || base == ".git" || base == "dist" ||
				base == "build" || base == "vendor" || base == ".cache" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isFrontendFile(path) {
			return nil
		}
		found, _ := ScanFile(path)
		all = append(all, found...)
		return nil
	})
	return all, err
}
