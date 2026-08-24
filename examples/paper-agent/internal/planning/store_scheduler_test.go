package planning_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/planning"
)

func TestPlanStorePersistsStepProgress(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "plans.db")
	store, err := planning.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.Save(ctx, "session-1", "run-1", "ship", []domain.PlanStep{{
		ID: "build", Description: "build", SuccessCriteria: "binary exists", Status: domain.PlanPending,
	}})
	if err != nil {
		t.Fatal(err)
	}
	created.Steps[0].Status = domain.PlanCompleted
	created.Steps[0].Evidence = []string{"build passed"}
	if _, err := store.Update(ctx, created.ID, created.Steps); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := planning.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	latest, err := reopened.Latest(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != domain.PlanCompleted || latest.Steps[0].Evidence[0] != "build passed" {
		t.Fatalf("persisted plan = %#v", latest)
	}
}

func TestSchedulerRunsReadyRolesInParallelAndWaitsForHuman(t *testing.T) {
	ctx := context.Background()
	store, err := planning.NewStore(filepath.Join(t.TempDir(), "plans.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	item, err := store.Save(ctx, "session-1", "run-1", "parallel", []domain.PlanStep{
		{ID: "research", Description: "research", AgentRole: "researcher", SuccessCriteria: "facts", Status: domain.PlanPending},
		{ID: "review", Description: "review", AgentRole: "reviewer", SuccessCriteria: "review", Status: domain.PlanPending},
		{ID: "release", Description: "release", Dependencies: []string{"research", "review"}, AgentRole: "operator", SuccessCriteria: "approved", Status: domain.PlanPending, Acceptance: []domain.AcceptanceCheck{{Type: "human"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var active, maximum atomic.Int32
	execute := func(_ context.Context, _ domain.PlanStep) (string, error) {
		value := active.Add(1)
		for {
			previous := maximum.Load()
			if value <= previous || maximum.CompareAndSwap(previous, value) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		return "ok", nil
	}
	scheduler := planning.Scheduler{Store: store, Concurrency: 2}
	item, err = scheduler.RunReady(ctx, item.ID, execute)
	if err != nil {
		t.Fatal(err)
	}
	if maximum.Load() < 2 || item.Steps[2].Status != domain.PlanPending {
		t.Fatalf("first wave did not run in parallel: max=%d plan=%#v", maximum.Load(), item)
	}
	item, err = scheduler.RunReady(ctx, item.ID, execute)
	if err != nil {
		t.Fatal(err)
	}
	if item.Steps[2].Status != domain.PlanWaiting {
		t.Fatalf("release status = %s, want waiting", item.Steps[2].Status)
	}
	item, err = scheduler.Accept(ctx, item.ID, "release", "owner checked artifacts")
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != domain.PlanCompleted {
		t.Fatalf("plan status = %s", item.Status)
	}
}
