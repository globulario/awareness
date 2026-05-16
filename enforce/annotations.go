package enforce

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// ValidateAnnotations walks srcDir for non-test .go files and validates every
// //globular: annotation. Returns one Finding per malformed annotation.
//
// Rules enforced:
//   - state_transition must contain " -> " (from -> to) and both sides non-empty
//   - hash_schema / expects_hash_schema value must be a non-empty identifier (no spaces)
//   - tested_by value must start with "Test", "Benchmark", or "Example"
//   - enforces / protects value must be non-empty and contain no whitespace
//   - All directives must have a non-empty value
func ValidateAnnotations(srcDir string) []Finding {
	var findings []Finding

	_ = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			rel = path
		}
		findings = append(findings, validateFileAnnotations(rel, path)...)
		return nil
	})

	return findings
}

func validateFileAnnotations(relPath, absPath string) []Finding {
	src, err := readFileText(absPath)
	if err != nil {
		return nil
	}

	var findings []Finding
	lines := strings.Split(src, "\n")
	for lineNo, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//globular:") {
			continue
		}
		// Strip the leading //globular: prefix.
		rest := strings.TrimPrefix(trimmed, "//globular:")
		parts := strings.SplitN(rest, " ", 2)
		directive := parts[0]
		value := ""
		if len(parts) == 2 {
			value = strings.TrimSpace(parts[1])
		}

		loc := findingLocation(relPath, lineNo+1)
		if value == "" {
			findings = append(findings, Finding{
				Code:     CodeAnnotationMissingValue,
				Severity: SeverityError,
				File:     relPath,
				Message:  loc + ": //globular:" + directive + " requires a non-empty value",
			})
			continue
		}

		switch directive {
		case "state_transition":
			findings = append(findings, validateStateTransition(relPath, loc, value)...)

		case "hash_schema", "expects_hash_schema":
			if strings.ContainsAny(value, " \t") {
				findings = append(findings, Finding{
					Code:     "ANNOTATION_BAD_IDENTIFIER",
					Severity: SeverityError,
					File:     relPath,
					Message:  loc + ": //globular:" + directive + " value must be a single identifier, got: " + value,
				})
			}

		case "enforces", "protects":
			if strings.ContainsAny(value, " \t") {
				findings = append(findings, Finding{
					Code:     "ANNOTATION_BAD_IDENTIFIER",
					Severity: SeverityError,
					File:     relPath,
					Message:  loc + ": //globular:" + directive + " value must be a dot-separated identifier, got: " + value,
				})
			}

		case "tested_by":
			if !strings.HasPrefix(value, "Test") &&
				!strings.HasPrefix(value, "Benchmark") &&
				!strings.HasPrefix(value, "Example") {
				findings = append(findings, Finding{
					Code:     "ANNOTATION_BAD_TEST_NAME",
					Severity: SeverityError,
					File:     relPath,
					Message:  loc + ": //globular:tested_by value must start with Test, Benchmark, or Example — got: " + value,
				})
			}

		case "service", "reads", "writes", "controls", "forbids", "phase", "risk":
			// Non-empty value is sufficient — already checked above.
		default:
			findings = append(findings, Finding{
				Code:     CodeAnnotationUnknownDirective,
				Severity: SeverityWarning,
				File:     relPath,
				Message:  loc + ": unknown //globular directive: " + directive,
			})
		}
	}

	return findings
}

func validateStateTransition(relPath, loc, value string) []Finding {
	// Normalize: "A->B" → "A -> B".
	norm := strings.ReplaceAll(value, "->", " -> ")
	norm = strings.Join(strings.Fields(norm), " ")

	idx := strings.Index(norm, " -> ")
	if idx < 0 {
		return []Finding{{
			Code:     CodeAnnotationBadStateTrans,
			Severity: SeverityError,
			File:     relPath,
			Message:  loc + ": //globular:state_transition must have format 'FROM -> TO', got: " + value,
		}}
	}
	from := strings.TrimSpace(norm[:idx])
	to := strings.TrimSpace(norm[idx+4:])
	if from == "" || to == "" {
		return []Finding{{
			Code:     CodeAnnotationBadStateTrans,
			Severity: SeverityError,
			File:     relPath,
			Message:  loc + ": //globular:state_transition FROM and TO must both be non-empty, got: " + value,
		}}
	}
	return nil
}

func findingLocation(relPath string, lineNo int) string {
	if lineNo > 0 {
		return relPath + ":" + itoa(lineNo)
	}
	return relPath
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := make([]byte, 0, 10)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	// reverse
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}
