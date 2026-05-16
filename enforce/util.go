package enforce

import "os"

// readFileText reads a file's text content. Returns "" on error.
func readFileText(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
