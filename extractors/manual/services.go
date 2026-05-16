package manual

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
)

// servicesFile is the top-level structure of services.yaml.
type servicesFile struct {
	Services []yamlService `yaml:"services"`
}

type yamlService struct {
	ID             string           `yaml:"id"`
	Name           string           `yaml:"name"`
	Summary        string           `yaml:"summary"`
	SystemdUnit    string           `yaml:"systemd_unit"`
	ProtoService   string           `yaml:"proto_service"`
	ProtoFile      string           `yaml:"proto_file"`
	Implementation []string         `yaml:"implementation"`
	DependsOn      []yamlDependency `yaml:"depends_on"`
	Authority      *yamlAuthority   `yaml:"authority"`
	Security       *yamlSecurity    `yaml:"security"`
}

type yamlDependency struct {
	Service  string `yaml:"service"`
	Phase    string `yaml:"phase"`
	Required bool   `yaml:"required"`
	Reason   string `yaml:"reason"`
}

type yamlAuthority struct {
	Owns       []string `yaml:"owns"`
	MustNotOwn []string `yaml:"must_not_own"`
}

type yamlSecurity struct {
	TLSRequired                      bool `yaml:"tls_required"`
	AuthzRequired                    bool `yaml:"authz_required"`
	StreamingRPCsRequirePathBeforeWrite bool `yaml:"streaming_rpcs_require_path_before_write"`
}

// LoadServices loads services.yaml into g.
// Missing files are silently skipped.
func LoadServices(ctx context.Context, g *graph.Graph, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("LoadServices: read %s: %w", path, err)
	}

	var f servicesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("LoadServices: parse %s: %w", path, err)
	}

	for _, svc := range f.Services {
		if err := loadService(ctx, g, svc); err != nil {
			return fmt.Errorf("LoadServices %s: %w", svc.ID, err)
		}
	}
	return nil
}

func loadService(ctx context.Context, g *graph.Graph, svc yamlService) error {
	svcID := "service:" + svc.ID
	name := svc.Name
	if name == "" {
		name = svc.ID
	}

	if err := g.AddNode(ctx, graph.Node{
		ID:      svcID,
		Type:    graph.NodeTypeGlobularService,
		Name:    name,
		Summary: svc.Summary,
	}); err != nil {
		return err
	}

	// Systemd unit → systemd_unit node + runs_as edge.
	if svc.SystemdUnit != "" {
		unitID := "systemd_unit:" + svc.SystemdUnit
		if err := g.AddNode(ctx, graph.Node{
			ID:   unitID,
			Type: graph.NodeTypeSystemdUnit,
			Name: svc.SystemdUnit,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  svcID,
			Kind: graph.EdgeRunsAs,
			Dst:  unitID,
		}); err != nil {
			return err
		}
	}

	// Proto service → proto_service node + provides_service edge.
	if svc.ProtoService != "" {
		protoID := "proto_service:" + svc.ProtoService
		if err := g.AddNode(ctx, graph.Node{
			ID:   protoID,
			Type: graph.NodeTypeProtoService,
			Name: svc.ProtoService,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  svcID,
			Kind: graph.EdgeProvidesService,
			Dst:  protoID,
		}); err != nil {
			return err
		}
		// Also maintain backward-compat owns edge.
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  svcID,
			Kind: graph.EdgeOwns,
			Dst:  protoID,
		}); err != nil {
			return err
		}
	}

	// Proto file → source_file node + defines edge from service.
	if svc.ProtoFile != "" {
		fileID := "source_file:" + svc.ProtoFile
		if err := g.AddNode(ctx, graph.Node{
			ID:   fileID,
			Type: graph.NodeTypeSourceFile,
			Name: lastSegment(svc.ProtoFile),
			Path: svc.ProtoFile,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  svcID,
			Kind: graph.EdgeOwns,
			Dst:  fileID,
		}); err != nil {
			return err
		}
	}

	// Implementation files → source_file nodes + implements edges.
	// These are the Go files that realize the proto service contract.
	for _, implFile := range svc.Implementation {
		fileID := "source_file:" + implFile
		if err := g.AddNode(ctx, graph.Node{
			ID:   fileID,
			Type: graph.NodeTypeSourceFile,
			Name: lastSegment(implFile),
			Path: implFile,
		}); err != nil {
			return err
		}
		// source_file → implements → service (enables BFS from changed file)
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  fileID,
			Kind: graph.EdgeImplements,
			Dst:  svcID,
		}); err != nil {
			return err
		}
		// service → owns → source_file (describes the service)
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  svcID,
			Kind: graph.EdgeOwns,
			Dst:  fileID,
		}); err != nil {
			return err
		}
		// Also link to proto service if known (for RPC traversal)
		if svc.ProtoService != "" {
			protoID := "proto_service:" + svc.ProtoService
			if err := g.AddEdge(ctx, graph.Edge{
				Src:  fileID,
				Kind: graph.EdgeImplements,
				Dst:  protoID,
			}); err != nil {
				return err
			}
		}
	}

	// Dependencies → ensure target node exists + depends_on edge.
	for _, dep := range svc.DependsOn {
		dstID := "service:" + dep.Service
		if err := g.AddNode(ctx, graph.Node{
			ID:   dstID,
			Type: graph.NodeTypeGlobularService,
			Name: dep.Service,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:      svcID,
			Kind:     graph.EdgeDependsOn,
			Dst:      dstID,
			Phase:    dep.Phase,
			Required: dep.Required,
		}); err != nil {
			return err
		}
	}

	return nil
}

// lastSegment returns the final path component (filename).
func lastSegment(path string) string {
	if i := strings.LastIndexAny(path, "/\\"); i >= 0 {
		return path[i+1:]
	}
	return path
}
