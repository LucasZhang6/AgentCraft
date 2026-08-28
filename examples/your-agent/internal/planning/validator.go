package planning

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
)

const (
	maxPlanSteps    = 32
	maxToolsPerStep = 16
)

var stepIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

type Validator struct{}

func (Validator) ValidateAndNormalize(plan []domain.PlanStep, tools []domain.ToolDescription) ([]domain.PlanStep, error) {
	if len(plan) == 0 {
		return nil, errors.New("plan must contain at least one step")
	}
	if len(plan) > maxPlanSteps {
		return nil, fmt.Errorf("plan contains %d steps; maximum is %d", len(plan), maxPlanSteps)
	}

	allowedTools := make(map[string]domain.ToolDescription, len(tools))
	for _, tool := range tools {
		allowedTools[tool.Name] = tool
	}

	normalized := make([]domain.PlanStep, len(plan))
	copy(normalized, plan)
	byID := make(map[string]int, len(normalized))
	for index := range normalized {
		step := &normalized[index]
		step.ID = strings.TrimSpace(step.ID)
		step.Description = strings.TrimSpace(step.Description)
		step.Tool = strings.TrimSpace(step.Tool)
		step.AllowedTools = normalizedToolNames(step.Tool, step.AllowedTools)
		step.SuccessCriteria = strings.TrimSpace(step.SuccessCriteria)
		if !stepIDPattern.MatchString(step.ID) {
			return nil, fmt.Errorf("plan step %d has invalid id %q", index+1, step.ID)
		}
		if _, exists := byID[step.ID]; exists {
			return nil, fmt.Errorf("plan step id is duplicated: %s", step.ID)
		}
		byID[step.ID] = index
		if step.Description == "" {
			return nil, fmt.Errorf("plan step %s has no description", step.ID)
		}
		if step.SuccessCriteria == "" {
			return nil, fmt.Errorf("plan step %s has no success criteria", step.ID)
		}
		if len(step.AllowedTools) > maxToolsPerStep {
			return nil, fmt.Errorf("plan step %s allows %d tools; maximum is %d", step.ID, len(step.AllowedTools), maxToolsPerStep)
		}
		for _, name := range step.AllowedTools {
			tool, exists := allowedTools[name]
			if !exists {
				return nil, fmt.Errorf("plan step %s references unavailable tool %q", step.ID, name)
			}
			if tool.Risk == domain.RiskWrite || tool.Risk == domain.RiskDangerous {
				step.RequiresApproval = true
			}
		}
		if step.Tool == "" && len(step.AllowedTools) == 1 {
			step.Tool = step.AllowedTools[0]
		}
		if step.Status == "" {
			step.Status = domain.PlanPending
		}
		if step.Status != domain.PlanPending {
			return nil, fmt.Errorf("new plan step %s must start as pending", step.ID)
		}
	}

	for _, step := range normalized {
		seenDependencies := make(map[string]struct{}, len(step.Dependencies))
		stepIndex := byID[step.ID]
		normalizedDependencies := make([]string, 0, len(step.Dependencies))
		for _, dependency := range step.Dependencies {
			dependency = strings.TrimSpace(dependency)
			if dependency == step.ID {
				return nil, fmt.Errorf("plan step %s depends on itself", step.ID)
			}
			if _, exists := byID[dependency]; !exists {
				return nil, fmt.Errorf("plan step %s depends on unknown step %q", step.ID, dependency)
			}
			if _, exists := seenDependencies[dependency]; exists {
				return nil, fmt.Errorf("plan step %s repeats dependency %q", step.ID, dependency)
			}
			seenDependencies[dependency] = struct{}{}
			normalizedDependencies = append(normalizedDependencies, dependency)
		}
		normalized[stepIndex].Dependencies = normalizedDependencies
	}

	if err := rejectCycles(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func normalizedToolNames(legacy string, names []string) []string {
	result := make([]string, 0, len(names)+1)
	seen := make(map[string]struct{}, len(names)+1)
	appendName := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, exists := seen[name]; exists {
			return
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	appendName(legacy)
	for _, name := range names {
		appendName(name)
	}
	return result
}

func rejectCycles(plan []domain.PlanStep) error {
	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[string]int, len(plan))
	dependencies := make(map[string][]string, len(plan))
	for _, step := range plan {
		dependencies[step.ID] = step.Dependencies
	}
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case visiting:
			return fmt.Errorf("plan contains a dependency cycle at %s", id)
		case visited:
			return nil
		}
		state[id] = visiting
		for _, dependency := range dependencies[id] {
			if err := visit(strings.TrimSpace(dependency)); err != nil {
				return err
			}
		}
		state[id] = visited
		return nil
	}
	for _, step := range plan {
		if err := visit(step.ID); err != nil {
			return err
		}
	}
	return nil
}
