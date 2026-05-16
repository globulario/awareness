package sessionoracle

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ai_memorypb "github.com/globulario/awareness/ai_memorypb"
	"google.golang.org/grpc"
)

// AIMemoryBridge writes compact durable summaries to the AI Memory service.
// The implementation is best-effort — errors are logged but never fatal.
type AIMemoryBridge interface {
	StoreSessionSummary(ctx context.Context, summary AIMemorySessionSummary) error
	StoreDurableDecision(ctx context.Context, decision SessionDecision) error
}

// AIMemorySessionSummary is the compact record pushed to AI Memory on session close.
type AIMemorySessionSummary struct {
	Type             string   `json:"type"`
	Project          string   `json:"project"`
	SessionID        string   `json:"session_id"`
	Title            string   `json:"title"`
	Summary          string   `json:"summary"`
	Durability       string   `json:"durability"`
	RelatedFiles     []string `json:"related_files,omitempty"`
	RelatedIncidents []string `json:"related_incidents,omitempty"`
	UnfinishedCount  int      `json:"unfinished_count"`
}

// gRPCMemoryBridge implements AIMemoryBridge over a live gRPC connection.
type gRPCMemoryBridge struct {
	endpoint string
	dialOpts []grpc.DialOption
}

// NewGRPCMemoryBridge returns a bridge that connects to the AI Memory service at endpoint.
func NewGRPCMemoryBridge(endpoint string, dialOpts ...grpc.DialOption) AIMemoryBridge {
	return &gRPCMemoryBridge{endpoint: endpoint, dialOpts: dialOpts}
}

func (b *gRPCMemoryBridge) StoreSessionSummary(ctx context.Context, summary AIMemorySessionSummary) error {
	conn, err := grpc.DialContext(ctx, b.endpoint, b.dialOpts...) //nolint:staticcheck
	if err != nil {
		return fmt.Errorf("memory bridge: dial: %w", err)
	}
	defer conn.Close()

	payload, _ := json.Marshal(summary)
	client := ai_memorypb.NewAiMemoryServiceClient(conn)
	_, err = client.Store(ctx, &ai_memorypb.StoreRqst{
		Memory: &ai_memorypb.Memory{
			Type:    ai_memorypb.MemoryType_SESSION,
			Title:   summary.Title,
			Content: string(payload),
			Tags:    []string{"session", "oracle", summary.SessionID},
			Metadata: map[string]string{
				"project":    summary.Project,
				"session_id": summary.SessionID,
				"durability": summary.Durability,
			},
		},
	})
	return err
}

func (b *gRPCMemoryBridge) StoreDurableDecision(ctx context.Context, d SessionDecision) error {
	conn, err := grpc.DialContext(ctx, b.endpoint, b.dialOpts...) //nolint:staticcheck
	if err != nil {
		return fmt.Errorf("memory bridge: dial: %w", err)
	}
	defer conn.Close()

	content := fmt.Sprintf("Decision: %s\nRationale: %s", d.Decision, d.Rationale)
	if len(d.AlternativesConsidered) > 0 {
		content += "\nAlternatives: " + strings.Join(d.AlternativesConsidered, "; ")
	}
	if len(d.RelatedIncidents) > 0 {
		content += "\nRelated incidents: " + strings.Join(d.RelatedIncidents, ", ")
	}

	client := ai_memorypb.NewAiMemoryServiceClient(conn)
	_, err = client.Store(ctx, &ai_memorypb.StoreRqst{
		Memory: &ai_memorypb.Memory{
			Type:    ai_memorypb.MemoryType_DECISION,
			Title:   d.Title,
			Content: content,
			Tags:    []string{"decision", "oracle", d.SessionID},
			Metadata: map[string]string{
				"session_id": d.SessionID,
				"confidence": d.Confidence,
			},
		},
	})
	return err
}

// sessionToMemorySummary converts a session + snapshot into the compact AI Memory record.
func sessionToMemorySummary(sess *AgentSession, snap *SessionResumeSnapshot) AIMemorySessionSummary {
	relatedFiles := make([]string, 0, len(snap.FilesTouched))
	seen := map[string]bool{}
	for _, ft := range snap.FilesTouched {
		if (ft.Action == "edit" || ft.Action == "create") && !seen[ft.Path] {
			relatedFiles = append(relatedFiles, ft.Path)
			seen[ft.Path] = true
		}
	}

	var relatedIncidents []string
	incSeen := map[string]bool{}
	for _, d := range snap.Decisions {
		for _, inc := range d.RelatedIncidents {
			if !incSeen[inc] {
				relatedIncidents = append(relatedIncidents, inc)
				incSeen[inc] = true
			}
		}
	}

	unfinCount := 0
	for _, w := range snap.Unfinished {
		if w.Status == "open" || w.Status == "in_progress" {
			unfinCount++
		}
	}

	title := sess.Title
	if title == "" {
		title = sess.Objective
	}

	return AIMemorySessionSummary{
		Type:             "session_resume_summary",
		Project:          "Globular",
		SessionID:        sess.ID,
		Title:            title,
		Summary:          snap.Summary,
		Durability:       "medium",
		RelatedFiles:     relatedFiles,
		RelatedIncidents: relatedIncidents,
		UnfinishedCount:  unfinCount,
	}
}

// noopBridge discards all writes — used in tests and when AI Memory is unavailable.
type noopBridge struct{}

func (noopBridge) StoreSessionSummary(_ context.Context, _ AIMemorySessionSummary) error {
	return nil
}
func (noopBridge) StoreDurableDecision(_ context.Context, _ SessionDecision) error { return nil }

// NoopBridge returns a bridge that discards all writes. Useful for testing.
func NoopBridge() AIMemoryBridge { return noopBridge{} }
