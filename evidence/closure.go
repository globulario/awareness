package evidence

import "time"

// ClosureRecord is a verified, promoted failure pattern for the closure ledger.
// It is only written after: diagnosis confirmed + fix applied + verification passed.
// Closure records are the input for future awareness bundle failure-signature seeds.
type ClosureRecord struct {
	ID               string    `json:"id"`
	FailureSignature string    `json:"failure_signature"`
	Symptoms         []string  `json:"symptoms"` // FactKind values observed
	RootCause        string    `json:"root_cause"`
	Classification   string    `json:"classification"` // Day1Classification value
	Fix              string    `json:"fix"`
	Verification     string    `json:"verification"`
	RegressionTest   string    `json:"required_regression_test"`
	ClosedAt         time.Time `json:"closed_at"`
	ClosedBy         string    `json:"closed_by,omitempty"`
}
