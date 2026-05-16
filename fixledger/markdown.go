package fixledger

import (
	"bufio"
	"os"
	"strings"
)

// ParsedFixSection is a guardrail section extracted from the guardrails.md file.
type ParsedFixSection struct {
	ID      string    // derived from title
	Title   string
	Status  FixStatus // parsed from STATUS: line
	Summary string
	Tests   []string // lines under "Tests:" section
	Files   []string // files mentioned in fixed_files or other file references
}

// ParseMarkdownFixCases parses a guardrails.md file into structured fix sections.
//
// The parser looks for:
//   - "================================================================================" as section boundaries
//   - "GUARDRAIL N — TITLE" as section headers (may appear between two consecutive dividers)
//   - "STATUS:" prefix for status lines
//   - "Tests:" section header followed by indented lines
//
// The guardrails.md format wraps each section header with dividers above and below:
//
//	================================================================================
//	GUARDRAIL N — TITLE
//	================================================================================
//	STATUS: ...
//
// so the parser uses the GUARDRAIL line, not the dividers, to open new sections.
func ParseMarkdownFixCases(path string) ([]ParsedFixSection, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	const divider = "================================================================================"

	var sections []ParsedFixSection
	var current *ParsedFixSection
	inTests := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		// Divider: if current section has content, do NOT flush yet.
		// The divider may appear as the closing line of a header block,
		// which means the section body follows after it.
		// We only use the GUARDRAIL line to open sections and close the
		// previous one.
		if strings.TrimSpace(line) == divider {
			// If we have a current section and are NOT inside the header
			// block (i.e. the section has a title that was already opened),
			// flush it when we see the NEXT divider that precedes a new
			// GUARDRAIL line. We handle this lazily: when a new GUARDRAIL
			// line is found, we flush the previous current section then.
			inTests = false
			continue
		}

		// Check for GUARDRAIL header line.
		if strings.HasPrefix(line, "GUARDRAIL ") && strings.Contains(line, "—") {
			// Flush the previous section if any.
			if current != nil {
				sections = append(sections, *current)
			}
			// Extract title after "GUARDRAIL N — ".
			parts := strings.SplitN(line, "—", 2)
			title := ""
			if len(parts) == 2 {
				title = strings.TrimSpace(parts[1])
			}
			current = &ParsedFixSection{
				Title:  title,
				ID:     sanitiseTitle(title),
				Status: FixUnknown,
			}
			inTests = false
			continue
		}

		if current == nil {
			continue
		}

		// STATUS: line.
		if strings.HasPrefix(strings.TrimSpace(line), "STATUS:") {
			statusLine := strings.TrimPrefix(strings.TrimSpace(line), "STATUS:")
			statusLine = strings.TrimSpace(statusLine)
			current.Status = parseStatusText(statusLine)
			inTests = false
			continue
		}

		// Tests: section header.
		if strings.TrimSpace(line) == "Tests:" {
			inTests = true
			continue
		}

		// Collect indented lines under Tests: section.
		if inTests {
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
					current.Tests = append(current.Tests, trimmed)
				}
			} else if strings.TrimSpace(line) != "" {
				// Non-indented non-empty line ends the tests section.
				inTests = false
			}
		}

		// Collect summary from non-empty lines that aren't headers.
		if current.Summary == "" && strings.TrimSpace(line) != "" &&
			!strings.HasPrefix(strings.TrimSpace(line), "STATUS:") &&
			!strings.HasPrefix(line, "=") &&
			!strings.HasPrefix(line, "─") &&
			!strings.HasPrefix(line, "GUARDRAIL") &&
			!strings.HasPrefix(line, "-") {
			// Only use the first non-trivial line as summary.
			if len(strings.TrimSpace(line)) > 10 {
				current.Summary = strings.TrimSpace(line)
			}
		}
	}

	// Flush any trailing section.
	if current != nil {
		sections = append(sections, *current)
	}

	return sections, scanner.Err()
}

// parseStatusText converts STATUS line text to a FixStatus.
//
//	"COMPLETE" → FixDone
//	"PARTIAL"  → FixPartial
//	"IN_PROGRESS" or not present → FixInProgress
//	"FUTURE"   → FixProposed
//	default    → FixUnknown
func parseStatusText(text string) FixStatus {
	upper := strings.ToUpper(text)
	switch {
	case strings.HasPrefix(upper, "COMPLETE"):
		return FixDone
	case strings.HasPrefix(upper, "PARTIAL"):
		return FixPartial
	case strings.HasPrefix(upper, "IN_PROGRESS") || strings.HasPrefix(upper, "IN PROGRESS"):
		return FixInProgress
	case strings.HasPrefix(upper, "FUTURE"):
		return FixProposed
	case upper == "" || upper == "UNKNOWN":
		return FixInProgress // default for unset
	default:
		return FixUnknown
	}
}

// sanitiseTitle converts a human-readable guardrail title to a safe ID fragment.
func sanitiseTitle(title string) string {
	title = strings.ToLower(title)
	var buf strings.Builder
	lastWasUnderscore := false
	for _, r := range title {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			buf.WriteRune(r)
			lastWasUnderscore = false
		} else {
			if !lastWasUnderscore {
				buf.WriteRune('_')
				lastWasUnderscore = true
			}
		}
	}
	return strings.Trim(buf.String(), "_")
}
