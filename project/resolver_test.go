package project_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/globulario/awareness/project"
)

func testdataDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is .../awareness/project/resolver_test.go
	// testdata is at .../awareness/testdata
	return filepath.Join(filepath.Dir(filepath.Dir(file)), "testdata")
}

func TestResolveProfile_CadenceLike(t *testing.T) {
	td := testdataDir(t)
	root := filepath.Join(td, "cadence-like")

	prof, err := project.ResolveProfile(root, project.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prof.Name != "cadence" {
		t.Errorf("Name = %q, want %q", prof.Name, "cadence")
	}
	if string(prof.Kind) != "application" {
		t.Errorf("Kind = %q, want %q", prof.Kind, "application")
	}
	if prof.IsRuntimeEnabled() {
		t.Error("cadence-like should have runtime disabled")
	}
	if prof.AdapterName() != "null" {
		t.Errorf("AdapterName = %q, want %q", prof.AdapterName(), "null")
	}
	if prof.Root != root {
		t.Errorf("Root = %q, want %q", prof.Root, root)
	}
}

func TestResolveProfile_GlobularLike(t *testing.T) {
	td := testdataDir(t)
	root := filepath.Join(td, "globular-like")

	prof, err := project.ResolveProfile(root, project.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prof.Name != "globular-services" {
		t.Errorf("Name = %q, want %q", prof.Name, "globular-services")
	}
	if string(prof.Kind) != "distributed-platform" {
		t.Errorf("Kind = %q, want %q", prof.Kind, "distributed-platform")
	}
	if !prof.IsRuntimeEnabled() {
		t.Error("globular-like should have runtime enabled")
	}
	if prof.AdapterName() != "globular" {
		t.Errorf("AdapterName = %q, want %q", prof.AdapterName(), "globular")
	}
}

func TestResolveProfile_ExplicitRoot(t *testing.T) {
	td := testdataDir(t)
	root := filepath.Join(td, "cadence-like")

	// explicit root overrides cwd
	prof, err := project.ResolveProfile("/tmp", project.ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prof.Name != "cadence" {
		t.Errorf("Name = %q, want cadence", prof.Name)
	}
}

func TestResolveProfile_UpwardDiscovery(t *testing.T) {
	td := testdataDir(t)
	root := filepath.Join(td, "cadence-like")
	subdir := filepath.Join(root, "internal", "model")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	prof, err := project.ResolveProfile(subdir, project.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error from subdir: %v", err)
	}
	if prof.Name != "cadence" {
		t.Errorf("Name = %q, want cadence", prof.Name)
	}
}

func TestResolveProfile_StopsAtGitRoot_NoBoundary(t *testing.T) {
	// /tmp has no .git and no .awareness.yaml — should reach filesystem root.
	_, err := project.ResolveProfile("/tmp", project.ResolveOptions{})
	if err == nil {
		t.Fatal("expected error when no config found")
	}
}

func TestResolveProfile_MissingExplicitRoot(t *testing.T) {
	_, err := project.ResolveProfile("/tmp", project.ResolveOptions{
		ProjectRoot: "/tmp/nonexistent-awareness-root-xyz",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent project root")
	}
}

func TestResolveProfile_MissingConfigFile(t *testing.T) {
	tmp := t.TempDir()
	_, err := project.ResolveProfile(tmp, project.ResolveOptions{ProjectRoot: tmp})
	if err == nil {
		t.Fatal("expected error when .awareness.yaml is absent")
	}
}

func TestResolveProfile_MalformedYAML(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, ".awareness.yaml"), []byte(":::bad yaml:::\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := project.ResolveProfile(tmp, project.ResolveOptions{ProjectRoot: tmp})
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestResolveProfile_FreshnessTTL(t *testing.T) {
	td := testdataDir(t)
	root := filepath.Join(td, "globular-like")
	prof, err := project.ResolveProfile(root, project.ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if prof.Graph.FreshnessTTL == 0 {
		t.Error("expected non-zero FreshnessTTL")
	}
}

func TestResolveProfile_RootIsAbsolute(t *testing.T) {
	td := testdataDir(t)
	prof, err := project.ResolveProfile(filepath.Join(td, "cadence-like"), project.ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(prof.Root) {
		t.Errorf("Root is not absolute: %s", prof.Root)
	}
	if !filepath.IsAbs(prof.ConfigPath) {
		t.Errorf("ConfigPath is not absolute: %s", prof.ConfigPath)
	}
}
