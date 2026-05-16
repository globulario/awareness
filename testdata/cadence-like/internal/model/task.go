// Package model defines the core domain types for the Cadence task engine.
package model

import (
	"errors"
	"time"
)

// TaskState is the lifecycle state of a Task.
type TaskState string

const (
	TaskStatePending   TaskState = "pending"
	TaskStateRunning   TaskState = "running"
	TaskStateCompleted TaskState = "completed"
	TaskStateFailed    TaskState = "failed"
	TaskStateCancelled TaskState = "cancelled"
)

// validTransitions maps each state to the set of states it may transition into.
var validTransitions = map[TaskState][]TaskState{
	TaskStatePending:   {TaskStateRunning, TaskStateCancelled},
	TaskStateRunning:   {TaskStateCompleted, TaskStateFailed, TaskStateCancelled},
	TaskStateCompleted: {},
	TaskStateFailed:    {},
	TaskStateCancelled: {},
}

// Task is the unit of work scheduled and executed by the Cadence engine.
//
// Identity fields (ID, WorkflowID, ActivityType) are set at creation and
// must not be modified afterward — see invariant model.immutability.
type Task struct {
	// Identity (immutable after creation).
	ID           string
	WorkflowID   string
	ActivityType string

	// Mutable state — always change through TransitionTo.
	State     TaskState
	CreatedAt time.Time
	UpdatedAt time.Time

	// Scheduling constraints.
	ScheduleToStartTimeout time.Duration
	ScheduleToCloseTimeout time.Duration

	// Result (set only in terminal states).
	Result []byte
	Error  string
}

// TransitionTo moves the task to newState, enforcing the state machine.
// Returns an error when the transition is invalid.
func (t *Task) TransitionTo(newState TaskState) error {
	allowed, ok := validTransitions[t.State]
	if !ok {
		return errors.New("task is in unknown state")
	}
	for _, s := range allowed {
		if s == newState {
			t.State = newState
			t.UpdatedAt = time.Now()
			return nil
		}
	}
	return errors.New("invalid state transition: " + string(t.State) + " → " + string(newState))
}
