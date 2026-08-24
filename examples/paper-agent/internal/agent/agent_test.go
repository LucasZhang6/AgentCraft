package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/agent"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/evaluator"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/planning"
)

type continuationModel struct {
	decisions int
}

type replanModel struct{ decisions int }

func (*replanModel) CreatePlan(context.Context, string, []domain.ToolDescription) (domain.PlanResponse, error) {
	return domain.PlanResponse{Plan: []domain.PlanStep{
		{ID: "search", Description: "search", Tool: "search", SuccessCriteria: "paper found"},
		{ID: "fetch", Description: "fetch optional source", Dependencies: []string{"search"}, Tool: "fetch", SuccessCriteria: "source fetched"},
		{ID: "report", Description: "report", Dependencies: []string{"fetch"}, SuccessCriteria: "report passed"},
	}}, nil
}

func (model *replanModel) Decide(context.Context, domain.DecisionContext) (domain.DecisionResponse, error) {
	model.decisions++
	switch model.decisions {
	case 1:
		return domain.DecisionResponse{Decision: domain.Decision{Type: domain.DecisionTool, Tool: "search"}}, nil
	case 2:
		return domain.DecisionResponse{Decision: domain.Decision{Type: domain.DecisionTool, Tool: "fetch"}}, nil
	default:
		paper := testPaper()
		return domain.DecisionResponse{Decision: domain.Decision{Type: domain.DecisionFinal, Paper: &paper, Content: validReport()}}, nil
	}
}

type replanTools struct{}

func (replanTools) Descriptions() []domain.ToolDescription {
	return []domain.ToolDescription{{Name: "search", Risk: domain.RiskRead}, {Name: "fetch", Risk: domain.RiskRead}}
}

func (replanTools) Execute(_ context.Context, name string, _ map[string]any) (domain.ToolExecution, error) {
	if name == "fetch" {
		return domain.ToolExecution{}, errors.New("source temporarily unavailable")
	}
	return domain.ToolExecution{Result: testPaper()}, nil
}

func TestAgentReplansOnlyReadStepAfterEvaluatorAcceptsAlternateEvidence(t *testing.T) {
	logger := &testLogger{}
	runtime, err := agent.New(agent.Config{
		Model: &replanModel{}, Tools: replanTools{}, Memory: &testMemory{}, Plans: planning.Validator{},
		Evaluator: evaluator.ReportEvaluator{}, Logger: logger, MaxSteps: 4, MaxGoalTurns: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), "explain")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.Plan[1].Status != domain.PlanSkipped || !contains(logger.events, "plan_replanned") {
		t.Fatalf("result=%#v events=%#v", result, logger.events)
	}
}

func (m *continuationModel) CreatePlan(context.Context, string, []domain.ToolDescription) (domain.PlanResponse, error) {
	return domain.PlanResponse{
		Plan: []domain.PlanStep{
			{ID: "search", Description: "search", Tool: "search_papers", SuccessCriteria: "match"},
			{ID: "read", Description: "read", Dependencies: []string{"search"}, Tool: "read_paper_card", SuccessCriteria: "paper card"},
			{ID: "explain", Description: "explain", Dependencies: []string{"read"}, SuccessCriteria: "report"},
		},
		Usage: domain.ModelUsage{TotalTokens: 3},
	}, nil
}

func (m *continuationModel) Decide(context.Context, domain.DecisionContext) (domain.DecisionResponse, error) {
	m.decisions++
	usage := domain.ModelUsage{TotalTokens: 2}
	switch m.decisions {
	case 1:
		return domain.DecisionResponse{Decision: domain.Decision{
			Type: domain.DecisionTool, Tool: "search_papers", Args: map[string]any{"query": "memory"},
		}, Usage: usage}, nil
	case 2:
		return domain.DecisionResponse{Decision: domain.Decision{
			Type: domain.DecisionTool, Tool: "read_paper_card", Args: map[string]any{"id": "paper-1"},
		}, Usage: usage}, nil
	default:
		paper := testPaper()
		report := "# Paper\n\n## 问题背景\n\n" + strings.Repeat("问题与证据。", 35) +
			"\n\n## 核心方法\n\n" + strings.Repeat("方法与步骤。", 35) +
			"\n\n## 工程启发\n\n" + strings.Repeat("实现与验证。", 25) +
			"\n\n## 局限\n\n" + strings.Repeat("边界与成本。", 20) +
			"\n\nhttps://arxiv.org/abs/test"
		return domain.DecisionResponse{Decision: domain.Decision{
			Type: domain.DecisionFinal, Content: report, Paper: &paper,
		}, Usage: usage}, nil
	}
}

func (*continuationModel) Compact(context.Context, domain.CompactionContext) (domain.CompactionResponse, error) {
	return domain.CompactionResponse{
		Summary: "condensed paper search evidence", Usage: domain.ModelUsage{TotalTokens: 1},
	}, nil
}

type testTools struct{}

func (testTools) Descriptions() []domain.ToolDescription {
	return []domain.ToolDescription{
		{Name: "search_papers", Risk: domain.RiskRead},
		{Name: "read_paper_card", Risk: domain.RiskRead},
	}
}

func (testTools) Execute(_ context.Context, name string, _ map[string]any) (domain.ToolExecution, error) {
	if name == "search_papers" {
		return domain.ToolExecution{Result: []domain.PaperMatch{{ID: "paper-1", Title: "Paper"}}, DurationMS: 2}, nil
	}
	return domain.ToolExecution{Result: testPaper(), DurationMS: 2}, nil
}

type testMemory struct {
	written []domain.Memory
}

func (*testMemory) Retrieve(context.Context, domain.MemoryQuery) ([]domain.Memory, error) {
	return nil, nil
}

func (m *testMemory) Remember(_ context.Context, input domain.MemoryInput) (domain.Memory, error) {
	item := domain.Memory{ID: "mem-1", Key: input.Key, Value: input.Value, Source: input.Source, Scope: input.Scope, Status: domain.MemoryActive}
	m.written = append(m.written, item)
	return item, nil
}

type testLogger struct {
	events []string
}

func (l *testLogger) Record(_ context.Context, eventType string, payload any) (domain.Event, error) {
	l.events = append(l.events, eventType)
	return domain.Event{Type: eventType, Payload: payload}, nil
}

func TestAgentContinuesGoalAndCompactsContext(t *testing.T) {
	memory := &testMemory{}
	logger := &testLogger{}
	runtime, err := agent.New(agent.Config{
		Model: &continuationModel{}, Tools: testTools{}, Memory: memory, Plans: planning.Validator{},
		Evaluator: evaluator.ReportEvaluator{}, Logger: logger,
		MaxSteps: 1, MaxGoalTurns: 3, MaxRecentObservations: 1,
	})
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	result, err := runtime.Run(context.Background(), "explain paper")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != "completed" || result.Goal.Turns != 3 {
		t.Fatalf("result status/goal = %#v", result.Goal)
	}
	if result.ContextSummary != "condensed paper search evidence" || result.Metrics.ContextCompactions != 1 {
		t.Fatalf("compaction result = %q, metrics = %#v", result.ContextSummary, result.Metrics)
	}
	if result.Metrics.LLMCalls != 5 || result.Metrics.ToolCalls != 2 || result.Metrics.TotalTokens != 10 {
		t.Fatalf("metrics = %#v", result.Metrics)
	}
	if len(memory.written) != 1 || !contains(logger.events, "goal_continued") || !contains(logger.events, "metrics_recorded") {
		t.Fatalf("memory/events = %#v / %#v", memory.written, logger.events)
	}
}

func testPaper() domain.Paper {
	return domain.Paper{
		ID: "paper-1", Title: "Paper", Module: "Memory", URL: "https://arxiv.org/abs/test",
		Problem: "problem", Method: "method", Contribution: "contribution", Limitation: "limitation", Engineering: "engineering",
	}
}

func validReport() string {
	return "# Paper\n\n## 问题背景\n\n" + strings.Repeat("问题与证据。", 35) +
		"\n\n## 核心方法\n\n" + strings.Repeat("方法与步骤。", 35) +
		"\n\n## 工程启发\n\n" + strings.Repeat("实现与验证。", 25) +
		"\n\n## 局限\n\n" + strings.Repeat("边界与成本。", 20) +
		"\n\nhttps://arxiv.org/abs/test"
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
