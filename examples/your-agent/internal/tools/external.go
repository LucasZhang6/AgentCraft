package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/plugin"
)

func RegisterExternalTools(ctx context.Context, registry *Registry, cwd string) ([]string, error) {
	discovered, _, warnings, err := plugin.Tools(ctx, cwd)
	if err != nil {
		return warnings, err
	}
	for _, item := range discovered {
		item := item
		definition := Definition{Name: item.Name, Description: item.Description, Risk: domain.RiskDangerous, InputSchema: item.InputSchema}
		definition.Execute = func(ctx context.Context, args map[string]any) (any, error) {
			encoded, err := json.Marshal(args)
			if err != nil {
				return nil, err
			}
			switch item.Kind {
			case "command":
				return plugin.ExecuteCommand(ctx, item.Plugin, encoded, cwd)
			case "mcp":
				return plugin.CallMCPTool(ctx, item.MCPServer, item.MCPToolName, encoded)
			default:
				return nil, fmt.Errorf("unsupported external tool kind %q", item.Kind)
			}
		}
		if err := registry.Register(definition); err != nil {
			return warnings, err
		}
	}
	return warnings, nil
}
