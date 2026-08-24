package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
)

var ErrEmptyGoal = errors.New("goal must not be empty")

type Model interface {
	CreatePlan(context.Context, string, []domain.ToolDescription) (domain.PlanResponse, error)
	Decide(context.Context, domain.DecisionContext) (domain.DecisionResponse, error)
}

type ContextCompactor interface {
	Compact(context.Context, domain.CompactionContext) (domain.CompactionResponse, error)
}

type ImagePlanningModel interface {
	CreatePlanWithImages(context.Context, string, []string, []domain.ToolDescription) (domain.PlanResponse, error)
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

type PlanRecorder interface {
	Save(context.Context, string, []domain.PlanStep) error
	Update(context.Context, []domain.PlanStep) error
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

type PaperAgent struct {
	model                 Model
	tools                 ToolExecutor
	memory                MemoryStore
	plans                 PlanValidator
	planRecorder          PlanRecorder
	evaluator             Evaluator
	logger                Logger
	metricsRecorder       MetricsRecorder
	maxSteps              int
	maxGoalTurns          int
	tokenBudget           int
	maxRecentObservations int
	memoryLimit           int
	memoryBytes           int
	now                   func() time.Time
}

type Config struct {
	Model                 Model
	Tools                 ToolExecutor
	Memory                MemoryStore
	Plans                 PlanValidator
	PlanRecorder          PlanRecorder
	Evaluator             Evaluator
	Logger                Logger
	MetricsRecorder       MetricsRecorder
	MaxSteps              int
	MaxGoalTurns          int
	TokenBudget           int
	MaxRecentObservations int
	MemoryLimit           int
	MemoryBytes           int
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

func New(config Config) (*PaperAgent, error) {
	if config.Model == nil || config.Tools == nil || config.Memory == nil || config.Plans == nil || config.Evaluator == nil || config.Logger == nil {
		return nil, errors.New("model, tools, memory, plans, evaluator, and logger are required")
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
	return &PaperAgent{
		model:                 config.Model,
		tools:                 config.Tools,
		memory:                config.Memory,
		plans:                 config.Plans,
		planRecorder:          config.PlanRecorder,
		evaluator:             config.Evaluator,
		logger:                config.Logger,
		metricsRecorder:       config.MetricsRecorder,
		maxSteps:              config.MaxSteps,
		maxGoalTurns:          config.MaxGoalTurns,
		tokenBudget:           max(config.TokenBudget, 0),
		maxRecentObservations: config.MaxRecentObservations,
		memoryLimit:           config.MemoryLimit,
		memoryBytes:           config.MemoryBytes,
		now:                   time.Now,
	}, nil
}

func (a *PaperAgent) Run(ctx context.Context, goal string) (result Result, retErr error) {
	return a.RunWithImages(ctx, goal, nil)
}

func (a *PaperAgent) RunWithImages(ctx context.Context, goal string, images []string) (result Result, retErr error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return Result{}, ErrEmptyGoal
	}
	metrics := domain.RunMetrics{StartedAt: a.now().UTC()}
	goalState := domain.GoalState{
		Objective: goal, Status: domain.GoalPursuing, MaxTurns: a.maxGoalTurns, TokenBudget: a.tokenBudget,
	}
	defer func() {
		if retErr == nil {
			return
		}
		finishMetrics(&metrics, a.now(), false)
		result.Metrics = metrics
		result.Goal = goalState
		logContext := ctx
		if logContext.Err() != nil {
			logContext = context.Background()
		}
		if a.metricsRecorder != nil {
			_ = a.metricsRecorder.Record(logContext, metrics)
		}
		_, _ = a.logger.Record(logContext, "run_failed", map[string]any{"error": retErr.Error(), "metrics": metrics})
	}()

	tools := a.tools.Descriptions()
	var planResponse domain.PlanResponse
	var err error
	if len(images) > 0 {
		planner, ok := a.model.(ImagePlanningModel)
		if !ok {
			return result, errors.New("configured model does not support image input")
		}
		planResponse, err = planner.CreatePlanWithImages(ctx, goal, images, tools)
	} else {
		planResponse, err = a.model.CreatePlan(ctx, goal, tools)
	}
	if err != nil {
		return result, fmt.Errorf("create plan: %w", err)
	}
	applyUsage(&metrics, planResponse.Usage)
	plan, err := a.plans.ValidateAndNormalize(planResponse.Plan, tools)
	if err != nil {
		return result, fmt.Errorf("validate generated plan: %w", err)
	}
	memories, err := a.memory.Retrieve(ctx, domain.MemoryQuery{
		Text: goal, Scopes: []string{"user", "project", "learning-preference"},
		Limit: a.memoryLimit, LimitBytes: a.memoryBytes,
	})
	if err != nil {
		return result, fmt.Errorf("retrieve memory: %w", err)
	}
	if err := a.record(ctx, "run_started", map[string]any{
		"goal": goal, "maxSteps": a.maxSteps, "maxGoalTurns": a.maxGoalTurns, "tokenBudget": a.tokenBudget,
	}); err != nil {
		return result, err
	}
	if err := a.record(ctx, "plan_created", map[string]any{"plan": plan, "validated": true}); err != nil {
		return result, err
	}
	if a.planRecorder != nil {
		if err := a.planRecorder.Save(ctx, goal, plan); err != nil {
			return result, fmt.Errorf("persist generated plan: %w", err)
		}
	}
	if err := a.record(ctx, "memory_retrieved", map[string]any{
		"count": len(memories), "limit": a.memoryLimit, "limitBytes": a.memoryBytes, "scopes": []string{"user", "project", "learning-preference"},
	}); err != nil {
		return result, err
	}

	var allObservations []domain.Observation
	var recentObservations []domain.Observation
	contextSummary := ""
	totalSteps := 0
	for turn := 1; a.maxGoalTurns == 0 || turn <= a.maxGoalTurns; turn++ {
		goalState.Turns = turn
		metrics.GoalTurns = turn
		if err := a.record(ctx, "goal_turn_started", map[string]any{"turn": turn, "summary": contextSummary}); err != nil {
			return result, err
		}
		for step := 1; step <= a.maxSteps; step++ {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if budgetExceeded(a.tokenBudget, metrics.TotalTokens) {
				goalState.Status = domain.GoalStopped
				goalState.StopReason = "token_budget_exceeded"
				return a.stop(ctx, result, plan, allObservations, contextSummary, totalSteps, goalState, &metrics)
			}
			totalSteps++
			decisionResponse, err := a.model.Decide(ctx, domain.DecisionContext{
				Goal: goal, GoalTurn: turn, Images: images, Plan: plan, ContextSummary: contextSummary,
				Observations: recentObservations, Memories: memories, Tools: tools,
			})
			if err != nil {
				return result, fmt.Errorf("decide at goal turn %d step %d: %w", turn, step, err)
			}
			applyUsage(&metrics, decisionResponse.Usage)
			decision := decisionResponse.Decision
			if err := a.record(ctx, "decision", map[string]any{"goalTurn": turn, "step": step, "decision": decision}); err != nil {
				return result, err
			}

			switch decision.Type {
			case domain.DecisionTool:
				toolCallID := fmt.Sprintf("tool_%d_%d", turn, step)
				if planErr := validatePlannedTool(plan, decision.Tool); planErr != nil {
					observation := domain.Observation{ToolCallID: toolCallID, Tool: "plan_validator", Args: decision.Args, Error: planErr.Error(), OK: false}
					allObservations = append(allObservations, observation)
					recentObservations = append(recentObservations, observation)
					if err := a.record(ctx, "decision_rejected", map[string]any{"decision": decision, "error": planErr.Error()}); err != nil {
						return result, err
					}
					continue
				}
				if err := a.record(ctx, "tool_call", map[string]any{"toolCallId": toolCallID, "tool": decision.Tool, "args": decision.Args}); err != nil {
					return result, err
				}
				startToolStep(plan, decision.Tool)
				if err := a.persistPlan(ctx, plan); err != nil {
					return result, err
				}
				execution, toolErr := a.tools.Execute(ctx, decision.Tool, decision.Args)
				metrics.ToolCalls++
				metrics.ToolDurationMS += execution.DurationMS
				if execution.ApprovalRequested {
					metrics.HumanApprovalRequests++
				}
				observation := domain.Observation{
					ToolCallID: toolCallID, Tool: decision.Tool, Args: decision.Args, Result: execution.Result, OK: toolErr == nil,
				}
				if toolErr != nil {
					metrics.ToolFailures++
					observation.Error = toolErr.Error()
					failToolAttempt(plan, decision.Tool, toolErr.Error())
					if err := a.persistPlan(ctx, plan); err != nil {
						return result, err
					}
					allObservations = append(allObservations, observation)
					recentObservations = append(recentObservations, observation)
					if err := a.record(ctx, "tool_failed", map[string]any{"observation": observation, "execution": execution}); err != nil {
						return result, err
					}
				} else {
					allObservations = append(allObservations, observation)
					recentObservations = append(recentObservations, observation)
					completeToolStep(plan, decision.Tool)
					if err := a.persistPlan(ctx, plan); err != nil {
						return result, err
					}
					if err := a.record(ctx, "tool_succeeded", map[string]any{"observation": observation, "execution": execution}); err != nil {
						return result, err
					}
				}
				contextSummary, recentObservations, err = a.compactIfNeeded(
					ctx, goal, contextSummary, recentObservations, false, &metrics,
				)
				if err != nil {
					return result, err
				}

			case domain.DecisionFinal:
				paper := mergePaperEvidence(decision.Paper, latestPaper(allObservations))
				if paper == nil {
					return result, errors.New("final decision must include a paper or follow a successful paper read")
				}
				evaluation := a.evaluator.Evaluate(decision.Content)
				if err := a.record(ctx, "report_evaluated", evaluation); err != nil {
					return result, err
				}
				if !evaluation.Passed {
					observation := domain.Observation{Tool: "evaluator", Result: evaluation, OK: false}
					allObservations = append(allObservations, observation)
					recentObservations = append(recentObservations, observation)
					continue
				}

				finalStepCompleted := completeFinalStep(plan)
				if !finalStepCompleted && paper.URL != "" {
					skipped := skipSupersededReadSteps(plan, tools, paper.URL)
					if skipped > 0 {
						if err := a.record(ctx, "plan_replanned", map[string]any{"skippedReadSteps": skipped, "alternateEvidence": paper.URL}); err != nil {
							return result, err
						}
						finalStepCompleted = completeFinalStep(plan)
					}
				}
				if !finalStepCompleted {
					observation := domain.Observation{Tool: "plan_verifier", Error: "final plan step is blocked by unfinished dependencies", OK: false}
					allObservations = append(allObservations, observation)
					recentObservations = append(recentObservations, observation)
					if err := a.record(ctx, "decision_rejected", observation); err != nil {
						return result, err
					}
					continue
				}
				if err := a.persistPlan(ctx, plan); err != nil {
					return result, err
				}
				memoryValue := paper.Module
				if strings.TrimSpace(memoryValue) == "" {
					memoryValue = paper.Title
				}
				written, err := a.memory.Remember(ctx, domain.MemoryInput{
					Key: "last_paper_topic", Value: memoryValue, Source: paper.URL, Confidence: 1, Scope: "user",
				})
				if err != nil {
					return result, fmt.Errorf("write memory: %w", err)
				}
				if err := a.record(ctx, "memory_written", written); err != nil {
					return result, err
				}
				goalState.Status = domain.GoalAchieved
				goalState.TokensUsed = metrics.TotalTokens
				finishMetrics(&metrics, a.now(), true)
				if err := a.persistMetrics(ctx, metrics); err != nil {
					return result, err
				}
				if err := a.record(ctx, "metrics_recorded", metrics); err != nil {
					return result, err
				}
				if err := a.record(ctx, "run_completed", map[string]any{"plan": plan, "evaluation": evaluation, "goal": goalState}); err != nil {
					return result, err
				}
				return Result{
					Status: "completed", Report: decision.Content, Plan: plan, Evaluation: evaluation,
					Steps: totalSteps, Observations: allObservations, ContextSummary: contextSummary,
					Goal: goalState, Metrics: metrics,
				}, nil

			default:
				return result, fmt.Errorf("unsupported decision type: %s", decision.Type)
			}
		}

		if a.maxGoalTurns > 0 && turn == a.maxGoalTurns {
			break
		}
		var err error
		contextSummary, recentObservations, err = a.compactIfNeeded(
			ctx, goal, contextSummary, recentObservations, true, &metrics,
		)
		if err != nil {
			return result, err
		}
		if err := a.record(ctx, "goal_continued", map[string]any{
			"completedTurn": turn, "nextTurn": turn + 1, "contextSummary": contextSummary,
		}); err != nil {
			return result, err
		}
	}

	goalState.Status = domain.GoalStopped
	goalState.StopReason = "goal_turn_limit_exceeded"
	return a.stop(ctx, result, plan, allObservations, contextSummary, totalSteps, goalState, &metrics)
}

func (a *PaperAgent) stop(
	ctx context.Context,
	result Result,
	plan []domain.PlanStep,
	observations []domain.Observation,
	contextSummary string,
	steps int,
	goal domain.GoalState,
	metrics *domain.RunMetrics,
) (Result, error) {
	goal.TokensUsed = metrics.TotalTokens
	finishMetrics(metrics, a.now(), false)
	if err := a.persistMetrics(ctx, *metrics); err != nil {
		return result, err
	}
	if err := a.record(ctx, "metrics_recorded", *metrics); err != nil {
		return result, err
	}
	if err := a.record(ctx, "run_stopped", map[string]any{"reason": goal.StopReason, "plan": plan, "goal": goal}); err != nil {
		return result, err
	}
	return Result{
		Status: "stopped", Reason: goal.StopReason, Plan: plan, Steps: steps,
		Observations: observations, ContextSummary: contextSummary, Goal: goal, Metrics: *metrics,
	}, nil
}

func (a *PaperAgent) compactIfNeeded(
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
	compactionFallback := false
	if compactor, ok := a.model.(ContextCompactor); ok {
		response, err := compactor.Compact(ctx, domain.CompactionContext{
			Goal: goal, PreviousSummary: previousSummary, Observations: toCompact,
		})
		if err == nil {
			summary = response.Summary
			applyUsage(metrics, response.Usage)
		} else {
			compactionFallback = true
		}
	}
	if strings.TrimSpace(summary) == "" {
		summary = deterministicSummary(previousSummary, toCompact, 4000)
		compactionFallback = true
	}
	metrics.ContextCompactions++
	if err := a.record(ctx, "context_compacted", map[string]any{
		"observationCount": len(toCompact), "keptRecent": keep, "fallback": compactionFallback,
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

func budgetExceeded(limit, used int) bool {
	return limit > 0 && used >= limit
}

func latestPaper(observations []domain.Observation) *domain.Paper {
	for index := len(observations) - 1; index >= 0; index-- {
		if paper, ok := observations[index].Result.(domain.Paper); ok {
			copy := paper
			return &copy
		}
	}
	return nil
}

func mergePaperEvidence(modelPaper, toolPaper *domain.Paper) *domain.Paper {
	if modelPaper == nil && toolPaper == nil {
		return nil
	}
	if modelPaper == nil {
		copy := *toolPaper
		return &copy
	}
	result := *modelPaper
	if toolPaper == nil {
		return &result
	}
	if result.ID == "" {
		result.ID = toolPaper.ID
	}
	if result.Title == "" {
		result.Title = toolPaper.Title
	}
	if result.Module == "" {
		result.Module = toolPaper.Module
	}
	if len(result.Keywords) == 0 {
		result.Keywords = append([]string(nil), toolPaper.Keywords...)
	}
	if result.URL == "" {
		result.URL = toolPaper.URL
	}
	if result.Problem == "" {
		result.Problem = toolPaper.Problem
	}
	if result.Method == "" {
		result.Method = toolPaper.Method
	}
	if result.Contribution == "" {
		result.Contribution = toolPaper.Contribution
	}
	if result.Limitation == "" {
		result.Limitation = toolPaper.Limitation
	}
	if result.Engineering == "" {
		result.Engineering = toolPaper.Engineering
	}
	return &result
}

func completeToolStep(plan []domain.PlanStep, tool string) {
	for index := range plan {
		if plan[index].Tool == tool && (plan[index].Status == domain.PlanPending || plan[index].Status == domain.PlanRunning) && dependenciesComplete(plan, plan[index]) {
			plan[index].Status = domain.PlanCompleted
			plan[index].Evidence = append(plan[index].Evidence, "tool execution succeeded")
			return
		}
	}
}

func startToolStep(plan []domain.PlanStep, tool string) {
	for index := range plan {
		if plan[index].Tool == tool && plan[index].Status == domain.PlanPending && dependenciesComplete(plan, plan[index]) {
			plan[index].Status = domain.PlanRunning
			plan[index].Attempts++
			return
		}
	}
}

func failToolAttempt(plan []domain.PlanStep, tool, evidence string) {
	for index := range plan {
		if plan[index].Tool == tool && plan[index].Status == domain.PlanRunning {
			plan[index].Status = domain.PlanPending
			plan[index].Evidence = append(plan[index].Evidence, evidence)
			return
		}
	}
}

func validatePlannedTool(plan []domain.PlanStep, tool string) error {
	found := false
	for _, step := range plan {
		if step.Tool != tool || step.Status != domain.PlanPending {
			continue
		}
		found = true
		if dependenciesComplete(plan, step) {
			return nil
		}
	}
	if found {
		return fmt.Errorf("tool %s is blocked by unfinished plan dependencies", tool)
	}
	return fmt.Errorf("tool %s is not a pending action in the validated plan", tool)
}

func completeFinalStep(plan []domain.PlanStep) bool {
	for index := range plan {
		if plan[index].Tool == "" && plan[index].Status == domain.PlanPending && dependenciesComplete(plan, plan[index]) {
			plan[index].Status = domain.PlanCompleted
			plan[index].Evidence = append(plan[index].Evidence, "report evaluator passed")
			return true
		}
	}
	return false
}

func skipSupersededReadSteps(plan []domain.PlanStep, tools []domain.ToolDescription, evidence string) int {
	risks := make(map[string]string, len(tools))
	for _, tool := range tools {
		risks[tool.Name] = tool.Risk
	}
	skipped := 0
	for index := range plan {
		step := &plan[index]
		if step.Status != domain.PlanPending || step.Tool == "" {
			continue
		}
		risk := risks[step.Tool]
		if risk == domain.RiskWrite || risk == domain.RiskDangerous {
			continue
		}
		step.Status = domain.PlanSkipped
		step.Evidence = append(step.Evidence, "superseded by evaluator-accepted evidence: "+evidence)
		skipped++
	}
	return skipped
}

func dependenciesComplete(plan []domain.PlanStep, step domain.PlanStep) bool {
	for _, dependency := range step.Dependencies {
		found := false
		for _, candidate := range plan {
			if candidate.ID == dependency {
				found = candidate.Status == domain.PlanCompleted || candidate.Status == domain.PlanSkipped
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (a *PaperAgent) record(ctx context.Context, eventType string, payload any) error {
	if _, err := a.logger.Record(ctx, eventType, payload); err != nil {
		return fmt.Errorf("record %s: %w", eventType, err)
	}
	return nil
}

func (a *PaperAgent) persistMetrics(ctx context.Context, metrics domain.RunMetrics) error {
	if a.metricsRecorder == nil {
		return nil
	}
	if err := a.metricsRecorder.Record(ctx, metrics); err != nil {
		return fmt.Errorf("persist metrics: %w", err)
	}
	return nil
}

func (a *PaperAgent) persistPlan(ctx context.Context, plan []domain.PlanStep) error {
	if a.planRecorder == nil {
		return nil
	}
	if err := a.planRecorder.Update(ctx, plan); err != nil {
		return fmt.Errorf("persist plan progress: %w", err)
	}
	return nil
}
