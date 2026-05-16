// Package engine demonstrates BPMN engine patterns that satisfy the
// awareness invariants for this project.
package engine

import (
	"context"
	"fmt"
	"time"
)

// Token represents an active flow token in a process instance.
// Identity fields are set at creation and must not change (token.lifecycle.explicit).
type Token struct {
	ID                string
	ProcessInstanceID string
	ElementID         string
	CreatedAt         time.Time
	Variables         map[string]interface{}
}

// GatewayResult is the decision from an exclusive gateway evaluation.
type GatewayResult struct {
	// ActivatedFlowID is the outgoing sequence flow to activate. Empty when
	// an error is returned.
	ActivatedFlowID string
}

// ConditionFunc evaluates a sequence flow condition against a token.
// Must be deterministic — no time.Now(), no random values.
type ConditionFunc func(token *Token, vars map[string]interface{}) bool

// SequenceFlow is an outgoing flow from a gateway.
type SequenceFlow struct {
	ID        string
	IsDefault bool
	Condition ConditionFunc // nil for default flows
}

// EvaluateExclusiveGateway selects exactly one outgoing flow.
// Satisfies gateway.exclusive.single.exit: when no condition matches and no
// default flow exists, returns a clear error — never silently drops the token.
func EvaluateExclusiveGateway(ctx context.Context, token *Token, flows []SequenceFlow) (*GatewayResult, error) {
	if len(flows) == 0 {
		return nil, fmt.Errorf("exclusive gateway %q has no outgoing flows", token.ElementID)
	}
	var defaultFlow *SequenceFlow
	for i := range flows {
		f := &flows[i]
		if f.IsDefault {
			defaultFlow = f
			continue
		}
		if f.Condition != nil && f.Condition(token, token.Variables) {
			return &GatewayResult{ActivatedFlowID: f.ID}, nil
		}
	}
	if defaultFlow != nil {
		return &GatewayResult{ActivatedFlowID: defaultFlow.ID}, nil
	}
	// No match and no default: fail the instance explicitly.
	return nil, fmt.Errorf("exclusive gateway %q: no condition matched and no default flow is defined", token.ElementID)
}
