package agent_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/agent"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/evaluator"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/planning"
)

type nativeModel struct{}

func (nativeModel) CreatePlan(context.Context, string, []domain.ToolDescription) (domain.PlanResponse, error) {
	return domain.PlanResponse{Plan: []domain.PlanStep{
		{ID: "search", Description: "search", Tool: "search_papers", SuccessCriteria: "paper id"},
		{ID: "read", Description: "read", Dependencies: []string{"search"}, Tool: "read_paper_card", SuccessCriteria: "paper card"},
		{ID: "synthesize", Description: "synthesize", Dependencies: []string{"read"}, SuccessCriteria: "evidence ready"},
	}, Usage: domain.ModelUsage{TotalTokens: 1}}, nil
}

func (nativeModel) RunStep(_ context.Context, input domain.StepContext) (domain.ModelTurn, error) {
	usage := domain.ModelUsage{TotalTokens: 1}
	if input.Step.Tool == "" {
		return domain.ModelTurn{Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: "synthesis complete"}}, Usage: usage}, nil
	}
	if observation, ok := latestToolObservation(input.Observations, input.Step.Tool); ok {
		return domain.ModelTurn{Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: "evidence: " + stringify(observation.Result)}}, Usage: usage}, nil
	}
	args := map[string]any{}
	if input.Step.Tool == "search_papers" {
		args["query"] = "memory"
	} else {
		id := ""
		if observation, ok := latestToolObservation(input.Observations, "search_papers"); ok {
			if matches, valid := observation.Result.([]domain.PaperMatch); valid && len(matches) > 0 {
				id = matches[0].ID
			}
		}
		if id == "" && len(input.Plan) > 0 {
			id = strings.TrimSpace(input.Plan[0].Output)
		}
		args["id"] = id
	}
	encoded, _ := json.Marshal(args)
	return domain.ModelTurn{Blocks: []domain.ContentBlock{{
		Type: domain.BlockToolCall, ToolCallID: "call_" + input.Step.ID, ToolName: input.Step.Tool, Arguments: encoded,
	}}, Usage: usage}, nil
}

func (nativeModel) GenerateFinal(_ context.Context, input domain.FinalContext) (domain.FinalResponse, error) {
	paper := testPaper()
	if observation, ok := latestToolObservation(input.Observations, "read_paper_card"); ok {
		if value, valid := observation.Result.(domain.Paper); valid {
			paper = value
		}
	}
	report := validReport(paper)
	return domain.FinalResponse{Content: report, Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: report}}, Usage: domain.ModelUsage{TotalTokens: 1}}, nil
}

type testTools struct{ calls []string }

func (tools *testTools) Descriptions() []domain.ToolDescription {
	return []domain.ToolDescription{
		{Name: "search_papers", Risk: domain.RiskRead, Schema: objectSchema("query")},
		{Name: "read_paper_card", Risk: domain.RiskRead, Schema: objectSchema("id")},
	}
}

func (tools *testTools) Execute(_ context.Context, name string, _ map[string]any) (domain.ToolExecution, error) {
	tools.calls = append(tools.calls, name)
	if name == "search_papers" {
		return domain.ToolExecution{Result: []domain.PaperMatch{{ID: "paper-1", Title: "Paper"}}}, nil
	}
	return domain.ToolExecution{Result: testPaper()}, nil
}

type testMemory struct{ written []domain.Memory }

func (*testMemory) Retrieve(context.Context, domain.MemoryQuery) ([]domain.Memory, error) {
	return nil, nil
}
func (memory *testMemory) Remember(_ context.Context, input domain.MemoryInput) (domain.Memory, error) {
	item := domain.Memory{ID: "mem-1", Key: input.Key, Value: input.Value, Source: input.Source, Scope: input.Scope}
	memory.written = append(memory.written, item)
	return item, nil
}

type testLogger struct{ events []string }

func (logger *testLogger) Record(_ context.Context, eventType string, payload any) (domain.Event, error) {
	logger.events = append(logger.events, eventType)
	return domain.Event{Type: eventType, Payload: payload}, nil
}

func TestAgentRunsNativeReActThroughPersistedScheduler(t *testing.T) {
	store := newPlanStore(t)
	tools := &testTools{}
	memory := &testMemory{}
	logger := &testLogger{}
	runtime := newAgent(t, store, tools, memory, logger, "run-new")

	result, err := runtime.Run(context.Background(), "explain paper")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || len(tools.calls) != 2 || result.Metrics.ToolCalls != 2 {
		t.Fatalf("result=%#v calls=%#v", result, tools.calls)
	}
	for _, step := range result.Plan {
		if step.Status != domain.PlanCompleted || strings.TrimSpace(step.Output) == "" {
			t.Fatalf("scheduled step was not persisted: %#v", step)
		}
	}
	if !contains(logger.events, "assistant_blocks") || !contains(logger.events, "tool_results") || !contains(logger.events, "scheduler_wave_started") {
		t.Fatalf("native scheduler events = %#v", logger.events)
	}
	if len(memory.written) != 1 || memory.written[0].Source != testPaper().URL {
		t.Fatalf("memory = %#v", memory.written)
	}
}

type controlledSetModel struct{}

func (controlledSetModel) CreatePlan(context.Context, string, []domain.ToolDescription) (domain.PlanResponse, error) {
	return domain.PlanResponse{Plan: []domain.PlanStep{{
		ID: "research", Description: "search and read", AllowedTools: []string{"search_papers", "read_paper_card"}, SuccessCriteria: "paper evidence",
	}}}, nil
}

func (controlledSetModel) RunStep(_ context.Context, input domain.StepContext) (domain.ModelTurn, error) {
	if _, ok := latestToolObservation(input.Observations, "search_papers"); !ok {
		return toolTurn("call_search", "search_papers", map[string]any{"query": "memory"}), nil
	}
	if _, ok := latestToolObservation(input.Observations, "read_paper_card"); !ok {
		return toolTurn("call_read", "read_paper_card", map[string]any{"id": "paper-1"}), nil
	}
	return domain.ModelTurn{Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: "paper evidence ready"}}}, nil
}

func (controlledSetModel) GenerateFinal(context.Context, domain.FinalContext) (domain.FinalResponse, error) {
	report := validReport(testPaper())
	return domain.FinalResponse{Content: report, Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: report}}}, nil
}

func TestPlanStepAllowsControlledToolSet(t *testing.T) {
	store := newPlanStore(t)
	toolset := &testTools{}
	runtime, err := agent.New(agent.Config{
		Model: controlledSetModel{}, Tools: toolset, Memory: &testMemory{}, Plans: planning.Validator{}, PlanStore: store,
		Scheduler: planning.Scheduler{Store: store}, Evaluator: evaluator.ReportEvaluator{}, Logger: &testLogger{},
		SessionID: "session-1", RunID: "controlled-tools", MaxSteps: 4, MaxGoalTurns: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), "research one paper")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || strings.Join(toolset.calls, ",") != "search_papers,read_paper_card" {
		t.Fatalf("result=%#v calls=%#v", result, toolset.calls)
	}
}

func toolTurn(id, name string, args map[string]any) domain.ModelTurn {
	encoded, _ := json.Marshal(args)
	return domain.ModelTurn{Blocks: []domain.ContentBlock{{Type: domain.BlockToolCall, ToolCallID: id, ToolName: name, Arguments: encoded}}}
}

func TestResumePlanRecoversRunningStepWithoutRepeatingCompletedStep(t *testing.T) {
	store := newPlanStore(t)
	created, err := store.Save(context.Background(), "session-1", "old-run", "resume paper", []domain.PlanStep{
		{ID: "search", Description: "search", Tool: "search_papers", SuccessCriteria: "paper id", Status: domain.PlanCompleted, Output: "paper-1", Attempts: 1},
		{ID: "read", Description: "read", Dependencies: []string{"search"}, Tool: "read_paper_card", SuccessCriteria: "paper card", Status: domain.PlanRunning},
		{ID: "synthesize", Description: "synthesize", Dependencies: []string{"read"}, SuccessCriteria: "evidence ready", Status: domain.PlanPending},
	})
	if err != nil {
		t.Fatal(err)
	}
	tools := &testTools{}
	runtime := newAgent(t, store, tools, &testMemory{}, &testLogger{}, "resume-run")

	result, err := runtime.ResumePlanWithSession(context.Background(), created.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || strings.Join(tools.calls, ",") != "read_paper_card" {
		t.Fatalf("result=%#v calls=%#v", result, tools.calls)
	}
	if result.Plan[0].Attempts != 1 || !contains(result.Plan[1].Evidence, "recovered interrupted running step") {
		t.Fatalf("resume repeated completed work or lost recovery evidence: %#v", result.Plan)
	}
}

func TestResumePlanRejectsAnotherSessionBeforeRecovery(t *testing.T) {
	store := newPlanStore(t)
	created, err := store.Save(context.Background(), "other-session", "old-run", "private plan", []domain.PlanStep{{
		ID: "search", Description: "search", Tool: "search_papers", SuccessCriteria: "paper id", Status: domain.PlanRunning,
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime := newAgent(t, store, &testTools{}, &testMemory{}, &testLogger{}, "resume-run")

	_, err = runtime.ResumePlanWithSession(context.Background(), created.ID, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "belongs to session") {
		t.Fatalf("resume error = %v", err)
	}
	item, getErr := store.Get(context.Background(), created.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if item.Steps[0].Status != domain.PlanRunning {
		t.Fatalf("foreign plan was mutated before ownership check: %#v", item.Steps[0])
	}
}

func newPlanStore(t *testing.T) *planning.Store {
	t.Helper()
	store, err := planning.NewStore(filepath.Join(t.TempDir(), "plans.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newAgent(t *testing.T, store *planning.Store, tools *testTools, memory *testMemory, logger *testLogger, runID string) *agent.YourAgent {
	t.Helper()
	runtime, err := agent.New(agent.Config{
		Model: nativeModel{}, Tools: tools, Memory: memory, Plans: planning.Validator{}, PlanStore: store,
		Scheduler: planning.Scheduler{Store: store, Concurrency: 2}, Evaluator: evaluator.ReportEvaluator{}, Logger: logger,
		SessionID: "session-1", RunID: runID, MaxSteps: 3, MaxGoalTurns: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func objectSchema(required string) domain.ToolSchema {
	return domain.ToolSchema{Type: "object", Required: []string{required}, Properties: map[string]domain.ToolField{required: {Type: "string"}}}
}

func latestToolObservation(observations []domain.Observation, tool string) (domain.Observation, bool) {
	for index := len(observations) - 1; index >= 0; index-- {
		if observations[index].Tool == tool && observations[index].OK {
			return observations[index], true
		}
	}
	return domain.Observation{}, false
}

func stringify(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func testPaper() domain.Paper {
	return domain.Paper{ID: "paper-1", Title: "Paper", Module: "Memory", URL: "https://arxiv.org/abs/test"}
}

func validReport(paper domain.Paper) string {
	return "# Paper\n\n## 问题背景\n\n" + strings.Repeat("问题与证据。", 35) +
		"\n\n## 核心方法\n\n" + strings.Repeat("方法与步骤。", 35) +
		"\n\n## 工程启发\n\n" + strings.Repeat("实现与验证。", 25) +
		"\n\n## 局限\n\n" + strings.Repeat("边界与成本。", 20) + "\n\n" + paper.URL
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
