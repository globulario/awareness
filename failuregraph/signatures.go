package failuregraph

import (
	"regexp"
	"strings"
)

// Normalization regexes applied in order.
var (
	reTimestamp  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?`)
	reUUID       = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	reMAC        = regexp.MustCompile(`(?:[0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}`)
	reIPPort     = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}:\d{2,5}`)
	reIPv4       = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)
	rePort       = regexp.MustCompile(`:\d{2,5}\b`)
	reHexHash    = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)
	reRetryCount = regexp.MustCompile(`\battempt[s]?\s+\d+\b|\bretry\s+\d+\b|\bafter\s+\d+\s+retries?\b`)
	// build_id= followed by a UUID or empty
	reBuildID    = regexp.MustCompile(`build_id=[0-9a-fA-F-]{0,40}`)
	// version strings like "1.2.3" or "1.2.3-b4"
	reVersion    = regexp.MustCompile(`\b\d+\.\d+\.\d+(?:-[a-z0-9]+)?\b`)
	// DNS hostnames (keep only first segment for readability)
	reDNSName    = regexp.MustCompile(`\b[a-z][a-z0-9-]{2,}\.[a-z][a-z0-9.-]+\b`)
	// run IDs that are all lowercase hex-like
	reRunID      = regexp.MustCompile(`\brun_id=[a-z0-9_-]+`)
	// punctuation normalizer — colons, commas, brackets → spaces
	rePunct      = regexp.MustCompile(`[,;:\[\](){}]`)
	reSpaces     = regexp.MustCompile(`\s{2,}`)
)

// NormalizeErrorSignature converts a raw error string into a canonical,
// deterministic form suitable for storage and comparison.
//
// The goal is to collapse concrete values (IPs, UUIDs, timestamps) into
// typed placeholders so that two errors from the same failure class produce
// the same normalized signature regardless of run-time context.
func NormalizeErrorSignature(raw string) string {
	s := raw

	s = reTimestamp.ReplaceAllString(s, "<time>")
	s = reBuildID.ReplaceAllString(s, "build_id=<build_id>")
	s = reUUID.ReplaceAllString(s, "<id>")
	s = reMAC.ReplaceAllString(s, "<mac>")
	s = reIPPort.ReplaceAllString(s, "<ip>:<port>")
	s = reIPv4.ReplaceAllString(s, "<ip>")
	s = rePort.ReplaceAllString(s, ":<port>")
	s = reHexHash.ReplaceAllString(s, "<hash>")
	s = reRetryCount.ReplaceAllString(s, "attempt <n>")
	s = reRunID.ReplaceAllString(s, "run_id=<id>")
	s = reVersion.ReplaceAllString(s, "<version>")
	s = reDNSName.ReplaceAllString(s, "<dns>")
	s = rePunct.ReplaceAllString(s, " ")
	s = reSpaces.ReplaceAllString(s, " ")
	s = strings.ToLower(strings.TrimSpace(s))

	return s
}

// ContainsKeywords returns true if all keywords (lowercased) appear in the
// lowercased normalized string. Used for keyword-style signature matching.
func ContainsKeywords(normalized string, keywords []string) bool {
	lower := strings.ToLower(normalized)
	for _, kw := range keywords {
		if !strings.Contains(lower, strings.ToLower(kw)) {
			return false
		}
	}
	return true
}
