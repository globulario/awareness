package failurelearning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/awareness/failuregraph"
	"gopkg.in/yaml.v3"
)

// failuregraphSeedsSubdir is the subdirectory under docsDir where seed YAML files live.
const failuregraphSeedsSubdir = "failuregraph_seeds"

// WriteOrUpdateSeedYAML writes or updates the YAML seed file for the given
// category at <docsDir>/failuregraph_seeds/<category_name>.yaml.
// It renders the full current state from the failure graph (not just the patch).
// Returns the seed path and content hash (sha256 hex).
func WriteOrUpdateSeedYAML(docsDir string, categoryID string, _ FailureGraphPatch, fg *failuregraph.Store) (string, string, error) {
	ctx := context.Background()

	// Load the full category state from the failure graph.
	exp, err := failuregraph.ExplainCategory(ctx, fg, categoryID)
	if err != nil {
		return "", "", fmt.Errorf("failurelearning: seed: explain category %s: %w", categoryID, err)
	}

	// Build the CategorySeed struct from the current graph state.
	seed := failuregraph.CategorySeed{
		ID:      exp.Category.ID,
		Type:    failuregraph.NodeTypeErrorCategory,
		Name:    exp.Category.Name,
		Severity: exp.Category.Severity,
		Summary: exp.Category.Summary,
	}

	// Signatures — load from the store.
	sigs, err := fg.AllSignatures(ctx)
	if err == nil {
		for _, sig := range sigs {
			if sig.CategoryID == categoryID {
				seed.Signatures = append(seed.Signatures, sig.Signature)
			}
		}
	}

	// Symptoms.
	for _, n := range exp.Symptoms {
		seed.Symptoms = append(seed.Symptoms, failuregraph.SeedItem{ID: n.ID, Summary: n.Summary})
	}

	// Causes.
	for _, n := range exp.LikelyCauses {
		seed.Causes = append(seed.Causes, failuregraph.SeedItem{ID: n.ID, Summary: n.Summary})
	}

	// Resolutions.
	for _, n := range exp.Resolutions {
		seed.Resolutions = append(seed.Resolutions, failuregraph.SeedItem{ID: n.ID, Summary: n.Summary})
	}

	// Wrong fixes.
	for _, n := range exp.WrongFixes {
		seed.WrongFixes = append(seed.WrongFixes, failuregraph.SeedItem{ID: n.ID, Summary: n.Summary})
	}

	// Required tests.
	for _, n := range exp.RequiredTests {
		seed.Tests = append(seed.Tests, failuregraph.SeedItem{ID: n.ID, Summary: n.Summary})
	}

	// Render to YAML.
	content, err := yaml.Marshal(seed)
	if err != nil {
		return "", "", fmt.Errorf("failurelearning: seed: marshal yaml: %w", err)
	}

	// Compute sha256.
	h := sha256.Sum256(content)
	contentHash := hex.EncodeToString(h[:])

	// Determine file path.
	catName := exp.Category.Name
	if catName == "" {
		catName = strings.TrimPrefix(categoryID, "ERRCAT-")
	}
	seedPath := filepath.Join(docsDir, failuregraphSeedsSubdir, catName+".yaml")

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(seedPath), 0o755); err != nil {
		return seedPath, contentHash, fmt.Errorf("failurelearning: seed: mkdir %s: %w", filepath.Dir(seedPath), err)
	}

	// Write the file.
	if err := os.WriteFile(seedPath, content, 0o644); err != nil {
		return seedPath, contentHash, fmt.Errorf("failurelearning: seed: write %s: %w", seedPath, err)
	}

	return seedPath, contentHash, nil
}

// ExportSeeds exports all known failure categories to YAML seed files under docsDir.
// Returns the number of seed files written.
func ExportSeeds(ctx context.Context, docsDir string, fg *failuregraph.Store) (int, error) {
	cats, err := fg.ListCategories(ctx)
	if err != nil {
		return 0, fmt.Errorf("failurelearning: export seeds: list categories: %w", err)
	}

	n := 0
	for _, cat := range cats {
		if _, _, err := WriteOrUpdateSeedYAML(docsDir, cat.ID, FailureGraphPatch{}, fg); err != nil {
			return n, fmt.Errorf("failurelearning: export seeds: %s: %w", cat.ID, err)
		}
		n++
	}
	return n, nil
}

// RebuildFromSeeds reads all YAML files from <docsDir>/failuregraph_seeds/
// and reseeds the failure graph from them. The existing graph data is NOT wiped —
// all operations are upserts so this is safe to call on a populated graph.
func RebuildFromSeeds(ctx context.Context, docsDir string, fg *failuregraph.Store) error {
	dir := filepath.Join(docsDir, failuregraphSeedsSubdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failurelearning: rebuild seeds: read dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("failurelearning: rebuild seeds: read %s: %w", e.Name(), err)
		}
		if err := failuregraph.SeedFromYAML(ctx, fg, data); err != nil {
			return fmt.Errorf("failurelearning: rebuild seeds: seed %s: %w", e.Name(), err)
		}
	}
	return nil
}
