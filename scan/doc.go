// Package scan provides AST-aware static analysis for Go source files,
// detecting violations of Globular's architectural hard rules.
//
// Unlike the regex-based scan in mcp/scan_violations_tool.go, this scanner
// uses the standard library go/parser and go/ast to detect violations that
// require semantic understanding (import declarations, call expressions,
// constant declarations, etc.).
//
// Detectable patterns:
//   - loopback_string_literal   — string literal containing 127.0.0.1 or localhost
//   - loopback_in_const_or_var  — const/var assigned loopback string value
//   - loopback_in_grpc_dial     — grpc.Dial/NewClient/DialContext called with loopback
//   - loopback_in_http_call     — http.Get/Post/NewRequest called with loopback URL
//   - os_getenv_runtime_config  — os.Getenv called in non-test file
//   - exec_import_in_controller — "os/exec" imported in cluster_controller path
//   - exec_command_in_high_risk — exec.Command called in high-risk path
//   - retry_without_terminal    — for loop with sleep and no terminal-error break (heuristic)
package scan
