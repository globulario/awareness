// Package fsutil holds small, dependency-free filesystem primitives shared
// across Globular services. The intent is to consolidate look-alike helpers
// (fileReadable, fileExists, presence/permission checks) that have caused
// composed-path failures when each subsystem invented its own contract.
package fsutil

import "os"

// ObserveFile reports two independent states for a path:
//
//   - exists: a stat call succeeds (the file is on disk).
//   - readable: an open-for-read call succeeds with the current process's
//     effective UID/GID.
//
// Conflating these into a single bool is the bug shape recorded in
// docs/awareness/composed_path_failures.md under "PKI fileReadable
// conflates missing with not-readable" (2026-05-14). A file owned by
// another user at mode 0400 reports exists=true, readable=false from
// any process that doesn't have read access. The right remediation for
// "missing" is reissuance; the right remediation for "unreadable" is
// ownership/permissions or running as the correct service user. They
// must not share a code path.
//
// Callers should treat (exists, readable) as a 2-bit value:
//
//	(true,  true)  — file present and accessible
//	(true,  false) — file present, current process lacks read access
//	(false, false) — file not on disk
//	(false, true)  — unreachable: cannot read what isn't there
//
// A "(false, true)" return from this function would be a bug; the
// implementation guarantees it cannot happen.
func ObserveFile(path string) (exists, readable bool) {
	if path == "" {
		return false, false
	}
	if _, err := os.Stat(path); err == nil {
		exists = true
	}
	f, err := os.Open(path)
	if err == nil {
		readable = true
		f.Close()
	}
	return
}
