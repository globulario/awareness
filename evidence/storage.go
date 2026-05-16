package evidence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	runtimeDir       = "/var/lib/globular/awareness/runtime"
	latestSnapshotFile = "latest_snapshot.json"
	errorsFile         = "errors.jsonl"
	factsFile          = "facts.jsonl"
)

// SaveSnapshot writes a NodeRuntimeSnapshot to the local runtime directory.
// latest_snapshot.json is overwritten atomically.
// New facts are appended to facts.jsonl.
// Failed services are appended to errors.jsonl.
func SaveSnapshot(snap *NodeRuntimeSnapshot) error {
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return fmt.Errorf("mkdir runtime dir: %w", err)
	}

	// Write latest_snapshot.json atomically.
	snapPath := filepath.Join(runtimeDir, latestSnapshotFile)
	if err := writeJSONAtomic(snapPath, snap); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}

	// Append facts.
	if len(snap.Facts) > 0 {
		fPath := filepath.Join(runtimeDir, factsFile)
		f, err := os.OpenFile(fPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("open facts file: %w", err)
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		for _, fact := range snap.Facts {
			if err := enc.Encode(fact); err != nil {
				return fmt.Errorf("encode fact: %w", err)
			}
		}
	}

	// Append error-level facts to errors.jsonl.
	ePathOpened := false
	var ef *os.File
	for _, fact := range snap.Facts {
		if fact.Severity != SeverityCritical && fact.Severity != SeverityHigh {
			continue
		}
		if !ePathOpened {
			var err error
			ef, err = os.OpenFile(filepath.Join(runtimeDir, errorsFile),
				os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("open errors file: %w", err)
			}
			defer ef.Close()
			ePathOpened = true
		}
		if err := json.NewEncoder(ef).Encode(fact); err != nil {
			return fmt.Errorf("encode error fact: %w", err)
		}
	}

	return nil
}

// LoadLatestSnapshot reads the most recently saved NodeRuntimeSnapshot from disk.
// Returns nil, nil if no snapshot exists yet.
func LoadLatestSnapshot() (*NodeRuntimeSnapshot, error) {
	data, err := os.ReadFile(filepath.Join(runtimeDir, latestSnapshotFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	var snap NodeRuntimeSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("parse snapshot: %w", err)
	}
	return &snap, nil
}

// LoadRecentFacts reads all facts from facts.jsonl that are newer than cutoff.
func LoadRecentFacts(cutoff time.Time) ([]RuntimeFact, error) {
	data, err := os.ReadFile(filepath.Join(runtimeDir, factsFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read facts: %w", err)
	}

	var out []RuntimeFact
	dec := json.NewDecoder(
		// fake reader wrapping bytes
		&bytesReader{data: data},
	)
	for dec.More() {
		var f RuntimeFact
		if err := dec.Decode(&f); err != nil {
			break
		}
		if f.Timestamp.After(cutoff) {
			out = append(out, f)
		}
	}
	return out, nil
}

// writeJSONAtomic writes v as JSON to path via a temp file rename.
func writeJSONAtomic(path string, v interface{}) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".snap-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// bytesReader wraps []byte as an io.Reader.
type bytesReader struct{ data []byte; pos int }

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, fmt.Errorf("EOF")
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
