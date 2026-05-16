package livecluster

import (
	"regexp"
	"strings"
	"time"
)

// Patterns for log normalization — replace volatile tokens with placeholders.
var (
	reTimestamp = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?`)
	reUUID      = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	reShortID   = regexp.MustCompile(`\b[a-zA-Z0-9]{2,8}-[a-zA-Z0-9]{1,8}\b`) // e.g. abc-123, action-01
	reIPPort    = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(:\d+)?`)
	rePort      = regexp.MustCompile(`:\d{2,5}\b`)
	reRetry     = regexp.MustCompile(`\b(retry|retries|attempt|count)=\d+`)
	reNumber    = regexp.MustCompile(`\b\d{4,}\b`)
)

// NormalizeLogLine produces a deterministic signature from a raw log line.
// Timestamps, UUIDs, short IDs, IPs, ports, retry counts, and large numbers
// are replaced with placeholders.
func NormalizeLogLine(line string) string {
	line = reTimestamp.ReplaceAllString(line, "<time>")
	line = reUUID.ReplaceAllString(line, "<id>")
	line = reShortID.ReplaceAllString(line, "<id>")
	line = reIPPort.ReplaceAllString(line, "<ip>")
	line = rePort.ReplaceAllString(line, ":<port>")
	line = reRetry.ReplaceAllStringFunc(line, func(m string) string {
		idx := strings.Index(m, "=")
		if idx < 0 {
			return m
		}
		return m[:idx+1] + "<n>"
	})
	line = reNumber.ReplaceAllString(line, "<n>")
	return strings.TrimSpace(line)
}

// LogLine is a raw log entry ready for signature extraction.
type LogLine struct {
	Service   string
	Component string
	NodeID    string
	Message   string
	Severity  string
	Timestamp int64
}

// ExtractRecentErrorSignatures deduplicates raw log lines into error signatures.
// Lines within lookbackHours are included; older entries are ignored.
func ExtractRecentErrorSignatures(lines []LogLine, lookbackHours int) []RecentErrorSignature {
	if lookbackHours == 0 {
		lookbackHours = 24
	}
	cutoff := time.Now().Unix() - int64(lookbackHours)*3600

	type key struct{ service, sig string }
	byKey := map[key]*RecentErrorSignature{}

	for _, l := range lines {
		if l.Timestamp < cutoff {
			continue
		}
		sig := NormalizeLogLine(l.Message)
		if sig == "" {
			continue
		}
		k := key{l.Service, sig}
		if existing, ok := byKey[k]; ok {
			existing.Count++
			if l.Timestamp < existing.FirstSeen {
				existing.FirstSeen = l.Timestamp
			}
			if l.Timestamp > existing.LastSeen {
				existing.LastSeen = l.Timestamp
			}
		} else {
			byKey[k] = &RecentErrorSignature{
				ServiceName: l.Service,
				Component:   l.Component,
				NodeID:      l.NodeID,
				Signature:   sig,
				Severity:    l.Severity,
				Count:       1,
				FirstSeen:   l.Timestamp,
				LastSeen:    l.Timestamp,
				Sample:      l.Message,
			}
		}
	}

	out := make([]RecentErrorSignature, 0, len(byKey))
	for _, e := range byKey {
		out = append(out, *e)
	}
	return out
}
