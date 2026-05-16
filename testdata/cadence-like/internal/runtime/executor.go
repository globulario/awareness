// Package runtime implements the task executor for the Cadence engine.
package runtime

import (
	"context"
	"errors"
	"time"

	"cadence-like/internal/model"
)

// ResourceLimits defines the maximum resources an executor may allocate.
type ResourceLimits struct {
	MaxCPUMillicores int
	MaxMemoryMB      int
}

// Executor schedules and runs tasks within resource bounds.
//
// All task acceptance decisions are made before execution begins —
// see invariant executor.resource.bounds.
type Executor struct {
	limits ResourceLimits
}

// NewExecutor creates an Executor with the given resource limits.
func NewExecutor(limits ResourceLimits) *Executor {
	return &Executor{limits: limits}
}

// TaskRequest describes a task submission.
type TaskRequest struct {
	Task            *model.Task
	CPUMillicores   int
	MemoryMB        int
	ActivityHandler func(ctx context.Context, task *model.Task) ([]byte, error)
}

// Submit validates resource bounds and schedules the task for execution.
// Returns an error when bounds are exceeded — callers must not retry without
// adjusting the request.
func (e *Executor) Submit(ctx context.Context, req TaskRequest) error {
	if err := e.checkBounds(req); err != nil {
		return err
	}
	if err := req.Task.TransitionTo(model.TaskStateRunning); err != nil {
		return err
	}
	result, err := e.run(ctx, req)
	if err != nil {
		_ = req.Task.TransitionTo(model.TaskStateFailed)
		req.Task.Error = err.Error()
		return nil
	}
	req.Task.Result = result
	return req.Task.TransitionTo(model.TaskStateCompleted)
}

func (e *Executor) checkBounds(req TaskRequest) error {
	if req.CPUMillicores > e.limits.MaxCPUMillicores {
		return errors.New("CPU request exceeds executor limit")
	}
	if req.MemoryMB > e.limits.MaxMemoryMB {
		return errors.New("memory request exceeds executor limit")
	}
	return nil
}

func (e *Executor) run(ctx context.Context, req TaskRequest) ([]byte, error) {
	deadline := time.Now().Add(req.Task.ScheduleToCloseTimeout)
	if req.Task.ScheduleToCloseTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	return req.ActivityHandler(ctx, req.Task)
}
