package scan_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/scan"
)

// writeTempGoFile writes Go source to a temporary file and returns its path.
func writeTempGoFile(t *testing.T, name, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// hasPatternID returns true if any finding has the given pattern ID.
func hasPatternID(findings []scan.Finding, patternID string) bool {
	for _, f := range findings {
		if f.PatternID == patternID {
			return true
		}
	}
	return false
}

// TestScanGoFile_LoopbackStringLiteral verifies direct loopback string literal is detected.
func TestScanGoFile_LoopbackStringLiteral(t *testing.T) {
	src := `package main

func dial() string {
	return "127.0.0.1:12000"
}
`
	path := writeTempGoFile(t, "service.go", src)
	findings, err := scan.ScanGoFile(path, nil)
	if err != nil {
		t.Fatalf("ScanGoFile: %v", err)
	}
	if !hasPatternID(findings, "loopback_string_literal") {
		t.Errorf("expected loopback_string_literal, got: %+v", findings)
	}
}

// TestScanGoFile_ConstLoopback verifies const assigned loopback is detected.
func TestScanGoFile_ConstLoopback(t *testing.T) {
	src := `package main

const defaultAddr = "localhost:9000"
`
	path := writeTempGoFile(t, "service.go", src)
	findings, err := scan.ScanGoFile(path, nil)
	if err != nil {
		t.Fatalf("ScanGoFile: %v", err)
	}
	if !hasPatternID(findings, "loopback_in_const_or_var") {
		t.Errorf("expected loopback_in_const_or_var, got: %+v", findings)
	}
}

// TestScanGoFile_GRPCDialLoopback verifies grpc.Dial with loopback is detected.
func TestScanGoFile_GRPCDialLoopback(t *testing.T) {
	src := `package main

import "google.golang.org/grpc"

func connect() {
	_, _ = grpc.Dial("127.0.0.1:12000")
}
`
	path := writeTempGoFile(t, "client.go", src)
	findings, err := scan.ScanGoFile(path, nil)
	if err != nil {
		t.Fatalf("ScanGoFile: %v", err)
	}
	if !hasPatternID(findings, "loopback_in_grpc_dial") {
		t.Errorf("expected loopback_in_grpc_dial, got: %+v", findings)
	}
	// Verify the finding has correct fields.
	for _, f := range findings {
		if f.PatternID == "loopback_in_grpc_dial" {
			if f.KnowledgeID != "hard_rule.no_localhost" {
				t.Errorf("expected KnowledgeID=hard_rule.no_localhost, got %q", f.KnowledgeID)
			}
			if f.Severity != "critical" {
				t.Errorf("expected Severity=critical, got %q", f.Severity)
			}
			if f.Scanner != "go_ast" {
				t.Errorf("expected Scanner=go_ast, got %q", f.Scanner)
			}
		}
	}
}

// TestScanGoFile_GRPCDialNonLoopback verifies grpc.Dial with non-loopback does NOT trigger.
func TestScanGoFile_GRPCDialNonLoopback(t *testing.T) {
	src := `package main

import "google.golang.org/grpc"

func connect() {
	_, _ = grpc.Dial("cluster-controller.globular.internal:12000")
}
`
	path := writeTempGoFile(t, "client.go", src)
	findings, err := scan.ScanGoFile(path, nil)
	if err != nil {
		t.Fatalf("ScanGoFile: %v", err)
	}
	for _, f := range findings {
		if f.PatternID == "loopback_in_grpc_dial" {
			t.Errorf("unexpected loopback_in_grpc_dial for non-loopback address: %+v", f)
		}
	}
}

// TestScanGoFile_OsGetenv verifies os.Getenv in non-test file is detected.
func TestScanGoFile_OsGetenv(t *testing.T) {
	src := `package myservice

import "os"

func getAddr() string {
	return os.Getenv("SERVICE_ADDR")
}
`
	path := writeTempGoFile(t, "config.go", src)
	findings, err := scan.ScanGoFile(path, nil)
	if err != nil {
		t.Fatalf("ScanGoFile: %v", err)
	}
	if !hasPatternID(findings, "os_getenv_runtime_config") {
		t.Errorf("expected os_getenv_runtime_config, got: %+v", findings)
	}
}

// TestScanGoFile_ExecImportInController verifies os/exec import in cluster_controller path.
func TestScanGoFile_ExecImportInController(t *testing.T) {
	src := `package controller

import "os/exec"

func run(cmd string) {
	_ = exec.Command(cmd)
}
`
	dir := t.TempDir()
	// Put file in a cluster_controller-named dir.
	controllerDir := filepath.Join(dir, "cluster_controller")
	if err := os.MkdirAll(controllerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(controllerDir, "handler.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	findings, err := scan.ScanGoFile(path, nil)
	if err != nil {
		t.Fatalf("ScanGoFile: %v", err)
	}
	if !hasPatternID(findings, "exec_import_in_controller") {
		t.Errorf("expected exec_import_in_controller, got: %+v", findings)
	}
}

// TestScanGoFile_TestFileAllowlisted verifies test files do NOT trigger loopback_string_literal.
func TestScanGoFile_TestFileAllowlisted(t *testing.T) {
	src := `package myservice_test

import "testing"

func TestDial(t *testing.T) {
	addr := "127.0.0.1:9000"
	_ = addr
}
`
	path := writeTempGoFile(t, "service_test.go", src)
	findings, err := scan.ScanGoFile(path, nil)
	if err != nil {
		t.Fatalf("ScanGoFile: %v", err)
	}
	// Test files should NOT trigger loopback_string_literal.
	for _, f := range findings {
		if f.PatternID == "loopback_string_literal" {
			t.Errorf("test file should not trigger loopback_string_literal: %+v", f)
		}
	}
}

// TestScanGoFile_AllowlistSuppressedVisible verifies suppressed findings appear in suppressed list.
// The scan package itself doesn't maintain an allowlist (that's the mcp layer), but we
// verify findings can be distinguished by field.
func TestScanGoFile_ScannerFieldIsGoAST(t *testing.T) {
	src := `package main

const addr = "127.0.0.1:9090"
`
	path := writeTempGoFile(t, "service.go", src)
	findings, err := scan.ScanGoFile(path, nil)
	if err != nil {
		t.Fatalf("ScanGoFile: %v", err)
	}
	for _, f := range findings {
		if f.Scanner != "go_ast" {
			t.Errorf("finding %q should have Scanner=go_ast, got %q", f.PatternID, f.Scanner)
		}
	}
}

// TestScanGoDir_MultipleFiles verifies ScanGoDir scans multiple files.
func TestScanGoDir_MultipleFiles(t *testing.T) {
	dir := t.TempDir()

	files := map[string]string{
		"a.go": `package main
const a = "127.0.0.1:1234"
`,
		"b.go": `package main
import "os"
func f() string { return os.Getenv("X") }
`,
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	findings, err := scan.ScanGoDir(dir, nil)
	if err != nil {
		t.Fatalf("ScanGoDir: %v", err)
	}
	if len(findings) < 2 {
		t.Errorf("expected at least 2 findings across 2 files, got %d: %+v", len(findings), findings)
	}
}

// TestScanGoFile_GRPCNewClient verifies grpc.NewClient with loopback is detected.
func TestScanGoFile_GRPCNewClient(t *testing.T) {
	src := `package main

import "google.golang.org/grpc"

func connect() {
	_, _ = grpc.NewClient("localhost:12000")
}
`
	path := writeTempGoFile(t, "client.go", src)
	findings, err := scan.ScanGoFile(path, nil)
	if err != nil {
		t.Fatalf("ScanGoFile: %v", err)
	}
	if !hasPatternID(findings, "loopback_in_grpc_dial") {
		t.Errorf("expected loopback_in_grpc_dial for grpc.NewClient, got: %+v", findings)
	}
}

// TestScanGoFile_ExecCommandHighRisk verifies exec.Command in high-risk path is detected.
func TestScanGoFile_ExecCommandHighRisk(t *testing.T) {
	src := `package controller

import "os/exec"

func doThing() {
	exec.Command("ls", "-la")
}
`
	dir := t.TempDir()
	controllerDir := filepath.Join(dir, "cluster_controller")
	if err := os.MkdirAll(controllerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(controllerDir, "unsafe.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	findings, err := scan.ScanGoFile(path, []string{"cluster_controller"})
	if err != nil {
		t.Fatalf("ScanGoFile: %v", err)
	}
	// Should detect either exec_import_in_controller or exec_command_in_high_risk.
	found := false
	for _, f := range findings {
		if f.PatternID == "exec_import_in_controller" || f.PatternID == "exec_command_in_high_risk" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected exec finding in cluster_controller path, got: %+v", findings)
	}
}
