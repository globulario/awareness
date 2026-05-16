package bundlesync

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ValidateTarSafe streams a tar (optionally gzipped) archive from r and
// rejects any entry that could escape the eventual extraction root or carry
// dangerous content.
//
// Phase A unsafe categories:
//   - absolute_path     — entry name starts with "/"
//   - path_traversal    — entry name contains ".." segments
//   - symlink_escape    — symlink/hardlink target is absolute or contains ".."
//   - device_file       — block/char device entries (TypeBlock, TypeChar)
//   - hardlink_unsafe   — hardlink target violates the symlink rules
//   - unknown_type      — entry type is not in the allowed set
//
// Allowed entry types: regular file, directory, symlink (with safe target),
// hardlink (with safe target). Anything else — fifo, char, block, sparse,
// global header — is treated as unsafe.
//
// The function NEVER extracts, opens new files, or writes anywhere. It only
// reads from r and walks the headers. r is consumed.
//
// Returns:
//   - violations slice (possibly empty) listing every unsafe entry found.
//     A nil/empty slice means the archive is safe.
//   - error if the archive itself is malformed (unreadable / not a tar).
//     Malformed-archive errors are returned in addition to violations so
//     callers can still surface what was found before the parse broke.
func ValidateTarSafe(r io.Reader) ([]TarEntryViolation, error) {
	br := bufio.NewReader(r)

	// Detect gzip by sniffing the magic bytes (1f 8b). We don't trust file
	// extensions; the bundle may arrive without one when streamed.
	magic, _ := br.Peek(2)
	var src io.Reader = br
	if len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("gzip open: %w", err)
		}
		defer gz.Close()
		src = gz
	}

	tr := tar.NewReader(src)
	var violations []TarEntryViolation

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return violations, fmt.Errorf("tar read: %w", err)
		}

		violations = append(violations, classifyEntry(hdr)...)
	}

	return violations, nil
}

// classifyEntry returns zero or more violations for a single tar header.
// Multiple violations on the same entry (e.g. an absolute symlink with a
// traversal target) are reported separately so operators see every issue.
func classifyEntry(hdr *tar.Header) []TarEntryViolation {
	var v []TarEntryViolation

	if isAbsolute(hdr.Name) {
		v = append(v, TarEntryViolation{Name: hdr.Name, Reason: TarReasonAbsolutePath})
	}
	if hasTraversal(hdr.Name) {
		v = append(v, TarEntryViolation{Name: hdr.Name, Reason: TarReasonPathTraversal})
	}

	switch hdr.Typeflag {
	case tar.TypeReg, tar.TypeRegA, tar.TypeDir:
		// Allowed entry types — name checks above are sufficient.

	case tar.TypeSymlink:
		if isUnsafeLinkTarget(hdr.Linkname) {
			v = append(v, TarEntryViolation{Name: hdr.Name, Reason: TarReasonSymlinkEscape})
		}

	case tar.TypeLink:
		if isUnsafeLinkTarget(hdr.Linkname) {
			v = append(v, TarEntryViolation{Name: hdr.Name, Reason: TarReasonHardlinkUnsafe})
		}

	case tar.TypeChar, tar.TypeBlock:
		v = append(v, TarEntryViolation{Name: hdr.Name, Reason: TarReasonDeviceFile})

	default:
		// FIFOs, sparse, global PAX headers, unknown — reject anything we
		// don't have an explicit allow for. Phase A is intentionally strict.
		v = append(v, TarEntryViolation{Name: hdr.Name, Reason: TarReasonUnknownType})
	}

	return v
}

// isAbsolute reports whether name is an absolute path. We check both the POSIX
// form (leading "/") and the Windows form (leading drive letter / backslash)
// even though the bundle is POSIX-targeted, because a malicious archive may
// still embed Windows-style paths for cross-platform exploits.
func isAbsolute(name string) bool {
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, "/") {
		return true
	}
	if strings.HasPrefix(name, "\\") {
		return true
	}
	if len(name) >= 2 && name[1] == ':' {
		// e.g. "C:\windows\system32"
		return true
	}
	return false
}

// hasTraversal reports whether the cleaned name contains any ".." segment.
// We use path.Clean and split on "/" so dot segments anywhere — leading,
// embedded, or as the only segment — are caught uniformly.
//
// Examples flagged: "..", "../etc", "a/../b", "./../x".
// Examples allowed: "a", "a/b", "a/.b", "a..b" (no path-segment traversal).
func hasTraversal(name string) bool {
	// Normalize backslashes too; a tar produced on Windows may use them.
	n := strings.ReplaceAll(name, "\\", "/")
	cleaned := path.Clean(n)
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == ".." {
			return true
		}
	}
	// path.Clean drops trailing slashes; raw inputs like ".." pre-clean
	// already collapsed above. But "../foo" with absolute prefix needs the
	// raw scan too — covered by the cleaned split above.
	return false
}

// ExtractTarSafe streams a tar (optionally gzipped) archive from r and
// extracts every entry into destDir, applying the same safety rules as
// ValidateTarSafe. On the first unsafe entry, extraction stops, destDir is
// removed entirely, and the violations are returned.
//
// destDir must NOT exist when called. The function creates it (along with
// any missing parents) and is responsible for cleanup on any failure path.
//
// Allowed entry types: regular file, directory, symlink (with safe target).
// Hardlinks and any device/fifo/unknown types are rejected — the install
// path needs no hardlinks, and rejecting them keeps the threat model small.
//
// File modes are masked with 0777 to drop suid/sgid/sticky bits — bundles
// must not introduce setuid binaries even by accident.
//
// Symlinks are created literally as declared. The safety check guarantees
// no created symlink points outside destDir.
func ExtractTarSafe(r io.Reader, destDir string) ([]TarEntryViolation, error) {
	if _, err := os.Stat(destDir); err == nil {
		return nil, fmt.Errorf("ExtractTarSafe: destDir %s already exists", destDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("ExtractTarSafe: stat destDir: %w", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("ExtractTarSafe: mkdir destDir: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(destDir)
	}

	br := bufio.NewReader(r)
	magic, _ := br.Peek(2)
	var src io.Reader = br
	if len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("ExtractTarSafe: gzip open: %w", err)
		}
		defer gz.Close()
		src = gz
	}

	tr := tar.NewReader(src)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("ExtractTarSafe: tar read: %w", err)
		}

		// Safety classification first; we never write a byte for an unsafe entry.
		if violations := classifyEntry(hdr); len(violations) > 0 {
			cleanup()
			return violations, fmt.Errorf("%w: %s", ErrTarUnsafe, hdr.Name)
		}

		if err := extractEntry(tr, hdr, destDir); err != nil {
			cleanup()
			return nil, fmt.Errorf("ExtractTarSafe: write %q: %w", hdr.Name, err)
		}
	}

	return nil, nil
}

// extractEntry writes a single safe tar header into destDir. Caller has
// already verified the entry is safe.
func extractEntry(tr *tar.Reader, hdr *tar.Header, destDir string) error {
	// Final path inside the destination root. classifyEntry already ruled
	// out absolute and traversal names, so filepath.Join is safe.
	target := filepath.Join(destDir, hdr.Name)

	// Defense-in-depth: ensure target is still under destDir even after Join.
	rel, err := filepath.Rel(destDir, target)
	if err != nil {
		return err
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return fmt.Errorf("entry escaped destDir: rel=%q", rel)
	}

	// Mask out suid/sgid/sticky bits.
	mode := os.FileMode(hdr.Mode) & 0777

	switch hdr.Typeflag {
	case tar.TypeDir:
		// Bundles are non-secret knowledge artifacts: service users (the
		// globular user running mcp / awareness CLI) must be able to traverse
		// into them to read the manifest and graph. mode|0700 was too
		// restrictive and silently broke freshness wiring — preflight could
		// not load /var/lib/globular/awareness/current/manifest.json because
		// the parent dir was 0700 root:root. Use 0755 to keep traversal open.
		if err := os.MkdirAll(target, mode|0755); err != nil {
			return err
		}

	case tar.TypeReg, tar.TypeRegA:
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}

	case tar.TypeSymlink:
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.Symlink(hdr.Linkname, target); err != nil {
			return err
		}

	default:
		// classifyEntry should have caught everything else, but be paranoid.
		return fmt.Errorf("unsupported entry type: %v", hdr.Typeflag)
	}
	return nil
}

// isUnsafeLinkTarget reports whether a symlink/hardlink target would let an
// extracted bundle reference content outside its own root. A target is unsafe
// when it is absolute OR contains a ".." segment.
//
// We do NOT try to resolve the target against the entry's own directory —
// that requires knowledge of the eventual extraction root and belongs in the
// install path, not the verification path. The conservative rule here keeps
// this primitive context-free.
func isUnsafeLinkTarget(target string) bool {
	if target == "" {
		// Empty target is malformed; treat as unsafe.
		return true
	}
	if isAbsolute(target) {
		return true
	}
	if hasTraversal(target) {
		return true
	}
	return false
}
