package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/fsutil"
)

// These tests are the canonical home for the (exists, readable) contract.
// The evidence collector and the mcp runtime activation/bootstrap helpers
// have all converged on this primitive — see the composed-path failure log.

func TestObserveFile_PresentAndReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	exists, readable := fsutil.ObserveFile(path)
	if !exists || !readable {
		t.Errorf("got exists=%v readable=%v, want both true", exists, readable)
	}
}

func TestObserveFile_Absent(t *testing.T) {
	exists, readable := fsutil.ObserveFile(filepath.Join(t.TempDir(), "nope"))
	if exists || readable {
		t.Errorf("got exists=%v readable=%v, want both false", exists, readable)
	}
}

// The bug shape: a file present but unreadable by the current process must
// report exists=true, readable=false. Treating them as one bool collapsed
// "needs reissuance" (missing) with "needs ownership fix" (unreadable).
func TestObserveFile_PresentButUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — cannot exercise unreadable-by-current-process branch")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "locked")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod 0o000: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	exists, readable := fsutil.ObserveFile(path)
	if !exists {
		t.Error("file with mode 0o000 must still be reported exists=true")
	}
	if readable {
		t.Error("file with mode 0o000 must not be readable by the current process")
	}
}

func TestObserveFile_EmptyPath(t *testing.T) {
	exists, readable := fsutil.ObserveFile("")
	if exists || readable {
		t.Errorf("empty path must report (false, false); got exists=%v readable=%v", exists, readable)
	}
}

// (false, true) is unreachable by construction — there is no way for a
// path that doesn't stat to open. This test pins the property as a
// dimensional check across the call shape.
func TestObserveFile_NoFalsePathOpenWithoutStat(t *testing.T) {
	for _, p := range []string{
		filepath.Join(t.TempDir(), "absent"),
		"/this/path/does/not/exist/xyz",
		"",
	} {
		exists, readable := fsutil.ObserveFile(p)
		if !exists && readable {
			t.Errorf("ObserveFile(%q) returned (false, true) — invariant violation", p)
		}
	}
}
