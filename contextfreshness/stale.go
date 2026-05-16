package contextfreshness

import (
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
)

const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// ContextRead records that an agent session consumed a file at a specific fingerprint.
type ContextRead struct {
	ID          string
	SessionID   string
	Path        string
	Fingerprint string
	SizeBytes   int64
	ModTimeUnix int64
	GitCommit   string
	ReadReason  string
	ReadTool    string
	TurnIndex   int
	CreatedAt   int64
}

// StaleContextWarning reports that a previously-read file has changed since
// the agent consumed it. Severity indicates urgency:
//   - critical: agent is about to edit or use for architecture reasoning
//   - warning:  background session-wide freshness scan
//   - info:     informational only, not blocking
type StaleContextWarning struct {
	ID                 string
	SessionID          string
	Path               string
	ReadFingerprint    string
	CurrentFingerprint string // "deleted" when file no longer exists
	ReadTurnIndex      int
	CurrentTurnIndex   int
	Severity           string
	Message            string
	CreatedAt          int64
	AcknowledgedAt     int64 // 0 = not acknowledged
}

// currentFingerprintOrDeleted returns the current sha256 fingerprint of path,
// or ("deleted", true) when the file no longer exists.
func currentFingerprintOrDeleted(path string) (fp string, deleted bool) {
	snap, err := Fingerprint(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "deleted", true
		}
		return "", false
	}
	return snap.Fingerprint, false
}

func buildWarning(sessionID, path string, cr *ContextRead, currentFP string, currentTurnIndex int, severity string) StaleContextWarning {
	msg := fmt.Sprintf(
		"You read %s at turn %d. It was modified since then. Re-read before acting.",
		path, cr.TurnIndex)
	if currentFP == "deleted" {
		msg = fmt.Sprintf(
			"You read %s at turn %d. The file has been deleted since then. Verify before acting.",
			path, cr.TurnIndex)
	}
	return StaleContextWarning{
		ID:                 uuid.New().String(),
		SessionID:          sessionID,
		Path:               path,
		ReadFingerprint:    cr.Fingerprint,
		CurrentFingerprint: currentFP,
		ReadTurnIndex:      cr.TurnIndex,
		CurrentTurnIndex:   currentTurnIndex,
		Severity:           severity,
		Message:            msg,
		CreatedAt:          time.Now().Unix(),
	}
}
