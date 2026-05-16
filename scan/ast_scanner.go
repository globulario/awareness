package scan

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Finding represents a single AST-detected violation.
type Finding struct {
	File            string `json:"file"`
	Line            int    `json:"line"`
	Column          int    `json:"column"`
	Snippet         string `json:"snippet"`
	PatternID       string `json:"pattern_id"`
	KnowledgeID     string `json:"knowledge_id"`
	Severity        string `json:"severity"`
	WhyDangerous    string `json:"why_dangerous"`
	SafeAlternative string `json:"safe_alternative"`
	Confidence      string `json:"confidence"`
	Scanner         string `json:"scanner"` // "go_ast" or "regex"
	Suppressed      bool   `json:"suppressed,omitempty"`
	SuppressReason  string `json:"suppress_reason,omitempty"`
}

// ScanGoFile parses a single Go file and returns AST findings.
// highRiskPaths is a list of path substrings that enable additional checks.
func ScanGoFile(path string, highRiskPaths []string) ([]Finding, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.AllErrors)
	if err != nil {
		// Partial parse — return what we have (parser returns partial AST).
		if f == nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}

	lines := strings.Split(string(src), "\n")
	isTest := strings.HasSuffix(path, "_test.go")
	isHighRisk := isHighRiskPath(path, highRiskPaths)

	var findings []Finding

	// Walk the AST.
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		switch node := n.(type) {
		case *ast.GenDecl:
			findings = append(findings, checkGenDecl(node, fset, path, lines, isTest, isHighRisk)...)
		case *ast.CallExpr:
			findings = append(findings, checkCallExpr(node, fset, path, lines, isTest, isHighRisk)...)
		}
		return true
	})

	// Check imports at the file level.
	findings = append(findings, checkImports(f, fset, path, lines, isHighRisk)...)

	// Check all string literals for loopback (belt-and-suspenders for non-const/var literals).
	findings = append(findings, checkStringLiterals(f, fset, path, lines, isTest)...)

	// Heuristic: retry without terminal (for loop with sleep, no break on error).
	if !isTest {
		findings = append(findings, checkBlindRetryLoops(f, fset, path, lines)...)
	}

	// Deduplicate findings by (file, line, patternID).
	findings = deduplicateFindings(findings)

	return findings, nil
}

// deduplicateFindings removes duplicate findings with the same file+line+patternID.
func deduplicateFindings(findings []Finding) []Finding {
	type key struct {
		file      string
		line      int
		patternID string
	}
	seen := make(map[key]bool)
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		k := key{f.File, f.Line, f.PatternID}
		if !seen[k] {
			seen[k] = true
			out = append(out, f)
		}
	}
	return out
}

// ScanGoDir recursively scans all .go files in a directory.
func ScanGoDir(dir string, highRiskPaths []string) ([]Finding, error) {
	var all []Finding
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
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
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		found, fileErr := ScanGoFile(path, highRiskPaths)
		if fileErr != nil {
			// Skip unparseable files — don't abort the whole scan.
			return nil
		}
		all = append(all, found...)
		return nil
	})
	return all, err
}

// --- Check functions ---

// checkGenDecl inspects const/var/import declarations.
func checkGenDecl(decl *ast.GenDecl, fset *token.FileSet, path string, lines []string, isTest, isHighRisk bool) []Finding {
	var findings []Finding
	switch decl.Tok {
	case token.CONST, token.VAR:
		for _, spec := range decl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, val := range vs.Values {
				lit, ok := val.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v := unquoteString(lit.Value)
				if isLoopback(v) {
					pos := fset.Position(lit.Pos())
					snippet := safeGetLine(lines, pos.Line)
					findings = append(findings, Finding{
						File:            path,
						Line:            pos.Line,
						Column:          pos.Column,
						Snippet:         strings.TrimSpace(snippet),
						PatternID:       "loopback_in_const_or_var",
						KnowledgeID:     "hard_rule.no_localhost",
						Severity:        "critical",
						WhyDangerous:    "Loopback addresses in const/var declarations break multi-node cluster semantics.",
						SafeAlternative: "Resolve address from etcd service discovery at runtime.",
						Confidence:      "high",
						Scanner:         "go_ast",
					})
				}
			}
		}
	}
	return findings
}

// checkCallExpr inspects function call expressions.
func checkCallExpr(call *ast.CallExpr, fset *token.FileSet, path string, lines []string, isTest, isHighRisk bool) []Finding {
	var findings []Finding
	pos := fset.Position(call.Pos())
	snippet := strings.TrimSpace(safeGetLine(lines, pos.Line))

	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return nil
	}

	pkgIdent, isPkgIdent := sel.X.(*ast.Ident)
	if !isPkgIdent {
		return nil
	}
	pkgName := pkgIdent.Name
	funcName := sel.Sel.Name

	// grpc.Dial / grpc.NewClient / grpc.DialContext — first string arg is loopback.
	if pkgName == "grpc" && (funcName == "Dial" || funcName == "NewClient" || funcName == "DialContext") {
		arg := firstStringArg(call)
		if arg != "" && isLoopback(arg) {
			findings = append(findings, Finding{
				File:            path,
				Line:            pos.Line,
				Column:          pos.Column,
				Snippet:         snippet,
				PatternID:       "loopback_in_grpc_dial",
				KnowledgeID:     "hard_rule.no_localhost",
				Severity:        "critical",
				WhyDangerous:    "gRPC Dial with loopback address breaks multi-node cluster semantics.",
				SafeAlternative: "Resolve address from etcd service discovery.",
				Confidence:      "high",
				Scanner:         "go_ast",
			})
		}
		return findings
	}

	// os.Getenv — non-test, non-main, non-dev file.
	if pkgName == "os" && funcName == "Getenv" && !isTest {
		if !isExcludedFromEnvCheck(path) {
			findings = append(findings, Finding{
				File:            path,
				Line:            pos.Line,
				Column:          pos.Column,
				Snippet:         snippet,
				PatternID:       "os_getenv_runtime_config",
				KnowledgeID:     "hard_rule.no_env_vars",
				Severity:        "warning",
				WhyDangerous:    "os.Getenv is forbidden for service configuration; etcd is the only config authority.",
				SafeAlternative: "Use etcd-backed config; os.Getenv is forbidden for service configuration.",
				Confidence:      "high",
				Scanner:         "go_ast",
			})
		}
		return findings
	}

	// exec.Command in high-risk files.
	if pkgName == "exec" && funcName == "Command" && isHighRisk {
		findings = append(findings, Finding{
			File:            path,
			Line:            pos.Line,
			Column:          pos.Column,
			Snippet:         snippet,
			PatternID:       "exec_command_in_high_risk",
			KnowledgeID:     "hard_rule.no_exec_in_controller",
			Severity:        "critical",
			WhyDangerous:    "exec.Command in cluster_controller or workflow violates the security boundary.",
			SafeAlternative: "Dispatch to node_agent via workflow step.",
			Confidence:      "high",
			Scanner:         "go_ast",
		})
		return findings
	}

	// http.Get / http.Post / http.NewRequest — first string arg is loopback.
	if pkgName == "http" && (funcName == "Get" || funcName == "Post" || funcName == "NewRequest") {
		arg := firstStringArg(call)
		if arg != "" && (strings.Contains(strings.ToLower(arg), "127.0.0.1") || strings.Contains(strings.ToLower(arg), "localhost")) {
			findings = append(findings, Finding{
				File:            path,
				Line:            pos.Line,
				Column:          pos.Column,
				Snippet:         snippet,
				PatternID:       "loopback_in_http_call",
				KnowledgeID:     "hard_rule.no_localhost",
				Severity:        "warning",
				WhyDangerous:    "HTTP call with loopback URL may break in multi-node deployments.",
				SafeAlternative: "Resolve address from etcd; use FQDN for intra-cluster HTTP.",
				Confidence:      "medium",
				Scanner:         "go_ast",
			})
		}
		return findings
	}

	return findings
}

// checkImports scans import declarations.
func checkImports(f *ast.File, fset *token.FileSet, path string, lines []string, isHighRisk bool) []Finding {
	var findings []Finding
	for _, imp := range f.Imports {
		if imp.Path == nil {
			continue
		}
		importPath := unquoteString(imp.Path.Value)
		pos := fset.Position(imp.Pos())
		snippet := strings.TrimSpace(safeGetLine(lines, pos.Line))

		// exec_import_in_controller — import of "os/exec" in cluster_controller path.
		if importPath == "os/exec" && strings.Contains(path, "cluster_controller") {
			findings = append(findings, Finding{
				File:            path,
				Line:            pos.Line,
				Column:          pos.Column,
				Snippet:         snippet,
				PatternID:       "exec_import_in_controller",
				KnowledgeID:     "hard_rule.no_exec_in_controller",
				Severity:        "critical",
				WhyDangerous:    "cluster_controller must never use os/exec; only node_agent may spawn processes.",
				SafeAlternative: "Move execution logic to a workflow step dispatched to node_agent.",
				Confidence:      "high",
				Scanner:         "go_ast",
			})
		}
	}
	return findings
}

// checkBlindRetryLoops is a heuristic check for for loops that sleep but lack terminal break.
func checkBlindRetryLoops(f *ast.File, fset *token.FileSet, path string, lines []string) []Finding {
	var findings []Finding
	ast.Inspect(f, func(n ast.Node) bool {
		forStmt, ok := n.(*ast.ForStmt)
		if !ok {
			return true
		}
		if forStmt.Body == nil {
			return true
		}
		hasSleep := false
		hasTerminalBreak := false

		ast.Inspect(forStmt.Body, func(inner ast.Node) bool {
			switch node := inner.(type) {
			case *ast.CallExpr:
				// Detect time.Sleep or time.After.
				if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
					if ident, ok := sel.X.(*ast.Ident); ok {
						if ident.Name == "time" && (sel.Sel.Name == "Sleep" || sel.Sel.Name == "After") {
							hasSleep = true
						}
					}
				}
			case *ast.BranchStmt:
				if node.Tok == token.BREAK {
					hasTerminalBreak = true
				}
			case *ast.ReturnStmt:
				hasTerminalBreak = true
			}
			return true
		})

		if hasSleep && !hasTerminalBreak {
			pos := fset.Position(forStmt.Pos())
			snippet := strings.TrimSpace(safeGetLine(lines, pos.Line))
			findings = append(findings, Finding{
				File:            path,
				Line:            pos.Line,
				Column:          pos.Column,
				Snippet:         snippet,
				PatternID:       "retry_without_terminal",
				KnowledgeID:     "pattern.blind_retry_loop",
				Severity:        "warning",
				WhyDangerous:    "Retry loop with sleep but no terminal break may spin forever on deterministic failures.",
				SafeAlternative: "Use FailureClass classification and explicit break/return on terminal errors.",
				Confidence:      "low",
				Scanner:         "go_ast",
			})
		}
		return true
	})
	return findings
}

// --- String literal scanning for loopback at file level ---
// We also scan all string literals for raw loopback addresses.
// This is a belt-and-suspenders check beyond checkGenDecl.
// We run it as a separate pass during ScanGoFile.

func checkStringLiterals(f *ast.File, fset *token.FileSet, path string, lines []string, isTest bool) []Finding {
	var findings []Finding
	if isTest {
		return nil
	}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v := unquoteString(lit.Value)
		if !isLoopback(v) {
			return true
		}
		// Skip if this is inside a const/var (already caught by checkGenDecl).
		pos := fset.Position(lit.Pos())
		snippet := strings.TrimSpace(safeGetLine(lines, pos.Line))
		findings = append(findings, Finding{
			File:            path,
			Line:            pos.Line,
			Column:          pos.Column,
			Snippet:         snippet,
			PatternID:       "loopback_string_literal",
			KnowledgeID:     "hard_rule.no_localhost",
			Severity:        "critical",
			WhyDangerous:    "String literal with loopback address breaks multi-node cluster semantics.",
			SafeAlternative: "Resolve address from etcd service discovery at runtime.",
			Confidence:      "high",
			Scanner:         "go_ast",
		})
		return true
	})
	return findings
}

// --- helpers ---

func isLoopback(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "127.0.0.1") || strings.Contains(lower, "localhost")
}

func unquoteString(s string) string {
	if len(s) >= 2 {
		if s[0] == '"' && s[len(s)-1] == '"' {
			return s[1 : len(s)-1]
		}
		if s[0] == '`' && s[len(s)-1] == '`' {
			return s[1 : len(s)-1]
		}
		if s[0] == '\'' && s[len(s)-1] == '\'' {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func safeGetLine(lines []string, lineNum int) string {
	if lineNum <= 0 || lineNum > len(lines) {
		return ""
	}
	return lines[lineNum-1]
}

func isHighRiskPath(path string, highRiskPaths []string) bool {
	for _, p := range highRiskPaths {
		if strings.Contains(path, p) {
			return true
		}
	}
	// Default high-risk: cluster_controller or workflow.
	return strings.Contains(path, "cluster_controller") || strings.Contains(path, "workflow")
}

func isExcludedFromEnvCheck(path string) bool {
	base := filepath.Base(path)
	dir := filepath.Dir(path)
	// Allow: CLI tools, code generation scripts, main packages with "main" in path.
	excludedDirs := []string{"globularcli", "generateCode", "build-all", "cmd/", "/main/"}
	for _, ex := range excludedDirs {
		if strings.Contains(dir, ex) || strings.Contains(base, ex) {
			return true
		}
	}
	// Allow main.go files (entry points).
	if base == "main.go" {
		return true
	}
	return false
}

// firstStringArg returns the value of the first string literal argument of a call.
func firstStringArg(call *ast.CallExpr) string {
	if len(call.Args) == 0 {
		return ""
	}
	// For DialContext, the first arg is context — skip it and look at second.
	// Detect by checking if first arg is an identifier (context.Background(), ctx, etc.).
	startIdx := 0
	if _, isIdent := call.Args[0].(*ast.Ident); isIdent {
		startIdx = 1
	}
	if sel, ok := call.Args[0].(*ast.SelectorExpr); ok {
		// e.g. context.Background() — skip.
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "context" {
			startIdx = 1
		}
	}
	for i := startIdx; i < len(call.Args); i++ {
		lit, ok := call.Args[i].(*ast.BasicLit)
		if ok && lit.Kind == token.STRING {
			return unquoteString(lit.Value)
		}
	}
	return ""
}
