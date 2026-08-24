package planning_test

import (
	"strings"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/planning"
)

func TestValidatorNormalizesPlanAndApproval(t *testing.T) {
	plan, err := (planning.Validator{}).ValidateAndNormalize([]domain.PlanStep{
		{ID: "read", Description: "read evidence", Tool: "reader", SuccessCriteria: "evidence exists"},
		{ID: "write", Description: "write result", Dependencies: []string{"read"}, Tool: "writer", SuccessCriteria: "file exists"},
	}, []domain.ToolDescription{
		{Name: "reader", Risk: domain.RiskRead},
		{Name: "writer", Risk: domain.RiskWrite},
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if plan[0].Status != domain.PlanPending || !plan[1].RequiresApproval {
		t.Fatalf("unexpected normalized plan: %#v", plan)
	}
}

func TestValidatorRejectsDependencyCycle(t *testing.T) {
	_, err := (planning.Validator{}).ValidateAndNormalize([]domain.PlanStep{
		{ID: "a", Description: "a", Dependencies: []string{"b"}, SuccessCriteria: "a"},
		{ID: "b", Description: "b", Dependencies: []string{"a"}, SuccessCriteria: "b"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %v, want cycle", err)
	}
}
