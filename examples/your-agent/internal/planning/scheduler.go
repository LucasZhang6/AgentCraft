package planning

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
)

type StepExecutor func(context.Context, domain.PlanStep) (string, error)
type StepApprover func(context.Context, domain.PlanStep) (bool, error)

type Scheduler struct {
	Store       *Store
	Verifier    Verifier
	Concurrency int
	Approve     StepApprover
}

func (scheduler Scheduler) RunReady(ctx context.Context, planID string, execute StepExecutor) (Plan, error) {
	if scheduler.Store == nil || execute == nil {
		return Plan{}, errors.New("plan store and step executor are required")
	}
	item, err := scheduler.Store.Get(ctx, planID)
	if err != nil {
		return Plan{}, err
	}
	ready := readySteps(item.Steps)
	if len(ready) == 0 {
		return item, nil
	}
	limit := scheduler.Concurrency
	if limit <= 0 {
		limit = 4
	}
	sem := make(chan struct{}, limit)
	var wait sync.WaitGroup
	var mu sync.Mutex
	steps := append([]domain.PlanStep(nil), item.Steps...)
	for _, index := range ready {
		steps[index].Status = domain.PlanRunning
	}
	if _, err := scheduler.Store.Update(ctx, item.ID, steps); err != nil {
		return Plan{}, fmt.Errorf("persist running plan steps: %w", err)
	}
	var combined error
	for _, index := range ready {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			step := steps[index]
			if step.RequiresApproval {
				if scheduler.Approve == nil {
					mu.Lock()
					steps[index].Status = domain.PlanFailed
					steps[index].Evidence = append(steps[index].Evidence, "step approval handler is unavailable")
					combined = errors.Join(combined, fmt.Errorf("step %s requires approval", step.ID))
					mu.Unlock()
					return
				}
				approved, err := scheduler.Approve(ctx, step)
				if err != nil || !approved {
					mu.Lock()
					steps[index].Status = domain.PlanFailed
					steps[index].Evidence = append(steps[index].Evidence, "step approval was rejected")
					combined = errors.Join(combined, err, fmt.Errorf("step %s was not approved", step.ID))
					mu.Unlock()
					return
				}
			}
			output, runErr := execute(ctx, step)
			verification := scheduler.Verifier.Verify(step, output)
			mu.Lock()
			defer mu.Unlock()
			steps[index].Attempts++
			steps[index].Output = output
			steps[index].Evidence = append(steps[index].Evidence, verification.Evidence...)
			switch {
			case runErr != nil:
				steps[index].Status = domain.PlanFailed
				steps[index].Evidence = append(steps[index].Evidence, runErr.Error())
				combined = errors.Join(combined, runErr)
			case verification.NeedsHuman:
				steps[index].Status = domain.PlanWaiting
			case !verification.Passed:
				steps[index].Status = domain.PlanFailed
				steps[index].Evidence = append(steps[index].Evidence, verification.FailureMessage)
				combined = errors.Join(combined, errors.New(verification.FailureMessage))
			default:
				steps[index].Status = domain.PlanCompleted
			}
		}()
	}
	wait.Wait()
	updated, updateErr := scheduler.Store.Update(ctx, item.ID, steps)
	return updated, errors.Join(combined, updateErr)
}

// RecoverInterrupted makes a persisted plan resumable after a process stopped
// between marking a step running and committing its terminal state.
func (scheduler Scheduler) RecoverInterrupted(ctx context.Context, planID string) (Plan, error) {
	if scheduler.Store == nil {
		return Plan{}, errors.New("plan store is required")
	}
	item, err := scheduler.Store.Get(ctx, planID)
	if err != nil {
		return Plan{}, err
	}
	changed := false
	for index := range item.Steps {
		if item.Steps[index].Status != domain.PlanRunning {
			continue
		}
		item.Steps[index].Status = domain.PlanPending
		item.Steps[index].Evidence = append(item.Steps[index].Evidence, "recovered interrupted running step")
		changed = true
	}
	if !changed {
		return item, nil
	}
	return scheduler.Store.Update(ctx, item.ID, item.Steps)
}

func (scheduler Scheduler) Accept(ctx context.Context, planID, stepID, evidence string) (Plan, error) {
	item, err := scheduler.Store.Get(ctx, planID)
	if err != nil {
		return Plan{}, err
	}
	found := false
	for index := range item.Steps {
		if item.Steps[index].ID == stepID && item.Steps[index].Status == domain.PlanWaiting {
			item.Steps[index].Status = domain.PlanCompleted
			item.Steps[index].Evidence = append(item.Steps[index].Evidence, "human accepted: "+evidence)
			found = true
		}
	}
	if !found {
		return Plan{}, fmt.Errorf("step %s is not waiting for acceptance", stepID)
	}
	return scheduler.Store.Update(ctx, item.ID, item.Steps)
}

func readySteps(steps []domain.PlanStep) []int {
	completed := make(map[string]bool, len(steps))
	for _, step := range steps {
		completed[step.ID] = step.Status == domain.PlanCompleted || step.Status == domain.PlanSkipped
	}
	var ready []int
	for index, step := range steps {
		if step.Status != domain.PlanPending && step.Status != domain.PlanFailed {
			continue
		}
		dependenciesReady := true
		for _, dependency := range step.Dependencies {
			if !completed[dependency] {
				dependenciesReady = false
				break
			}
		}
		if dependenciesReady {
			ready = append(ready, index)
		}
	}
	return ready
}
