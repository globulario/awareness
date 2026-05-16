package livecluster

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/globulario/awareness/graph"
	"github.com/google/uuid"
)

const (
	collectorTimeout = 3 * time.Second
	totalTimeout     = 10 * time.Second
	collectorVersion = "1"
)

// CollectClusterSignals runs all registered collectors and merges results into a snapshot.
// Individual collector failures degrade (not block) the result unless RequireLiveData is set.
func CollectClusterSignals(ctx context.Context, req CollectSignalsRequest, collectors []SignalCollector) (*ClusterSignalSnapshot, error) {
	if req.LookbackHours == 0 {
		req.LookbackHours = 24
	}

	snap := &ClusterSignalSnapshot{
		ID:               "SIG-" + uuid.New().String()[:8],
		ClusterID:        req.ClusterID,
		CollectedAt:      time.Now().Unix(),
		CollectorVersion: collectorVersion,
		Status:           "unknown",
	}

	// Run collectors concurrently with per-collector timeout.
	totalCtx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()

	type result struct {
		r   *SignalSourceResult
		err error
	}
	ch := make(chan result, len(collectors))

	var wg sync.WaitGroup
	for _, c := range collectors {
		c := c
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, ccancel := context.WithTimeout(totalCtx, collectorTimeout)
			defer ccancel()
			r, err := c.Collect(cctx, req)
			if err != nil && r == nil {
				// Synthesize an unavailable source so callers see why the
				// collector dropped out instead of a silent disappearance.
				r = &SignalSourceResult{
					Source: SignalSourceStatus{
						Name:        c.Name(),
						Status:      "unavailable",
						Message:     err.Error(),
						CollectedAt: time.Now().Unix(),
					},
				}
			}
			ch <- result{r, err}
		}()
	}
	go func() { wg.Wait(); close(ch) }()

	var unavailable []string
	for res := range ch {
		if res.r == nil {
			continue
		}
		if res.r.Source.Status != "ok" && res.r.Source.Status != "degraded" {
			unavailable = append(unavailable, res.r.Source.Name)
		}
		snap.Sources = append(snap.Sources, res.r.Source)
		snap.Services = append(snap.Services, res.r.Services...)
		snap.Errors = append(snap.Errors, res.r.Errors...)
		snap.Convergence = append(snap.Convergence, res.r.Convergence...)
		snap.Incidents = append(snap.Incidents, res.r.Incidents...)
	}

	snap.Status = deriveSnapshotStatus(snap)
	snap.Summary = buildSnapshotSummary(snap, unavailable)
	return snap, nil
}

// deriveSnapshotStatus rolls up service health and incident severity.
func deriveSnapshotStatus(snap *ClusterSignalSnapshot) string {
	for _, inc := range snap.Incidents {
		if inc.Severity == "critical" && inc.Status == "active" {
			return "critical"
		}
	}
	for _, svc := range snap.Services {
		if svc.Health == "unhealthy" || svc.Health == "unreachable" {
			return "degraded"
		}
	}
	for _, c := range snap.Convergence {
		if c.ConvergenceStatus == "stuck" || c.ConvergenceStatus == "diverged" {
			return "degraded"
		}
	}
	for _, src := range snap.Sources {
		if src.Status == "ok" {
			return "healthy"
		}
	}
	return "unknown"
}

func buildSnapshotSummary(snap *ClusterSignalSnapshot, unavailable []string) string {
	var parts []string
	unhealthy := 0
	for _, s := range snap.Services {
		if s.Health != "healthy" && s.Health != "unknown" {
			unhealthy++
		}
	}
	if unhealthy > 0 {
		parts = append(parts, fmt.Sprintf("%d service(s) degraded/unhealthy", unhealthy))
	}
	stuck := 0
	for _, c := range snap.Convergence {
		if c.ConvergenceStatus == "stuck" || c.ConvergenceStatus == "blocked" {
			stuck++
		}
	}
	if stuck > 0 {
		parts = append(parts, fmt.Sprintf("%d convergence item(s) stuck/blocked", stuck))
	}
	if len(snap.Incidents) > 0 {
		parts = append(parts, fmt.Sprintf("%d active incident(s)", len(snap.Incidents)))
	}
	if len(unavailable) > 0 {
		parts = append(parts, "sources unavailable: "+strings.Join(unavailable, ", "))
	}
	if len(parts) == 0 {
		return "All signals nominal."
	}
	return strings.Join(parts, "; ") + "."
}

// MapFilesToComponents derives component names from file paths using graph node
// lookups, falling back to path-prefix heuristics.
func MapFilesToComponents(ctx context.Context, g *graph.Graph, files []string) []string {
	seen := map[string]bool{}
	var out []string

	if g != nil {
		db := g.DB()
		for _, f := range files {
			rows, err := db.QueryContext(ctx,
				`SELECT DISTINCT n2.name FROM nodes n1
				 JOIN edges e ON e.src=n1.id
				 JOIN nodes n2 ON n2.id=e.dst
				 WHERE (n1.path=? OR n1.path LIKE ?) AND n2.type IN ('service','package','component')
				 LIMIT 10`, f, f+"/%")
			if err == nil {
				for rows.Next() {
					var name string
					if rows.Scan(&name) == nil && !seen[name] {
						out = append(out, name)
						seen[name] = true
					}
				}
				rows.Close()
			}
		}
	}

	for _, f := range files {
		comp := pathToComponent(f)
		if comp != "" && !seen[comp] {
			out = append(out, comp)
			seen[comp] = true
		}
	}
	return out
}

// MapComponentsToServices maps component names to service names.
func MapComponentsToServices(ctx context.Context, g *graph.Graph, components []string) []string {
	seen := map[string]bool{}
	var out []string

	if g != nil {
		db := g.DB()
		for _, comp := range components {
			var svcName string
			err := db.QueryRowContext(ctx,
				`SELECT n2.name FROM nodes n1
				 JOIN edges e ON e.src=n1.id
				 JOIN nodes n2 ON n2.id=e.dst
				 WHERE n1.name=? AND n2.type='service' LIMIT 1`, comp).Scan(&svcName)
			if err == nil && !seen[svcName] {
				out = append(out, svcName)
				seen[svcName] = true
			}
		}
	}

	for _, comp := range components {
		svc := componentToService(comp)
		if svc != "" && !seen[svc] {
			out = append(out, svc)
			seen[svc] = true
		}
	}
	return out
}

// pathToComponent extracts a component name from a Go source file path.
// golang/cluster_controller/foo.go → cluster_controller
func pathToComponent(path string) string {
	path = strings.TrimPrefix(path, "golang/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	return parts[0]
}

// componentToService maps internal Go package names to service names.
func componentToService(component string) string {
	static := map[string]string{
		"cluster_controller": "cluster-controller",
		"node_agent":         "node-agent",
		"workflow":           "workflow-service",
		"repository":         "repository",
		"authentication":     "authentication",
		"rbac":               "rbac",
		"dns":                "dns",
		"ai_memory":          "ai-memory",
		"ai_executor":        "ai-executor",
		"ai_watcher":         "ai-watcher",
		"ai_router":          "ai-router",
		"mcp":                "mcp",
		"cluster_doctor":     "cluster-doctor",
	}
	if svc, ok := static[component]; ok {
		return svc
	}
	return strings.ReplaceAll(component, "_", "-")
}
