package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
)

const truncatedChildContinuation = "[SYSTEM NOTE] The previous response was truncated after complete blocks were received. Do not repeat successful tool calls. Continue from the interruption and finish the delegated task."

// Model is the native provider turn used by an isolated child runtime.
type Model interface {
	RunStep(context.Context, domain.StepContext) (domain.ModelTurn, error)
}

type ToolExecutor interface {
	Descriptions() []domain.ToolDescription
	Execute(context.Context, string, map[string]any) (domain.ToolExecution, error)
}

// ToolRuntime runs one child task with its own native conversation and tool loop.
// The parent scheduler and observations are intentionally not shared.
type ToolRuntime struct {
	model    Model
	tools    ToolExecutor
	maxTurns int
	excluded map[string]struct{}
}

func NewToolRuntime(model Model, tools ToolExecutor, maxTurns int, excludedTools ...string) (*ToolRuntime, error) {
	if model == nil || tools == nil {
		return nil, errors.New("subagent model and tools are required")
	}
	if maxTurns <= 0 {
		maxTurns = 12
	}
	excluded := map[string]struct{}{"subagent": {}}
	for _, name := range excludedTools {
		name = strings.TrimSpace(name)
		if name != "" {
			excluded[name] = struct{}{}
		}
	}
	return &ToolRuntime{model: model, tools: tools, maxTurns: maxTurns, excluded: excluded}, nil
}

func (runtime *ToolRuntime) Run(ctx context.Context, task string) (string, error) {
	if runtime == nil {
		return "", errors.New("subagent runtime is unavailable")
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return "", errors.New("subagent task is required")
	}
	allowed := runtime.allowedTools()
	allowedNames := make([]string, 0, len(allowed))
	for _, tool := range allowed {
		allowedNames = append(allowedNames, tool.Name)
	}
	step := domain.PlanStep{
		ID: "delegated_task", Description: task, AllowedTools: allowedNames,
		SuccessCriteria: "return a concise result grounded in real tool evidence", Status: domain.PlanRunning,
	}
	var history []domain.SessionMessage
	var observations []domain.Observation
	for turn := 1; turn <= runtime.maxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		modelTurn, err := runtime.model.RunStep(ctx, domain.StepContext{
			Goal: task, PlanID: "subagent", GoalTurn: turn, Step: step, Plan: []domain.PlanStep{step},
			Observations: observations, Tools: allowed, SessionHistory: history,
		})
		if err != nil {
			return "", fmt.Errorf("subagent model turn %d: %w", turn, err)
		}
		if len(modelTurn.Blocks) == 0 {
			return "", fmt.Errorf("subagent model turn %d returned no content", turn)
		}
		history = append(history, domain.SessionMessage{Role: "assistant_blocks", Blocks: cloneRuntimeBlocks(modelTurn.Blocks)})
		calls := runtimeToolCalls(modelTurn.Blocks)
		if len(calls) == 0 {
			if modelTurn.Truncated {
				history = append(history, domain.SessionMessage{Role: "user", Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: truncatedChildContinuation}}})
				continue
			}
			text := runtimeText(modelTurn.Blocks)
			if strings.TrimSpace(text) == "" {
				return "", errors.New("subagent ended without tool calls or result text")
			}
			return text, nil
		}
		outcomes := runtime.executeCalls(ctx, calls, allowed)
		results := make([]domain.ContentBlock, 0, len(outcomes))
		for _, outcome := range outcomes {
			results = append(results, outcome.result)
			observations = append(observations, outcome.observation)
		}
		history = append(history, domain.SessionMessage{Role: "tool_results", Blocks: results})
		if modelTurn.Truncated {
			history = append(history, domain.SessionMessage{Role: "user", Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: truncatedChildContinuation}}})
		}
	}
	return "", fmt.Errorf("subagent exceeded %d native ReAct turns", runtime.maxTurns)
}

type runtimeToolOutcome struct {
	result      domain.ContentBlock
	observation domain.Observation
}

func (runtime *ToolRuntime) executeCalls(ctx context.Context, calls []domain.ContentBlock, allowed []domain.ToolDescription) []runtimeToolOutcome {
	outcomes := make([]runtimeToolOutcome, len(calls))
	for index := 0; index < len(calls); {
		policy := runtimeToolPolicy(calls[index].ToolName, allowed)
		if !policy.parallel {
			result, observation := runtime.execute(ctx, calls[index], allowed)
			outcomes[index] = runtimeToolOutcome{result: result, observation: observation}
			index++
			continue
		}
		end := index + 1
		for end < len(calls) && runtimeToolPolicy(calls[end].ToolName, allowed).parallel {
			end++
		}
		runtime.executeParallelBatch(ctx, calls[index:end], allowed, outcomes[index:end])
		index = end
	}
	return outcomes
}

type runtimeParallelPolicy struct {
	parallel bool
	lane     string
}

func (runtime *ToolRuntime) executeParallelBatch(ctx context.Context, calls []domain.ContentBlock, allowed []domain.ToolDescription, outcomes []runtimeToolOutcome) {
	lanes := make(map[string][]int)
	var order []string
	for index, call := range calls {
		policy := runtimeToolPolicy(call.ToolName, allowed)
		lane := strings.TrimSpace(policy.lane)
		if lane == "" {
			lane = call.ToolName + ":" + string(call.Arguments)
		}
		if _, exists := lanes[lane]; !exists {
			order = append(order, lane)
		}
		lanes[lane] = append(lanes[lane], index)
	}
	var wait sync.WaitGroup
	wait.Add(len(order))
	for _, lane := range order {
		indices := append([]int(nil), lanes[lane]...)
		go func() {
			defer wait.Done()
			for _, index := range indices {
				result, observation := runtime.execute(ctx, calls[index], allowed)
				outcomes[index] = runtimeToolOutcome{result: result, observation: observation}
			}
		}()
	}
	wait.Wait()
}

func runtimeToolPolicy(name string, allowed []domain.ToolDescription) runtimeParallelPolicy {
	for _, tool := range allowed {
		if tool.Name == name {
			return runtimeParallelPolicy{parallel: tool.SupportsParallel && tool.Risk == domain.RiskRead, lane: tool.ConcurrencyGroup}
		}
	}
	return runtimeParallelPolicy{}
}

func (runtime *ToolRuntime) allowedTools() []domain.ToolDescription {
	all := runtime.tools.Descriptions()
	result := make([]domain.ToolDescription, 0, len(all))
	for _, tool := range all {
		if _, excluded := runtime.excluded[tool.Name]; !excluded {
			result = append(result, tool)
		}
	}
	return result
}

func (runtime *ToolRuntime) execute(ctx context.Context, call domain.ContentBlock, allowed []domain.ToolDescription) (domain.ContentBlock, domain.Observation) {
	observation := domain.Observation{ToolCallID: strings.TrimSpace(call.ToolCallID), Tool: strings.TrimSpace(call.ToolName)}
	fail := func(message string) (domain.ContentBlock, domain.Observation) {
		observation.Error = message
		return domain.ContentBlock{Type: domain.BlockToolResult, ToolCallID: observation.ToolCallID, Output: message, IsError: true}, observation
	}
	if observation.ToolCallID == "" {
		return fail("provider returned a subagent tool call without call_id")
	}
	if !runtimeToolAllowed(observation.Tool, allowed) {
		return fail(fmt.Sprintf("tool %s is not allowed in this subagent runtime", observation.Tool))
	}
	args := map[string]any{}
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return fail("invalid native tool arguments: " + err.Error())
		}
	}
	observation.Args = args
	execution, err := runtime.tools.Execute(ctx, observation.Tool, args)
	observation.Result = execution.Result
	observation.OK = err == nil
	if err != nil {
		return fail(err.Error())
	}
	encoded, err := json.Marshal(execution.Result)
	if err != nil {
		return fail("encode tool output: " + err.Error())
	}
	return domain.ContentBlock{Type: domain.BlockToolResult, ToolCallID: observation.ToolCallID, Output: string(encoded)}, observation
}

func runtimeToolAllowed(name string, allowed []domain.ToolDescription) bool {
	for _, tool := range allowed {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func runtimeToolCalls(blocks []domain.ContentBlock) []domain.ContentBlock {
	var result []domain.ContentBlock
	for _, block := range blocks {
		if block.Type == domain.BlockToolCall {
			result = append(result, block)
		}
	}
	return result
}

func runtimeText(blocks []domain.ContentBlock) string {
	var result strings.Builder
	for _, block := range blocks {
		if block.Type == domain.BlockText {
			result.WriteString(block.Text)
		}
	}
	return strings.TrimSpace(result.String())
}

func cloneRuntimeBlocks(blocks []domain.ContentBlock) []domain.ContentBlock {
	result := make([]domain.ContentBlock, len(blocks))
	for index, block := range blocks {
		result[index] = block
		result[index].Arguments = append(json.RawMessage(nil), block.Arguments...)
		result[index].Raw = append(json.RawMessage(nil), block.Raw...)
	}
	return result
}
