package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/planning"
)

type reactScriptModel struct {
	mu      sync.Mutex
	turns   int
	runStep func(int, domain.StepContext) (domain.ModelTurn, error)
}

func (*reactScriptModel) CreatePlan(context.Context, string, []domain.ToolDescription) (domain.PlanResponse, error) {
	return domain.PlanResponse{}, errors.New("unexpected CreatePlan call")
}

func (model *reactScriptModel) RunStep(_ context.Context, input domain.StepContext) (domain.ModelTurn, error) {
	model.mu.Lock()
	defer model.mu.Unlock()
	model.turns++
	return model.runStep(model.turns, input)
}

func (*reactScriptModel) GenerateFinal(context.Context, domain.FinalContext) (domain.FinalResponse, error) {
	return domain.FinalResponse{}, errors.New("unexpected GenerateFinal call")
}

type reactEventLogger struct {
	mu     sync.Mutex
	events []string
}

func (logger *reactEventLogger) Record(_ context.Context, eventType string, payload any) (domain.Event, error) {
	logger.mu.Lock()
	logger.events = append(logger.events, eventType)
	logger.mu.Unlock()
	return domain.Event{Type: eventType, Payload: payload}, nil
}

func (logger *reactEventLogger) contains(eventType string) bool {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	for _, candidate := range logger.events {
		if candidate == eventType {
			return true
		}
	}
	return false
}

type parallelReadTools struct {
	started chan string
	release <-chan struct{}
}

func (tools *parallelReadTools) Descriptions() []domain.ToolDescription {
	return []domain.ToolDescription{
		{Name: "read_a", Risk: domain.RiskRead, SupportsParallel: true},
		{Name: "read_b", Risk: domain.RiskRead, SupportsParallel: true},
	}
}

func (tools *parallelReadTools) Execute(ctx context.Context, name string, _ map[string]any) (domain.ToolExecution, error) {
	select {
	case tools.started <- name:
	case <-ctx.Done():
		return domain.ToolExecution{}, ctx.Err()
	}
	select {
	case <-tools.release:
		return domain.ToolExecution{Result: map[string]any{"tool": name}}, nil
	case <-ctx.Done():
		return domain.ToolExecution{}, ctx.Err()
	}
}

func TestExecuteStepRunsParallelReadsAndPreservesProviderOrder(t *testing.T) {
	store := newReactPlanStore(t)
	step := domain.PlanStep{ID: "inspect", Description: "inspect both", AllowedTools: []string{"read_a", "read_b"}, Status: domain.PlanPending}
	item, err := store.Save(context.Background(), "session", "run", "inspect", []domain.PlanStep{step})
	if err != nil {
		t.Fatal(err)
	}
	model := &reactScriptModel{runStep: func(turn int, input domain.StepContext) (domain.ModelTurn, error) {
		if turn == 1 {
			return domain.ModelTurn{Blocks: []domain.ContentBlock{
				{Type: domain.BlockToolCall, ToolCallID: "call_a", ToolName: "read_a", Arguments: json.RawMessage(`{}`)},
				{Type: domain.BlockToolCall, ToolCallID: "call_b", ToolName: "read_b", Arguments: json.RawMessage(`{}`)},
			}}, nil
		}
		if len(input.SessionHistory) != 2 || input.SessionHistory[1].Role != "tool_results" {
			return domain.ModelTurn{}, errors.New("native tool history was not preserved")
		}
		results := input.SessionHistory[1].Blocks
		if len(results) != 2 || results[0].ToolCallID != "call_a" || results[1].ToolCallID != "call_b" {
			return domain.ModelTurn{}, errors.New("parallel results were not restored in provider call order")
		}
		return domain.ModelTurn{Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: "done"}}}, nil
	}}
	release := make(chan struct{})
	tools := &parallelReadTools{started: make(chan string, 2), release: release}
	runtime := &YourAgent{model: model, tools: tools, planStore: store, logger: &reactEventLogger{}, maxSteps: 3}
	state := &executionState{metrics: &domain.RunMetrics{}}
	type result struct {
		output string
		err    error
	}
	done := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		output, runErr := runtime.executeStep(ctx, item.ID, 1, step, nil, nil, nil, state)
		done <- result{output: output, err: runErr}
	}()

	parallel := true
	for count := 0; count < 2; count++ {
		select {
		case <-tools.started:
		case <-time.After(300 * time.Millisecond):
			parallel = false
		}
	}
	close(release)
	completed := <-done
	if !parallel {
		t.Fatal("read-only tool calls did not overlap")
	}
	if completed.err != nil || completed.output != "done" {
		t.Fatalf("output=%q err=%v", completed.output, completed.err)
	}
	if state.metrics.ToolCalls != 2 {
		t.Fatalf("metrics = %#v", state.metrics)
	}
}

type immediateReadTool struct{ calls int }

func (*immediateReadTool) Descriptions() []domain.ToolDescription {
	return []domain.ToolDescription{{Name: "read", Risk: domain.RiskRead}}
}

func (tool *immediateReadTool) Execute(_ context.Context, name string, _ map[string]any) (domain.ToolExecution, error) {
	tool.calls++
	return domain.ToolExecution{Result: map[string]any{"tool": name, "ok": true}}, nil
}

func TestExecuteStepContinuesAfterTruncatedCompleteBlocksWithoutRepeatingTool(t *testing.T) {
	store := newReactPlanStore(t)
	step := domain.PlanStep{ID: "read", Description: "read", AllowedTools: []string{"read"}, Status: domain.PlanPending}
	item, err := store.Save(context.Background(), "session", "run", "read", []domain.PlanStep{step})
	if err != nil {
		t.Fatal(err)
	}
	model := &reactScriptModel{runStep: func(turn int, input domain.StepContext) (domain.ModelTurn, error) {
		if turn == 1 {
			return domain.ModelTurn{Truncated: true, Blocks: []domain.ContentBlock{{
				Type: domain.BlockToolCall, ToolCallID: "call_read", ToolName: "read", Arguments: json.RawMessage(`{}`),
			}}}, nil
		}
		if len(input.SessionHistory) != 3 || input.SessionHistory[0].Role != "assistant_blocks" ||
			input.SessionHistory[1].Role != "tool_results" || input.SessionHistory[2].Role != "user" ||
			!strings.Contains(input.SessionHistory[2].Blocks[0].Text, "do not repeat successful calls") {
			return domain.ModelTurn{}, errors.New("truncation continuation did not preserve native ReAct order")
		}
		return domain.ModelTurn{Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: "continued"}}}, nil
	}}
	tools := &immediateReadTool{}
	logger := &reactEventLogger{}
	runtime := &YourAgent{model: model, tools: tools, planStore: store, logger: logger, maxSteps: 3}
	state := &executionState{metrics: &domain.RunMetrics{}}
	output, err := runtime.executeStep(context.Background(), item.ID, 1, step, nil, nil, nil, state)
	if err != nil || output != "continued" {
		t.Fatalf("output=%q err=%v", output, err)
	}
	if tools.calls != 1 || !logger.contains("react_user_message") {
		t.Fatalf("tool calls=%d continuation event=%v", tools.calls, logger.contains("react_user_message"))
	}
}

func TestToolPolicyNeverParallelizesWrites(t *testing.T) {
	policy := toolPolicy("write", []domain.ToolDescription{{Name: "write", Risk: domain.RiskWrite, SupportsParallel: true}})
	if policy.parallel {
		t.Fatal("write-risk tools must remain serial even when a plugin opts into parallel execution")
	}
}

func newReactPlanStore(t *testing.T) *planning.Store {
	t.Helper()
	store, err := planning.NewStore(filepath.Join(t.TempDir(), "plans.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
