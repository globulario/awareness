// Package workflows extracts workflow and workflow_step nodes from YAML workflow definitions.
package workflows

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
)

// workflowDef is a minimal representation of a Globular workflow YAML file.
type workflowDef struct {
	ID          string        `yaml:"id"`
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Steps       []workflowStep `yaml:"steps"`
}

type workflowStep struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Actor       string   `yaml:"actor"`
	DependsOn   []string `yaml:"depends_on"`
	RetryPolicy struct {
		MaxAttempts int    `yaml:"max_attempts"`
		Backoff     string `yaml:"backoff"`
	} `yaml:"retry_policy"`
	Verification string `yaml:"verification"`
}

// Extract walks repoRoot for workflow YAML files and extracts workflow/step nodes.
// It recognises YAML files that have a top-level "id" and "steps" field.
func Extract(ctx context.Context, g *graph.Graph, repoRoot string) error {
	return filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		return maybeExtractWorkflow(ctx, g, path, rel)
	})
}

func maybeExtractWorkflow(ctx context.Context, g *graph.Graph, absPath, relPath string) error {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}

	var wf workflowDef
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil // not a workflow file
	}
	if wf.ID == "" || len(wf.Steps) == 0 {
		return nil // not a workflow file
	}

	wfID := "workflow:" + wf.ID
	name := wf.Name
	if name == "" {
		name = wf.ID
	}

	if err := g.AddNode(ctx, graph.Node{
		ID:      wfID,
		Type:    graph.NodeTypeWorkflow,
		Name:    name,
		Path:    relPath,
		Summary: wf.Description,
	}); err != nil {
		return err
	}

	for _, step := range wf.Steps {
		if step.ID == "" {
			continue
		}
		stepID := "workflow_step:" + wf.ID + "." + step.ID
		stepName := step.Name
		if stepName == "" {
			stepName = step.ID
		}

		meta := map[string]any{}
		if step.Actor != "" {
			meta["actor"] = step.Actor
		}
		if step.Verification != "" {
			meta["verification"] = step.Verification
		}
		if step.RetryPolicy.MaxAttempts > 0 {
			retryJSON, _ := json.Marshal(step.RetryPolicy)
			meta["retry_policy"] = string(retryJSON)
		}

		if err := g.AddNode(ctx, graph.Node{
			ID:       stepID,
			Type:     graph.NodeTypeWorkflowStep,
			Name:     stepName,
			Path:     relPath,
			Metadata: meta,
		}); err != nil {
			return err
		}

		// Workflow owns step.
		if err := g.AddEdge(ctx, graph.Edge{Src: wfID, Kind: graph.EdgeOwns, Dst: stepID}); err != nil {
			return err
		}

		// Step depends_on other steps.
		for _, dep := range step.DependsOn {
			depID := "workflow_step:" + wf.ID + "." + dep
			if err := g.AddEdge(ctx, graph.Edge{
				Src:      stepID,
				Kind:     graph.EdgeDependsOn,
				Dst:      depID,
				Required: true,
			}); err != nil {
				return err
			}
		}

		// Actor → service node.
		if step.Actor != "" {
			svcID := "service:" + step.Actor
			_ = g.AddNode(ctx, graph.Node{ID: svcID, Type: graph.NodeTypeGlobularService, Name: step.Actor})
			_ = g.AddEdge(ctx, graph.Edge{Src: stepID, Kind: graph.EdgeRequires, Dst: svcID})
		}
	}

	return nil
}
