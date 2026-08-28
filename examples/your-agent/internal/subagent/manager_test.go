package subagent

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
)

type runtimeTestModel struct {
	mu    sync.Mutex
	turns int
}

func (model *runtimeTestModel) RunStep(_ context.Context, input domain.StepContext) (domain.ModelTurn, error) {
	model.mu.Lock()
	defer model.mu.Unlock()
	model.turns++
	if model.turns == 1 {
		for _, tool := range input.Tools {
			if tool.Name == "subagent" {
				return domain.ModelTurn{}, errors.New("recursive subagent tool was exposed")
			}
		}
		return domain.ModelTurn{Blocks: []domain.ContentBlock{{
			Type: domain.BlockToolCall, ToolCallID: "call_echo", ToolName: "echo", Arguments: json.RawMessage(`{"value":"hello"}`),
		}}}, nil
	}
	if len(input.SessionHistory) != 2 || input.SessionHistory[1].Role != "tool_results" {
		return domain.ModelTurn{}, errors.New("structured tool history was not preserved")
	}
	return domain.ModelTurn{Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: "delegated result"}}}, nil
}

type runtimeTestTools struct{ calls int }

func (tools *runtimeTestTools) Descriptions() []domain.ToolDescription {
	return []domain.ToolDescription{{Name: "echo", Risk: domain.RiskRead}, {Name: "subagent", Risk: domain.RiskRead}}
}

func (tools *runtimeTestTools) Execute(_ context.Context, name string, args map[string]any) (domain.ToolExecution, error) {
	tools.calls++
	return domain.ToolExecution{Result: map[string]any{"tool": name, "value": args["value"]}}, nil
}

func TestManagerPersistsLifecycle(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "subagents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	record, err := manager.Spawn(context.Background(), Input{ParentSessionID: "session-1", Label: "research", Task: "inspect code"}, func(_ context.Context, task string) (string, error) {
		return "done: " + task, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	finished, err := manager.Wait(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != StatusCompleted || finished.Result != "done: inspect code" || finished.StartedAt == nil || finished.FinishedAt == nil {
		t.Fatalf("unexpected record: %+v", finished)
	}
	items, err := manager.List(context.Background(), "session-1", "", 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("list: items=%+v err=%v", items, err)
	}
}

func TestManagerCancelAndTimeout(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "subagents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	runner := func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}

	cancelled, err := manager.Spawn(context.Background(), Input{Task: "cancel me"}, runner)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Cancel(context.Background(), cancelled.ID)
	if err != nil || result.Status != StatusCanceled {
		t.Fatalf("cancel: record=%+v err=%v", result, err)
	}

	timed, err := manager.Spawn(context.Background(), Input{Task: "timeout", Timeout: 10 * time.Millisecond}, runner)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err = manager.Wait(ctx, timed.ID)
	if err != nil || result.Status != StatusTimedOut {
		t.Fatalf("timeout: record=%+v err=%v", result, err)
	}
}

func TestToolRuntimeUsesIndependentNativeToolLoop(t *testing.T) {
	model := &runtimeTestModel{}
	tools := &runtimeTestTools{}
	runtime, err := NewToolRuntime(model, tools, 4)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), "delegate research")
	if err != nil || result != "delegated result" || tools.calls != 1 {
		t.Fatalf("result=%q calls=%d err=%v", result, tools.calls, err)
	}
}

type truncatedRuntimeModel struct{ turns int }

func (model *truncatedRuntimeModel) RunStep(_ context.Context, input domain.StepContext) (domain.ModelTurn, error) {
	model.turns++
	if model.turns == 1 {
		return domain.ModelTurn{Truncated: true, Blocks: []domain.ContentBlock{{
			Type: domain.BlockToolCall, ToolCallID: "call_echo", ToolName: "echo", Arguments: json.RawMessage(`{"value":"once"}`),
		}}}, nil
	}
	if len(input.SessionHistory) != 3 || input.SessionHistory[0].Role != "assistant_blocks" ||
		input.SessionHistory[1].Role != "tool_results" || input.SessionHistory[2].Role != "user" ||
		!strings.Contains(input.SessionHistory[2].Blocks[0].Text, "Do not repeat successful tool calls") {
		return domain.ModelTurn{}, errors.New("child truncation continuation lost structured history")
	}
	return domain.ModelTurn{Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: "continued child result"}}}, nil
}

func TestToolRuntimeContinuesTruncatedCompleteBlocksWithoutRepeatingTool(t *testing.T) {
	model := &truncatedRuntimeModel{}
	tools := &runtimeTestTools{}
	runtime, err := NewToolRuntime(model, tools, 3)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), "delegate once")
	if err != nil || result != "continued child result" || tools.calls != 1 {
		t.Fatalf("result=%q calls=%d err=%v", result, tools.calls, err)
	}
}

func TestSpawnDetachesFromShortLivedToolContext(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "subagents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	parent, cancelParent := context.WithCancel(context.Background())
	record, err := manager.Spawn(parent, Input{Task: "finish after tool return", Timeout: time.Second}, func(ctx context.Context, task string) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(30 * time.Millisecond):
			return strings.ToUpper(task), nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	cancelParent()
	finished, err := manager.Wait(context.Background(), record.ID)
	if err != nil || finished.Status != StatusCompleted {
		t.Fatalf("record=%+v err=%v", finished, err)
	}
}
