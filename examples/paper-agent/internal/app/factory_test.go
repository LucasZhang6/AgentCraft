package app_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/app"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/memory"
)

func TestAgentCompletesPaperLearningTrajectory(t *testing.T) {
	dataDir := t.TempDir()
	runtime, err := app.New(app.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer runtime.Close()
	result, err := runtime.Agent.Run(context.Background(), "解读 Agent Memory 的代表性论文")
	if err != nil {
		t.Fatalf("run agent: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	metricsSummary, err := runtime.MetricsSummary(context.Background())
	if err != nil {
		t.Fatalf("metrics summary: %v", err)
	}
	if metricsSummary.Runs != 1 || metricsSummary.TaskSuccessRate != 1 {
		t.Fatalf("metrics summary = %#v", metricsSummary)
	}
	if !result.Evaluation.Passed {
		t.Fatalf("evaluation did not pass: %#v", result.Evaluation.Checks)
	}
	if !strings.Contains(result.Report, "A-MEM") {
		t.Fatalf("report does not describe A-MEM")
	}
	for _, step := range result.Plan {
		if step.Status != "completed" {
			t.Fatalf("plan step %q status = %q", step.ID, step.Status)
		}
	}

	logData, err := os.ReadFile(runtime.LogPath)
	if err != nil {
		t.Fatalf("read trajectory: %v", err)
	}
	for _, eventType := range []string{"tool_succeeded", "report_evaluated", "run_completed"} {
		if !strings.Contains(string(logData), `"type":"`+eventType+`"`) {
			t.Fatalf("trajectory does not contain %s", eventType)
		}
	}

	memoryStore, err := memory.NewStore(runtime.MemoryPath)
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer memoryStore.Close()
	memories, err := memoryStore.List(context.Background())
	if err != nil {
		t.Fatalf("list memory: %v", err)
	}
	if len(memories) != 1 || memories[0].Key != "last_paper_topic" {
		t.Fatalf("unexpected memories: %#v", memories)
	}
	if memories[0].Source != "https://arxiv.org/abs/2502.12110" {
		t.Fatalf("memory source = %q", memories[0].Source)
	}
}

func TestAgentStopsWhenStepBudgetIsTooSmall(t *testing.T) {
	runtime, err := app.New(app.Config{DataDir: t.TempDir(), MaxSteps: 1, MaxGoalTurns: 1})
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer runtime.Close()
	result, err := runtime.Agent.Run(context.Background(), "解读 Tool Use")
	if err != nil {
		t.Fatalf("run agent: %v", err)
	}
	if result.Status != "stopped" || result.Reason != "goal_turn_limit_exceeded" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
