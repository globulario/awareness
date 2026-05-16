package scan

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AllowlistEntry is one record in the scan allowlist. A finding is
// suppressed when its PatternID equals the entry's PatternID and its
// File matches the entry's PathPattern.
//
// The allowlist is the only operator-controlled escape hatch on top of
// the scanner. Each entry MUST carry a Reason explaining why the
// otherwise-critical pattern is acceptable at that location.
type AllowlistEntry struct {
	PathPattern string `yaml:"path_pattern"`
	PatternID   string `yaml:"pattern_id"`
	Reason      string `yaml:"reason"`
}

type allowlistFile struct {
	Allowlist []AllowlistEntry `yaml:"allowlist"`
}

// LoadAllowlist reads docs/awareness/knowledge/scan_allowlist.yaml from
// the given docs directory. Missing file, empty docsDir, or parse error
// returns nil — equivalent to "no entries" — so a missing allowlist is
// never silently dangerous (the scanner emits the finding either way;
// the consumer decides whether to allow it).
func LoadAllowlist(docsDir string) []AllowlistEntry {
	if docsDir == "" {
		return nil
	}
	path := filepath.Join(docsDir, "knowledge", "scan_allowlist.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f allowlistFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil
	}
	return f.Allowlist
}

// AllowlistMatch returns the allowlist entry that suppresses this
// finding, or nil. Match logic: PatternID must equal, and the finding's
// File must match the entry's PathPattern (see MatchPathPattern).
func AllowlistMatch(f Finding, allowlist []AllowlistEntry) *AllowlistEntry {
	for i, e := range allowlist {
		if e.PatternID != f.PatternID {
			continue
		}
		if MatchPathPattern(f.File, e.PathPattern) {
			return &allowlist[i]
		}
	}
	return nil
}

// MatchPathPattern checks whether filePath matches a glob pattern.
// Supported shapes:
//   - "**/*.ext"  — matches any file whose basename ends with the
//     extension, regardless of directory.
//   - "glob"      — filepath.Match against the basename and full path.
//
// The implementation is intentionally narrow: a real glob matcher would
// allow surprising matches that an allowlist must never permit. Each
// new pattern shape needs an explicit case.
func MatchPathPattern(filePath, pattern string) bool {
	if strings.HasPrefix(pattern, "**/") {
		rest := pattern[3:]
		base := filepath.Base(filePath)
		matched, err := filepath.Match(rest, base)
		if err == nil && matched {
			return true
		}
		return false
	}
	matched, err := filepath.Match(pattern, filepath.Base(filePath))
	if err == nil && matched {
		return true
	}
	matched, err = filepath.Match(pattern, filePath)
	if err == nil && matched {
		return true
	}
	return false
}
