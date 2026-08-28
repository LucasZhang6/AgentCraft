package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/planning"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/session"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/verification"
)

var ErrEmptyGoal = errors.New("goal must not be empty")

const truncationContinuation = "[SYSTEM NOTE] The previous model response was cut off after some complete content blocks were received. Tool results above are authoritative: do not repeat successful calls. Continue from the interruption, retry only calls without results, and finish the step without repeating completed work."

type Model interface {
	CreatePlan(context.Context, string, []domain.ToolDescription) (domain.PlanResponse, error)
	RunStep(context.Context, domain.StepContext) (domain.ModelTurn, error)
	GenerateFinal(context.Context, domain.FinalContext) (domain.FinalResponse, error)
}

type ContextCompactor interface {
	Compact(context.Context, domain.CompactionContext) (domain.CompactionResponse, error)
}

type ImagePlanningModel interface {
	CreatePlanWithImages(context.Context, string, []string, []domain.ToolDescription) (domain.PlanResponse, error)
}

type SessionPlanningModel interface {
	CreatePlanWithSession(context.Context, string, []string, []domain.ToolDescription, []domain.SessionMessage) (domain.PlanResponse, error)
}

type ToolExecutor interface {
	Descriptions() []domain.ToolDescription
	Execute(context.Context, string, map[string]any) (domain.ToolExecution, error)
}

type MemoryStore interface {
	Retrieve(context.Context, domain.MemoryQuery) ([]domain.Memory, error)
	Remember(context.Context, domain.MemoryInput) (domain.Memory, error)
}

type PlanValidator interface {
	ValidateAndNormalize([]domain.PlanStep, []domain.ToolDescription) ([]domain.PlanStep, error)
}

type Evaluator interface {
	Evaluate(string) domain.Evaluation
}

type Logger interface {
	Record(context.Context, string, any) (domain.Event, error)
}

type MetricsRecorder interface {
	Record(context.Context, domain.RunMetrics) error
}

type YourAgent struct {
	model                 Model
	tools                 ToolExecutor
	memory                MemoryStore
	plans                 PlanValidator
	planStore             *planning.Store
	scheduler             planning.Scheduler
	verification          *verification.Gate
	evaluator             Evaluator
	logger                Logger
	metricsRecorder       MetricsRecorder
	sessionID             string
	runID                 string
	maxSteps              int
	maxGoalTurns          int
	tokenBudget           int
	maxRecentObservations int
	memoryLimit           int
	memoryBytes           int
	skillPrompt           string
	now                   func() time.Time
}

type Config struct {
	Model                 Model
	Tools                 ToolExecutor
	Memory                MemoryStore
	Plans                 PlanValidator
	PlanStore             *planning.Store
	Scheduler             planning.Scheduler
	Verification          *verification.Gate
	Evaluator             Evaluator
	Logger                Logger
	MetricsRecorder       MetricsRecorder
	SessionID             string
	RunID                 string
	MaxSteps              int
	MaxGoalTurns          int
	TokenBudget           int
	MaxRecentObservations int
	MemoryLimit           int
	MemoryBytes           int
	SkillPrompt           string
}

type Result struct {
	Status         string               `json:"status"`
	Reason         string               `json:"reason,omitempty"`
	Report         string               `json:"report,omitempty"`
	Plan           []domain.PlanStep    `json:"plan"`
	Evaluation     domain.Evaluation    `json:"evaluation"`
	Steps          int                  `json:"steps"`
	Observations   []domain.Observation `json:"observations"`
	ContextSummary string               `json:"contextSummary,omitempty"`
	Goal           domain.GoalState     `json:"goal"`
	Metrics        domain.RunMetrics    `json:"metrics"`
}

func New(config Config) (*YourAgent, error) {
	if config.Model == nil || config.Tools == nil || config.Memory == nil || config.Plans == nil ||
		config.PlanStore == nil || config.Evaluator == nil || config.Logger == nil {
		return nil, errors.New("model, tools, memory, plans, plan store, evaluator, and logger are required")
	}
	if config.MaxSteps <= 0 {
		config.MaxSteps = 6
	}
	if config.MaxGoalTurns < 0 {
		config.MaxGoalTurns = 0
	}
	if config.MaxRecentObservations <= 0 {
		config.MaxRecentObservations = 4
	}
	if config.MemoryLimit <= 0 {
		config.MemoryLimit = 8
	}
	if config.MemoryBytes <= 0 {
		config.MemoryBytes = 4000
	}
	if strings.TrimSpace(config.SessionID) == "" {
		config.SessionID = "standalone"
	}
	if strings.TrimSpace(config.RunID) == "" {
		config.RunID = "standalone"
	}
	scheduler := config.Scheduler
	if scheduler.Store == nil {
		scheduler.Store = config.PlanStore
	}
	if scheduler.Approve == nil {
		// Tool-level approval remains authoritative because it receives the real arguments.
		scheduler.Approve = func(context.Context, domain.PlanStep) (bool, error) { return true, nil }
	}
	return &YourAgent{
		model: config.Model, tools: config.Tools, memory: config.Memory, plans: config.Plans,
		planStore: config.PlanStore, scheduler: scheduler, verification: config.Verification, evaluator: config.Evaluator, logger: config.Logger,
		metricsRecorder: config.MetricsRecorder, sessionID: strings.TrimSpace(config.SessionID), runID: strings.TrimSpace(config.RunID),
		maxSteps: config.MaxSteps, maxGoalTurns: config.MaxGoalTurns, tokenBudget: max(config.TokenBudget, 0),
		maxRecentObservations: config.MaxRecentObservations, memoryLimit: config.MemoryLimit, memoryBytes: config.MemoryBytes,
		skillPrompt: strings.TrimSpace(config.SkillPrompt),
		now:         time.Now,
	}, nil
}

func (a *YourAgent) Run(ctx context.Context, goal string) (Result, error) {
	return a.RunWithSession(ctx, goal, nil, nil)
}

func (a *YourAgent) RunWithImages(ctx context.Context, goal string, images []string) (Result, error) {
	return a.RunWithSession(ctx, goal, images, nil)
}

func (a *YourAgent) RunWithSession(ctx context.Context, goal string, images []string, history []domain.SessionMessage) (result Result, retErr error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return Result{}, ErrEmptyGoal
	}
	metrics := domain.RunMetrics{StartedAt: a.now().UTC()}
	goalState := domain.GoalState{Objective: goal, Status: domain.GoalPursuing, MaxTurns: a.maxGoalTurns, TokenBudget: a.tokenBudget}
	defer a.captureFailure(ctx, &result, &retErr, &metrics, &goalState)

	tools := a.tools.Descriptions()
	planResponse, err := a.createPlan(ctx, goal, images, tools, history)
	if err != nil {
		return result, err
	}
	applyUsage(&metrics, planResponse.Usage)
	if len(planResponse.ReasoningBlocks) > 0 {
		if err := a.record(ctx, "assistant_blocks", map[string]any{"phase": "planning", "blocks": planResponse.ReasoningBlocks}); err != nil {
			return result, err
		}
	}
	steps, err := a.plans.ValidateAndNormalize(planResponse.Plan, tools)
	if err != nil {
		return result, fmt.Errorf("validate generated plan: %w", err)
	}
	item, err := a.planStore.Save(ctx, a.sessionID, a.runID, goal, steps)
	if err != nil {
		return result, fmt.Errorf("persist generated plan: %w", err)
	}
	if err := a.record(ctx, "plan_created", map[string]any{"planId": item.ID, "plan": item.Steps, "validated": true}); err != nil {
		return result, err
	}
	memories, err := a.retrieveMemories(ctx, goal)
	if err != nil {
		return result, err
	}
	return a.executePlan(ctx, item.ID, images, history, memories, &metrics, &goalState)
}

func (a *YourAgent) ResumePlanWithSession(ctx context.Context, planID string, images []string, history []domain.SessionMessage) (result Result, retErr error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return Result{}, errors.New("plan id is required")
	}
	item, err := a.planStore.Get(ctx, planID)
	if err != nil {
		return result, fmt.Errorf("load persisted plan: %w", err)
	}
	if item.SessionID != a.sessionID {
		return result, fmt.Errorf("plan %s belongs to session %s, not %s", planID, item.SessionID, a.sessionID)
	}
	metrics := domain.RunMetrics{StartedAt: a.now().UTC()}
	item, err = a.scheduler.RecoverInterrupted(ctx, planID)
	if err != nil {
		return result, fmt.Errorf("recover persisted plan: %w", err)
	}
	goalState := domain.GoalState{Objective: item.Objective, Status: domain.GoalPursuing, MaxTurns: a.maxGoalTurns, TokenBudget: a.tokenBudget}
	defer a.captureFailure(ctx, &result, &retErr, &metrics, &goalState)
	memories, err := a.retrieveMemories(ctx, item.Objective)
	if err != nil {
		return result, err
	}
	if err := a.record(ctx, "plan_resumed", map[string]any{"planId": item.ID, "status": item.Status}); err != nil {
		return result, err
	}
	return a.executePlan(ctx, item.ID, images, history, memories, &metrics, &goalState)
}

func (a *YourAgent) createPlan(ctx context.Context, goal string, images []string, tools []domain.ToolDescription, history []domain.SessionMessage) (domain.PlanResponse, error) {
	planningGoal := goal
	if a.skillPrompt != "" {
		planningGoal += a.skillPrompt
	}
	var response domain.PlanResponse
	var err error
	if planner, ok := a.model.(SessionPlanningModel); ok {
		response, err = planner.CreatePlanWithSession(ctx, planningGoal, images, tools, history)
	} else if len(images) > 0 {
		planner, ok := a.model.(ImagePlanningModel)
		if !ok {
			return response, errors.New("configured model does not support image input")
		}
		response, err = planner.CreatePlanWithImages(ctx, planningGoal, images, tools)
	} else {
		response, err = a.model.CreatePlan(ctx, planningGoal, tools)
	}
	if err != nil {
		return response, fmt.Errorf("create plan: %w", err)
	}
	return response, nil
}

func (a *YourAgent) retrieveMemories(ctx context.Context, goal string) ([]domain.Memory, error) {
	memories, err := a.memory.Retrieve(ctx, domain.MemoryQuery{
		Text: goal, Scopes: []string{"user", "project", "learning-preference"}, Limit: a.memoryLimit, LimitBytes: a.memoryBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("retrieve memory: %w", err)
	}
	if err := a.record(ctx, "memory_retrieved", map[string]any{
		"count": len(memories), "limit": a.memoryLimit, "limitBytes": a.memoryBytes,
		"scopes": []string{"user", "project", "learning-preference"},
	}); err != nil {
		return nil, err
	}
	return memories, nil
}

type executionState struct {
	mu           sync.Mutex
	metrics      *domain.RunMetrics
	observations []domain.Observation
	recent       []domain.Observation
	summary      string
	modelTurns   int
}

func (state *executionState) applyUsage(usage domain.ModelUsage) {
	state.mu.Lock()
	applyUsage(state.metrics, usage)
	state.modelTurns++
	state.mu.Unlock()
}

func (state *executionState) addTool(execution domain.ToolExecution, observation domain.Observation) {
	state.mu.Lock()
	state.metrics.ToolCalls++
	state.metrics.ToolDurationMS += execution.DurationMS
	if execution.ApprovalRequested {
		state.metrics.HumanApprovalRequests++
	}
	if !observation.OK {
		state.metrics.ToolFailures++
	}
	state.observations = append(state.observations, observation)
	state.recent = append(state.recent, observation)
	state.mu.Unlock()
}

func (state *executionState) snapshot() ([]domain.Observation, []domain.Observation, string, int, int) {
	state.mu.Lock()
	defer state.mu.Unlock()
	return append([]domain.Observation(nil), state.observations...), append([]domain.Observation(nil), state.recent...),
		state.summary, state.modelTurns, state.metrics.TotalTokens
}

func (state *executionState) setCompaction(summary string, recent []domain.Observation) {
	state.mu.Lock()
	state.summary = summary
	state.recent = recent
	state.mu.Unlock()
}

func (state *executionState) addSessionCompaction() {
	state.mu.Lock()
	state.metrics.ContextCompactions++
	state.mu.Unlock()
}

func (a *YourAgent) executePlan(
	ctx context.Context,
	planID string,
	images []string,
	history []domain.SessionMessage,
	memories []domain.Memory,
	metrics *domain.RunMetrics,
	goalState *domain.GoalState,
) (Result, error) {
	state := &executionState{metrics: metrics}
	if err := a.record(ctx, "run_started", map[string]any{
		"goal": goalState.Objective, "planId": planID, "maxStepTurns": a.maxSteps,
		"maxGoalTurns": a.maxGoalTurns, "tokenBudget": a.tokenBudget,
	}); err != nil {
		return Result{}, err
	}

	for wave := 1; a.maxGoalTurns == 0 || wave <= a.maxGoalTurns; wave++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		_, _, _, _, tokens := state.snapshot()
		if budgetExceeded(a.tokenBudget, tokens) {
			goalState.Status = domain.GoalStopped
			goalState.StopReason = "token_budget_exceeded"
			return a.stoppedResult(ctx, planID, state, *goalState)
		}
		goalState.Turns = wave
		metrics.GoalTurns = wave
		before, err := a.planStore.Get(ctx, planID)
		if err != nil {
			return Result{}, err
		}
		if before.Status == domain.PlanCompleted {
			before, injected, gateErr := a.applyVerificationGate(ctx, before)
			if gateErr != nil {
				return Result{}, gateErr
			}
			if injected {
				continue
			}
			if before.Status == domain.PlanCompleted {
				break
			}
		}
		if before.Status == domain.PlanWaiting {
			goalState.Status = domain.GoalStopped
			goalState.StopReason = "plan_waiting_for_acceptance"
			return a.stoppedResult(ctx, planID, state, *goalState)
		}
		if err := a.record(ctx, "scheduler_wave_started", map[string]any{"planId": planID, "wave": wave}); err != nil {
			return Result{}, err
		}
		updated, runErr := a.scheduler.RunReady(ctx, planID, func(stepCtx context.Context, step domain.PlanStep) (string, error) {
			return a.executeStep(stepCtx, planID, wave, step, images, history, memories, state)
		})
		if runErr != nil {
			_ = a.record(ctx, "scheduler_wave_failed", map[string]any{"planId": planID, "wave": wave, "error": runErr.Error()})
			goalState.Status = domain.GoalStopped
			goalState.StopReason = "plan_step_failed"
			return a.stoppedResult(ctx, planID, state, *goalState)
		}
		if err := a.compactState(ctx, goalState.Objective, state, false); err != nil {
			return Result{}, err
		}
		if updated.Status == domain.PlanCompleted {
			updated, injected, gateErr := a.applyVerificationGate(ctx, updated)
			if gateErr != nil {
				return Result{}, gateErr
			}
			if injected {
				if err := a.record(ctx, "goal_continued", map[string]any{"completedTurn": wave, "nextTurn": wave + 1, "planId": planID, "reason": "verification_gate"}); err != nil {
					return Result{}, err
				}
				continue
			}
			if updated.Status == domain.PlanCompleted {
				break
			}
		}
		if updated.Status == domain.PlanWaiting {
			goalState.Status = domain.GoalStopped
			goalState.StopReason = "plan_waiting_for_acceptance"
			return a.stoppedResult(ctx, planID, state, *goalState)
		}
		if planProgressKey(before.Steps) == planProgressKey(updated.Steps) {
			goalState.Status = domain.GoalStopped
			goalState.StopReason = "plan_has_no_ready_steps"
			return a.stoppedResult(ctx, planID, state, *goalState)
		}
		if err := a.record(ctx, "goal_continued", map[string]any{"completedTurn": wave, "nextTurn": wave + 1, "planId": planID}); err != nil {
			return Result{}, err
		}
	}

	item, err := a.planStore.Get(ctx, planID)
	if err != nil {
		return Result{}, err
	}
	if item.Status != domain.PlanCompleted {
		goalState.Status = domain.GoalStopped
		goalState.StopReason = "goal_turn_limit_exceeded"
		return a.stoppedResult(ctx, planID, state, *goalState)
	}
	return a.finishPlan(ctx, item, images, history, memories, state, goalState)
}

func (a *YourAgent) applyVerificationGate(ctx context.Context, item planning.Plan) (planning.Plan, bool, error) {
	if a.verification == nil {
		return item, false, nil
	}
	updated, injected, err := a.verification.Ensure(ctx, a.planStore, item)
	if err != nil {
		_ = a.record(ctx, "verification_failed", map[string]any{"planId": item.ID, "error": err.Error(), "gate": a.verification.Snapshot()})
		return item, false, err
	}
	if injected {
		if err := a.record(ctx, "verification_required", map[string]any{"planId": item.ID, "gate": a.verification.Snapshot()}); err != nil {
			return item, false, err
		}
	}
	return updated, injected, nil
}

func (a *YourAgent) executeStep(
	ctx context.Context,
	planID string,
	wave int,
	step domain.PlanStep,
	images []string,
	history []domain.SessionMessage,
	memories []domain.Memory,
	state *executionState,
) (string, error) {
	allowedTools, err := a.toolsForStep(step)
	if err != nil {
		return "", err
	}
	localHistory := append([]domain.SessionMessage(nil), history...)
	contextRecoveries := 0
	for turn := 1; turn <= a.maxSteps; turn++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		_, recent, summary, _, tokens := state.snapshot()
		if budgetExceeded(a.tokenBudget, tokens) {
			return "", errors.New("token budget exceeded while executing plan step")
		}
		item, err := a.planStore.Get(ctx, planID)
		if err != nil {
			return "", err
		}
		currentStep := findPlanStep(item.Steps, step.ID)
		if currentStep == nil {
			return "", fmt.Errorf("persisted plan no longer contains step %s", step.ID)
		}
		localHistory, err = a.prepareStepHistory(ctx, planID, step.ID, localHistory, false, session.DefaultMinRecentTurns, state)
		if err != nil {
			return "", err
		}
		var modelTurn domain.ModelTurn
		for {
			modelTurn, err = a.model.RunStep(ctx, domain.StepContext{
				Goal: item.Objective, PlanID: planID, GoalTurn: wave, Images: images, Step: *currentStep, Plan: item.Steps,
				ContextSummary: summary, Observations: recent, Memories: memories, Skills: a.skillPrompt, Tools: allowedTools, SessionHistory: localHistory,
			})
			if err == nil || !isContextLengthError(err) || contextRecoveries >= 2 {
				break
			}
			minRecent := 1
			if contextRecoveries > 0 {
				minRecent = 0
			}
			var compactErr error
			localHistory, compactErr = a.prepareStepHistory(ctx, planID, step.ID, localHistory, true, minRecent, state)
			if compactErr != nil {
				return "", compactErr
			}
			contextRecoveries++
		}
		if err != nil {
			return "", fmt.Errorf("run native model turn for step %s: %w", step.ID, err)
		}
		state.applyUsage(modelTurn.Usage)
		if len(modelTurn.Blocks) == 0 {
			return "", fmt.Errorf("model returned no content blocks for step %s", step.ID)
		}
		if err := a.record(ctx, "assistant_blocks", map[string]any{
			"phase": "step", "planId": planID, "stepId": step.ID, "turn": turn, "blocks": modelTurn.Blocks,
		}); err != nil {
			return "", err
		}
		localHistory = append(localHistory, domain.SessionMessage{Role: "assistant_blocks", Blocks: cloneBlocks(modelTurn.Blocks)})
		calls := toolCallBlocks(modelTurn.Blocks)
		if len(calls) == 0 {
			if modelTurn.Truncated {
				localHistory, err = a.appendReActContinuation(ctx, planID, step.ID, localHistory, "stream_truncated")
				if err != nil {
					return "", err
				}
				continue
			}
			output := textFromBlocks(modelTurn.Blocks)
			if strings.TrimSpace(output) == "" {
				return "", fmt.Errorf("step %s ended without tool calls or completion text", step.ID)
			}
			return output, nil
		}

		outcomes := a.executeToolCalls(ctx, step, allowedTools, calls)
		resultBlocks := make([]domain.ContentBlock, 0, len(outcomes))
		for _, outcome := range outcomes {
			state.addTool(outcome.execution, outcome.observation)
			resultBlocks = append(resultBlocks, outcome.result)
			eventType := "native_tool_succeeded"
			if !outcome.observation.OK {
				eventType = "native_tool_failed"
			}
			if err := a.record(ctx, eventType, map[string]any{"planId": planID, "stepId": step.ID, "observation": outcome.observation, "execution": outcome.execution}); err != nil {
				return "", err
			}
		}
		if err := a.record(ctx, "tool_results", map[string]any{"planId": planID, "stepId": step.ID, "blocks": resultBlocks}); err != nil {
			return "", err
		}
		localHistory = append(localHistory, domain.SessionMessage{Role: "tool_results", Blocks: cloneBlocks(resultBlocks)})
		if modelTurn.Truncated {
			localHistory, err = a.appendReActContinuation(ctx, planID, step.ID, localHistory, "stream_truncated")
			if err != nil {
				return "", err
			}
		}
	}
	return "", fmt.Errorf("step %s exceeded %d native ReAct turns", step.ID, a.maxSteps)
}

type toolCallOutcome struct {
	result      domain.ContentBlock
	observation domain.Observation
	execution   domain.ToolExecution
}

type toolExecutionPolicy struct {
	parallel bool
	lane     string
}

func (a *YourAgent) executeToolCalls(
	ctx context.Context,
	step domain.PlanStep,
	allowed []domain.ToolDescription,
	calls []domain.ContentBlock,
) []toolCallOutcome {
	outcomes := make([]toolCallOutcome, len(calls))
	for index := 0; index < len(calls); {
		policy := toolPolicy(calls[index].ToolName, allowed)
		if !policy.parallel {
			a.recordToolStart(ctx, step.ID, calls[index])
			result, observation, execution := a.executeToolCall(ctx, step, allowed, calls[index])
			outcomes[index] = toolCallOutcome{result: result, observation: observation, execution: execution}
			index++
			continue
		}
		end := index + 1
		for end < len(calls) && toolPolicy(calls[end].ToolName, allowed).parallel {
			end++
		}
		for callIndex := index; callIndex < end; callIndex++ {
			a.recordToolStart(ctx, step.ID, calls[callIndex])
		}
		a.executeParallelToolBatch(ctx, step, allowed, calls[index:end], outcomes[index:end])
		index = end
	}
	return outcomes
}

func (a *YourAgent) executeParallelToolBatch(
	ctx context.Context,
	step domain.PlanStep,
	allowed []domain.ToolDescription,
	calls []domain.ContentBlock,
	outcomes []toolCallOutcome,
) {
	lanes := make(map[string][]int)
	var order []string
	for index, call := range calls {
		policy := toolPolicy(call.ToolName, allowed)
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
				result, observation, execution := a.executeToolCall(ctx, step, allowed, calls[index])
				outcomes[index] = toolCallOutcome{result: result, observation: observation, execution: execution}
			}
		}()
	}
	wait.Wait()
}

func toolPolicy(name string, allowed []domain.ToolDescription) toolExecutionPolicy {
	for _, tool := range allowed {
		if tool.Name == name {
			return toolExecutionPolicy{parallel: tool.SupportsParallel && tool.Risk == domain.RiskRead, lane: tool.ConcurrencyGroup}
		}
	}
	return toolExecutionPolicy{}
}

func (a *YourAgent) recordToolStart(ctx context.Context, stepID string, call domain.ContentBlock) {
	args := json.RawMessage(`{}`)
	if len(call.Arguments) > 0 && json.Valid(call.Arguments) {
		args = append(json.RawMessage(nil), call.Arguments...)
	}
	_ = a.record(ctx, "native_tool_started", map[string]any{
		"stepId": stepID, "toolCallId": call.ToolCallID, "tool": call.ToolName, "args": args,
	})
}

func (a *YourAgent) executeToolCall(
	ctx context.Context,
	step domain.PlanStep,
	allowed []domain.ToolDescription,
	call domain.ContentBlock,
) (domain.ContentBlock, domain.Observation, domain.ToolExecution) {
	callID := strings.TrimSpace(call.ToolCallID)
	observation := domain.Observation{ToolCallID: callID, Tool: call.ToolName}
	if callID == "" {
		observation.Error = "provider returned a tool call without call_id"
		return toolResultBlock(callID, observation.Error, true), observation, domain.ToolExecution{}
	}
	if !toolIsAllowed(call.ToolName, allowed) {
		observation.Error = fmt.Sprintf("tool %s is not allowed for scheduled step %s", call.ToolName, step.ID)
		return toolResultBlock(callID, observation.Error, true), observation, domain.ToolExecution{}
	}
	args := map[string]any{}
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			observation.Error = "invalid native tool arguments: " + err.Error()
			return toolResultBlock(callID, observation.Error, true), observation, domain.ToolExecution{}
		}
	}
	observation.Args = args
	execution, err := a.tools.Execute(ctx, call.ToolName, args)
	observation.Result = execution.Result
	observation.OK = err == nil
	if err != nil {
		observation.Error = err.Error()
		return toolResultBlock(callID, observation.Error, true), observation, execution
	}
	output, encodeErr := json.Marshal(execution.Result)
	if encodeErr != nil {
		observation.OK = false
		observation.Error = "encode tool output: " + encodeErr.Error()
		return toolResultBlock(callID, observation.Error, true), observation, execution
	}
	return toolResultBlock(callID, string(output), false), observation, execution
}

func (a *YourAgent) prepareStepHistory(
	ctx context.Context,
	planID string,
	stepID string,
	history []domain.SessionMessage,
	force bool,
	minRecentTurns int,
	state *executionState,
) ([]domain.SessionMessage, error) {
	prepared, info := session.PrepareNativeHistory(history, force, minRecentTurns)
	if !info.Compacted && info.MicroCompactions == 0 && info.DedupeFoldedResults == 0 {
		return prepared, nil
	}
	if info.Compacted {
		state.addSessionCompaction()
	}
	if err := a.record(ctx, "session_view_compacted", map[string]any{
		"planId": planID, "stepId": stepID, "info": info,
	}); err != nil {
		return history, err
	}
	return prepared, nil
}

func (a *YourAgent) appendReActContinuation(
	ctx context.Context,
	planID string,
	stepID string,
	history []domain.SessionMessage,
	reason string,
) ([]domain.SessionMessage, error) {
	message := domain.SessionMessage{Role: "user", Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: truncationContinuation}}}
	if err := a.record(ctx, "react_user_message", map[string]any{
		"planId": planID, "stepId": stepID, "reason": reason, "text": truncationContinuation,
	}); err != nil {
		return history, err
	}
	return append(history, message), nil
}

func isContextLengthError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, pattern := range []string{
		"context_length_exceeded", "context length exceeded", "maximum context length",
		"context window", "input is too long", "input too long", "prompt is too long", "too many tokens",
	} {
		if strings.Contains(message, pattern) {
			return true
		}
	}
	return false
}

func (a *YourAgent) toolsForStep(step domain.PlanStep) ([]domain.ToolDescription, error) {
	names := append([]string(nil), step.AllowedTools...)
	if len(names) == 0 && strings.TrimSpace(step.Tool) != "" {
		names = append(names, step.Tool)
	}
	if len(names) == 0 {
		return nil, nil
	}
	descriptions := make(map[string]domain.ToolDescription)
	for _, tool := range a.tools.Descriptions() {
		descriptions[tool.Name] = tool
	}
	result := make([]domain.ToolDescription, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		tool, exists := descriptions[name]
		if !exists {
			return nil, fmt.Errorf("plan step %s references unavailable tool %q", step.ID, name)
		}
		seen[name] = struct{}{}
		result = append(result, tool)
	}
	return result, nil
}

func toolIsAllowed(name string, allowed []domain.ToolDescription) bool {
	name = strings.TrimSpace(name)
	for _, tool := range allowed {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func (a *YourAgent) compactState(ctx context.Context, goal string, state *executionState, force bool) error {
	_, recent, previous, _, _ := state.snapshot()
	summary, kept, err := a.compactIfNeeded(ctx, goal, previous, recent, force, state.metrics)
	if err != nil {
		return err
	}
	state.setCompaction(summary, kept)
	return nil
}

func (a *YourAgent) finishPlan(
	ctx context.Context,
	item planning.Plan,
	images []string,
	history []domain.SessionMessage,
	memories []domain.Memory,
	state *executionState,
	goalState *domain.GoalState,
) (Result, error) {
	var final domain.FinalResponse
	var evaluation domain.Evaluation
	for attempt := 1; attempt <= a.maxSteps; attempt++ {
		observations, _, summary, _, tokens := state.snapshot()
		if budgetExceeded(a.tokenBudget, tokens) {
			goalState.Status = domain.GoalStopped
			goalState.StopReason = "token_budget_exceeded"
			return a.stoppedResult(ctx, item.ID, state, *goalState)
		}
		var err error
		final, err = a.model.GenerateFinal(ctx, domain.FinalContext{
			Goal: item.Objective, PlanID: item.ID, Plan: item.Steps, ContextSummary: summary,
			Observations: observations, Memories: memories, Images: images, SessionHistory: history,
		})
		if err != nil {
			return Result{}, err
		}
		state.applyUsage(final.Usage)
		if len(toolCallBlocks(final.Blocks)) > 0 {
			return Result{}, errors.New("final response attempted a tool call after the scheduler completed")
		}
		if strings.TrimSpace(final.Content) == "" {
			final.Content = textFromBlocks(final.Blocks)
		}
		if reasoning := blocksOfType(final.Blocks, domain.BlockReasoning); len(reasoning) > 0 {
			if err := a.record(ctx, "assistant_blocks", map[string]any{"phase": "final", "planId": item.ID, "blocks": reasoning}); err != nil {
				return Result{}, err
			}
		}
		evaluation = a.evaluator.Evaluate(final.Content)
		if err := a.record(ctx, "report_evaluated", map[string]any{"attempt": attempt, "evaluation": evaluation}); err != nil {
			return Result{}, err
		}
		if evaluation.Passed {
			break
		}
		observation := domain.Observation{Tool: "evaluator", Result: evaluation, Error: "final report did not pass evaluation", OK: false}
		state.mu.Lock()
		state.observations = append(state.observations, observation)
		state.recent = append(state.recent, observation)
		state.mu.Unlock()
	}
	if !evaluation.Passed {
		return Result{}, errors.New("final report did not pass evaluator within retry limit")
	}

	observations, _, summary, steps, _ := state.snapshot()
	if paper := latestPaper(observations); paper != nil {
		value := paper.Module
		if strings.TrimSpace(value) == "" {
			value = paper.Title
		}
		written, err := a.memory.Remember(ctx, domain.MemoryInput{
			Key: "last_paper_topic", Value: value, Source: paper.URL, Confidence: 1, Scope: "user",
		})
		if err != nil {
			return Result{}, fmt.Errorf("write memory: %w", err)
		}
		if err := a.record(ctx, "memory_written", written); err != nil {
			return Result{}, err
		}
	}
	goalState.Status = domain.GoalAchieved
	goalState.TokensUsed = state.metrics.TotalTokens
	finishMetrics(state.metrics, a.now(), true)
	if err := a.persistMetrics(ctx, *state.metrics); err != nil {
		return Result{}, err
	}
	if err := a.record(ctx, "metrics_recorded", *state.metrics); err != nil {
		return Result{}, err
	}
	if err := a.record(ctx, "run_completed", map[string]any{"planId": item.ID, "plan": item.Steps, "evaluation": evaluation, "goal": goalState}); err != nil {
		return Result{}, err
	}
	return Result{
		Status: "completed", Report: final.Content, Plan: item.Steps, Evaluation: evaluation, Steps: steps,
		Observations: observations, ContextSummary: summary, Goal: *goalState, Metrics: *state.metrics,
	}, nil
}

func (a *YourAgent) stoppedResult(ctx context.Context, planID string, state *executionState, goal domain.GoalState) (Result, error) {
	item, err := a.planStore.Get(ctx, planID)
	if err != nil {
		return Result{}, err
	}
	observations, _, summary, steps, _ := state.snapshot()
	goal.TokensUsed = state.metrics.TotalTokens
	finishMetrics(state.metrics, a.now(), false)
	if err := a.persistMetrics(ctx, *state.metrics); err != nil {
		return Result{}, err
	}
	if err := a.record(ctx, "metrics_recorded", *state.metrics); err != nil {
		return Result{}, err
	}
	if err := a.record(ctx, "run_stopped", map[string]any{"reason": goal.StopReason, "planId": planID, "plan": item.Steps, "goal": goal}); err != nil {
		return Result{}, err
	}
	return Result{
		Status: "stopped", Reason: goal.StopReason, Plan: item.Steps, Steps: steps,
		Observations: observations, ContextSummary: summary, Goal: goal, Metrics: *state.metrics,
	}, nil
}

func (a *YourAgent) captureFailure(ctx context.Context, result *Result, retErr *error, metrics *domain.RunMetrics, goal *domain.GoalState) {
	if retErr == nil || *retErr == nil {
		return
	}
	finishMetrics(metrics, a.now(), false)
	goal.TokensUsed = metrics.TotalTokens
	result.Metrics = *metrics
	result.Goal = *goal
	logContext := ctx
	if logContext == nil || logContext.Err() != nil {
		logContext = context.Background()
	}
	if a.metricsRecorder != nil {
		_ = a.metricsRecorder.Record(logContext, *metrics)
	}
	_, _ = a.logger.Record(logContext, "run_failed", map[string]any{"error": (*retErr).Error(), "metrics": metrics})
}

func (a *YourAgent) compactIfNeeded(
	ctx context.Context,
	goal string,
	previousSummary string,
	observations []domain.Observation,
	force bool,
	metrics *domain.RunMetrics,
) (string, []domain.Observation, error) {
	keep := a.maxRecentObservations
	if force && keep > 2 {
		keep = 2
	}
	if len(observations) <= keep {
		return previousSummary, observations, nil
	}
	cut := len(observations) - keep
	toCompact := append([]domain.Observation(nil), observations[:cut]...)
	summary := ""
	fallback := false
	if compactor, ok := a.model.(ContextCompactor); ok {
		response, err := compactor.Compact(ctx, domain.CompactionContext{Goal: goal, PreviousSummary: previousSummary, Observations: toCompact})
		if err == nil {
			summary = response.Summary
			applyUsage(metrics, response.Usage)
		} else {
			fallback = true
		}
	}
	if strings.TrimSpace(summary) == "" {
		summary = deterministicSummary(previousSummary, toCompact, 4000)
		fallback = true
	}
	metrics.ContextCompactions++
	if err := a.record(ctx, "context_compacted", map[string]any{
		"observationCount": len(toCompact), "keptRecent": keep, "fallback": fallback,
	}); err != nil {
		return previousSummary, observations, err
	}
	return summary, append([]domain.Observation(nil), observations[cut:]...), nil
}

func deterministicSummary(previous string, observations []domain.Observation, limit int) string {
	payload := struct {
		Previous     string               `json:"previous,omitempty"`
		Observations []domain.Observation `json:"observations"`
	}{Previous: previous, Observations: observations}
	data, err := json.Marshal(payload)
	if err != nil {
		return truncateUTF8(previous, limit)
	}
	return truncateUTF8(string(data), limit)
}

func truncateUTF8(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) && len(value) > 0 {
		value = value[:len(value)-1]
	}
	return value
}

func applyUsage(metrics *domain.RunMetrics, usage domain.ModelUsage) {
	metrics.LLMCalls++
	metrics.InputTokens += usage.InputTokens
	metrics.OutputTokens += usage.OutputTokens
	metrics.CacheReadInputTokens += usage.CacheReadInputTokens
	metrics.CacheCreationInputTokens += usage.CacheCreationInputTokens
	if usage.TotalTokens > 0 {
		metrics.TotalTokens += usage.TotalTokens
	} else {
		metrics.TotalTokens += usage.InputTokens + usage.OutputTokens
	}
}

func finishMetrics(metrics *domain.RunMetrics, now time.Time, success bool) {
	metrics.CompletedAt = now.UTC()
	metrics.DurationMS = metrics.CompletedAt.Sub(metrics.StartedAt).Milliseconds()
	metrics.Success = success
}

func budgetExceeded(limit, used int) bool { return limit > 0 && used >= limit }

func latestPaper(observations []domain.Observation) *domain.Paper {
	for index := len(observations) - 1; index >= 0; index-- {
		if paper, ok := observations[index].Result.(domain.Paper); ok {
			copy := paper
			return &copy
		}
	}
	return nil
}

func findPlanStep(steps []domain.PlanStep, id string) *domain.PlanStep {
	for index := range steps {
		if steps[index].ID == id {
			copy := steps[index]
			return &copy
		}
	}
	return nil
}

func planProgressKey(steps []domain.PlanStep) string {
	var value strings.Builder
	for _, step := range steps {
		fmt.Fprintf(&value, "%s:%s:%d|", step.ID, step.Status, step.Attempts)
	}
	return value.String()
}

func toolCallBlocks(blocks []domain.ContentBlock) []domain.ContentBlock {
	return blocksOfType(blocks, domain.BlockToolCall)
}

func blocksOfType(blocks []domain.ContentBlock, blockType string) []domain.ContentBlock {
	var result []domain.ContentBlock
	for _, block := range blocks {
		if block.Type == blockType {
			result = append(result, block)
		}
	}
	return result
}

func textFromBlocks(blocks []domain.ContentBlock) string {
	var parts []string
	for _, block := range blocks {
		if block.Type == domain.BlockText && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func toolResultBlock(callID, output string, failed bool) domain.ContentBlock {
	return domain.ContentBlock{Type: domain.BlockToolResult, ToolCallID: callID, Output: output, IsError: failed}
}

func cloneBlocks(blocks []domain.ContentBlock) []domain.ContentBlock {
	result := make([]domain.ContentBlock, len(blocks))
	for index, block := range blocks {
		result[index] = block
		result[index].Arguments = append(json.RawMessage(nil), block.Arguments...)
		result[index].Raw = append(json.RawMessage(nil), block.Raw...)
	}
	return result
}

func (a *YourAgent) record(ctx context.Context, eventType string, payload any) error {
	if _, err := a.logger.Record(ctx, eventType, payload); err != nil {
		return fmt.Errorf("record %s: %w", eventType, err)
	}
	return nil
}

func (a *YourAgent) persistMetrics(ctx context.Context, metrics domain.RunMetrics) error {
	if a.metricsRecorder == nil {
		return nil
	}
	if err := a.metricsRecorder.Record(ctx, metrics); err != nil {
		return fmt.Errorf("persist metrics: %w", err)
	}
	return nil
}
