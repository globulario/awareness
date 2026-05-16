package livecluster

import (
	"fmt"
	"strings"
)

// FormatLiveSection returns a Markdown section suitable for injecting into agent-context output.
func FormatLiveSection(r *LivePreflightResult) string {
	if r == nil {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Live Cluster Signals\n\n")
	fmt.Fprintf(&sb, "**Verdict:** %s  **Severity:** %s\n\n", strings.ToUpper(r.Verdict), r.Severity)

	if len(r.Blockers) > 0 {
		fmt.Fprintf(&sb, "### Blockers\n")
		for _, b := range r.Blockers {
			fmt.Fprintf(&sb, "- [%s] %s", b.Kind, b.Message)
			if b.Evidence != "" {
				fmt.Fprintf(&sb, " — %s", b.Evidence)
			}
			fmt.Fprintln(&sb)
		}
		fmt.Fprintln(&sb)
	}

	if len(r.Warnings) > 0 {
		fmt.Fprintf(&sb, "### Warnings\n")
		for _, w := range r.Warnings {
			fmt.Fprintf(&sb, "- %s\n", w.Message)
		}
		fmt.Fprintln(&sb)
	}

	if len(r.Confirmations) > 0 {
		fmt.Fprintf(&sb, "### Confirmations\n")
		for _, c := range r.Confirmations {
			fmt.Fprintf(&sb, "- %s\n", c.Message)
		}
		fmt.Fprintln(&sb)
	}

	fmt.Fprintf(&sb, "**Summary:** %s\n", r.Summary)
	return sb.String()
}
