package contextfreshness

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// FileSnapshot records the identity of a file at a point in time.
type FileSnapshot struct {
	Path        string
	Fingerprint string // "sha256:<hex>" — authoritative over mtime
	SizeBytes   int64
	ModTimeUnix int64
	GitCommit   string // HEAD commit at time of capture (may be empty)
}

// Fingerprint computes the canonical fingerprint for path.
// Uses sha256(file bytes) as the authoritative identity — not mtime alone,
// since some tools preserve timestamps or write files too quickly.
func Fingerprint(path string) (FileSnapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileSnapshot{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return FileSnapshot{}, err
	}

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return FileSnapshot{}, err
	}

	return FileSnapshot{
		Path:        path,
		Fingerprint: fmt.Sprintf("sha256:%x", h.Sum(nil)),
		SizeBytes:   info.Size(),
		ModTimeUnix: info.ModTime().Unix(),
		GitCommit:   gitHead(),
	}, nil
}

// gitHead returns the current git HEAD commit hash, or empty string if unavailable.
func gitHead() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
