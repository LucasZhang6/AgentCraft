package tools_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/tools"
)

func TestRegistryRejectsToolsAndArgumentsOutsideAllowlist(t *testing.T) {
	registry := tools.NewRegistry()
	err := registry.Register(tools.Definition{
		Name: "safe_tool", Description: "A test tool", Risk: "read-only",
		Schema: domain.ToolSchema{
			Type:       "object",
			Required:   []string{"query"},
			Properties: map[string]domain.ToolField{"query": {Type: "string"}},
		},
		Execute: func(_ context.Context, args map[string]any) (any, error) {
			return args["query"], nil
		},
	})
	if err != nil {
		t.Fatalf("register tool: %v", err)
	}

	cases := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{name: "unknown tool", tool: "shell", args: map[string]any{}, want: "not allowlisted"},
		{name: "unknown argument", tool: "safe_tool", args: map[string]any{"query": "ok", "extra": true}, want: "unknown tool argument"},
		{name: "wrong type", tool: "safe_tool", args: map[string]any{"query": 42}, want: "must be a string"},
		{name: "missing argument", tool: "safe_tool", args: map[string]any{}, want: "missing required"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := registry.Execute(context.Background(), test.tool, test.args)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestRegistryRequiresApprovalForWriteTools(t *testing.T) {
	registry := tools.NewRegistry(tools.RegistryOptions{
		Approval: func(_ context.Context, request tools.ApprovalRequest) (bool, error) {
			if request.Risk != domain.RiskWrite {
				t.Fatalf("risk = %q", request.Risk)
			}
			return false, nil
		},
	})
	if err := registry.Register(tools.Definition{
		Name: "write_note", Risk: domain.RiskWrite,
		Schema:  domain.ToolSchema{Type: "object", Properties: map[string]domain.ToolField{}},
		Execute: func(context.Context, map[string]any) (any, error) { return "written", nil },
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	execution, err := registry.Execute(context.Background(), "write_note", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("error = %v", err)
	}
	if !execution.ApprovalRequested {
		t.Fatal("approval request was not recorded")
	}
}

func TestRegistryEnforcesTimeoutAndOutputLimit(t *testing.T) {
	registry := tools.NewRegistry(tools.RegistryOptions{DefaultTimeout: 10 * time.Millisecond, MaxOutputBytes: 8})
	if err := registry.Register(tools.Definition{
		Name: "slow", Risk: domain.RiskRead,
		Schema: domain.ToolSchema{Type: "object", Properties: map[string]domain.ToolField{}},
		Execute: func(ctx context.Context, _ map[string]any) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}); err != nil {
		t.Fatalf("register slow: %v", err)
	}
	if _, err := registry.Execute(context.Background(), "slow", map[string]any{}); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}

	if err := registry.Register(tools.Definition{
		Name: "large", Risk: domain.RiskRead,
		Schema:  domain.ToolSchema{Type: "object", Properties: map[string]domain.ToolField{}},
		Execute: func(context.Context, map[string]any) (any, error) { return "0123456789abcdef", nil },
	}); err != nil {
		t.Fatalf("register large: %v", err)
	}
	execution, err := registry.Execute(context.Background(), "large", map[string]any{})
	if err != nil {
		t.Fatalf("execute large: %v", err)
	}
	if !execution.Truncated || len(execution.Result.(string)) > 8 {
		t.Fatalf("execution = %#v", execution)
	}
}

func TestRegistryExecutesValidAllowlistedTool(t *testing.T) {
	registry := tools.NewRegistry()
	err := registry.Register(tools.Definition{
		Name: "echo", Description: "Echo input", Risk: "read-only",
		Schema: domain.ToolSchema{
			Type:       "object",
			Required:   []string{"value"},
			Properties: map[string]domain.ToolField{"value": {Type: "string"}},
		},
		Execute: func(_ context.Context, args map[string]any) (any, error) { return args["value"], nil },
	})
	if err != nil {
		t.Fatalf("register tool: %v", err)
	}
	result, err := registry.Execute(context.Background(), "echo", map[string]any{"value": "ok"})
	if err != nil {
		t.Fatalf("execute tool: %v", err)
	}
	if result.Result != "ok" {
		t.Fatalf("result = %#v, want ok", result.Result)
	}
}
