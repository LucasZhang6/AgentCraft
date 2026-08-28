package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/planning"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/ui"
)

func TestPlanResumeReturnsExactPersistedPlanID(t *testing.T) {
	dataDir := t.TempDir()
	store, err := planning.NewStore(filepath.Join(dataDir, "plans.db"))
	if err != nil {
		t.Fatalf("new plan store: %v", err)
	}
	item, err := store.Save(context.Background(), "session-1", "run-1", "resume this objective", []domain.PlanStep{{
		ID: "step-1", Description: "already planned", SuccessCriteria: "done", Status: domain.PlanPending,
	}})
	if err != nil {
		t.Fatalf("save plan: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close plan store: %v", err)
	}

	input, planID, shouldRun := handlePlanCommand(
		context.Background(), ui.New(), dataDir, "session-1", "/plan resume "+item.ID,
	)
	if !shouldRun || planID != item.ID {
		t.Fatalf("resume routing = input %q plan %q run %v", input, planID, shouldRun)
	}
	if !strings.Contains(input, item.Objective) {
		t.Fatalf("resume display input %q does not identify objective", input)
	}
}

func TestPlanResumeRejectsAnotherSession(t *testing.T) {
	dataDir := t.TempDir()
	store, err := planning.NewStore(filepath.Join(dataDir, "plans.db"))
	if err != nil {
		t.Fatalf("new plan store: %v", err)
	}
	item, err := store.Save(context.Background(), "other-session", "run-1", "private objective", []domain.PlanStep{{
		ID: "step-1", Description: "already planned", SuccessCriteria: "done", Status: domain.PlanPending,
	}})
	if err != nil {
		t.Fatalf("save plan: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close plan store: %v", err)
	}

	_, planID, shouldRun := handlePlanCommand(
		context.Background(), ui.New(), dataDir, "session-1", "/plan resume "+item.ID,
	)
	if shouldRun || planID != "" {
		t.Fatalf("foreign plan was routed for execution: plan %q run %v", planID, shouldRun)
	}
}
