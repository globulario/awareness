package failuregraph

import (
	"context"
	"fmt"
	"time"
)

// LearnFromIncident extracts failure knowledge from a previously recorded incident
// and stores it as graph nodes and edges.
//
// The incident is looked up from the awareness graph's incidents table.
// For each symptom/cause/resolution extracted, a node is created and linked.
// Returns the counts of created nodes and edges.
func LearnFromIncident(ctx context.Context, s *Store, incidentID string, category string, symptoms, causes, resolutions, wrongFixes, tests []string) (int, int, error) {
	var nodeCount, edgeCount int

	// Ensure category node exists
	catID := "ERRCAT-" + sanitizeID(category)
	catNode := FailureNode{
		ID:       catID,
		NodeType: NodeTypeErrorCategory,
		Name:     category,
		Summary:  fmt.Sprintf("Failure category extracted from incident %s", incidentID),
		Severity: "warning",
		Status:   StatusActive,
	}
	if _, err := s.RecordFailureNode(ctx, catNode); err != nil {
		return 0, 0, fmt.Errorf("failuregraph learn: category node: %w", err)
	}
	nodeCount++

	now := time.Now().Unix()

	linkFn := func(items []string, nodeType, edgeType string) error {
		for _, item := range items {
			if item == "" {
				continue
			}
			n := FailureNode{
				ID:        nodePrefix(nodeType) + sanitizeID(item)[:12],
				NodeType:  nodeType,
				Name:      item,
				Summary:   item,
				Status:    StatusActive,
				CreatedAt: now,
			}
			if _, err := s.RecordFailureNode(ctx, n); err != nil {
				return err
			}
			nodeCount++
			edge := FailureEdge{
				FromID:     catID,
				ToID:       n.ID,
				EdgeType:   edgeType,
				Confidence: ConfidenceMedium,
				Evidence:   "incident " + incidentID,
				Source:     "learn_from_incident",
			}
			if _, err := s.RecordFailureEdge(ctx, edge); err != nil {
				return err
			}
			edgeCount++
		}
		return nil
	}

	for _, fn := range []struct {
		items    []string
		nodeType string
		edgeType string
	}{
		{symptoms, NodeTypeSymptom, EdgeObservedAs},
		{causes, NodeTypeRootCause, EdgeCommonlyCausedBy},
		{wrongFixes, NodeTypeWrongFix, EdgeAvoidFix},
		{tests, NodeTypeRegressionTest, EdgeClosureRequires},
	} {
		if err := linkFn(fn.items, fn.nodeType, fn.edgeType); err != nil {
			return nodeCount, edgeCount, err
		}
	}

	// Resolutions link as cause → fixed_by
	for _, res := range resolutions {
		if res == "" {
			continue
		}
		// Pick the first cause as parent or fall back to category
		parentID := catID
		if len(causes) > 0 {
			parentID = nodePrefix(NodeTypeRootCause) + sanitizeID(causes[0])[:12]
		}
		resNode := FailureNode{
			ID:       nodePrefix(NodeTypeResolution) + sanitizeID(res)[:12],
			NodeType: NodeTypeResolution,
			Name:     res,
			Summary:  res,
			Status:   StatusActive,
		}
		if _, err := s.RecordFailureNode(ctx, resNode); err != nil {
			return nodeCount, edgeCount, err
		}
		nodeCount++
		edge := FailureEdge{
			FromID:     parentID,
			ToID:       resNode.ID,
			EdgeType:   EdgeFixedBy,
			Confidence: ConfidenceMedium,
			Evidence:   "incident " + incidentID,
			Source:     "learn_from_incident",
		}
		if _, err := s.RecordFailureEdge(ctx, edge); err != nil {
			return nodeCount, edgeCount, err
		}
		edgeCount++
	}

	return nodeCount, edgeCount, nil
}

// sanitizeID converts a human-readable string into a safe ID fragment.
func sanitizeID(s string) string {
	out := make([]byte, 0, len(s))
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32) // toLower
		case c == ' ', c == '-', c == '.':
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}
