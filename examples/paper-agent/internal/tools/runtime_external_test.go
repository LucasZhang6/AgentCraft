package tools_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/tools"
)

func TestRuntimeFileToolsHonorApprovalAndWorkspaceBoundary(t *testing.T) {
	root := t.TempDir()
	approvals := 0
	registry := tools.NewRegistry(tools.RegistryOptions{Approval: func(context.Context, tools.ApprovalRequest) (bool, error) {
		approvals++
		return true, nil
	}})
	if err := tools.RegisterRuntimeTools(registry, tools.RuntimeOptions{WorkDir: root}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), "file_write", map[string]any{"path": "notes/a.txt", "content": "evidence"}); err != nil {
		t.Fatal(err)
	}
	read, err := registry.Execute(context.Background(), "file_read", map[string]any{"path": "notes/a.txt"})
	if err != nil || read.Result != "evidence" {
		t.Fatalf("read=%#v err=%v", read, err)
	}
	if approvals != 1 {
		t.Fatalf("approvals=%d, want 1", approvals)
	}
	if _, err := registry.Execute(context.Background(), "file_read", map[string]any{"path": "../outside.txt"}); err == nil || !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("escape error=%v", err)
	}
	want := map[string]bool{"file_read": false, "file_write": false, "file_edit": false, "list_dir": false, "glob": false, "grep": false, "bash": false, "web_fetch": false, "web_search": false, "clarification": false, "subagent": false}
	for _, description := range registry.Descriptions() {
		if _, ok := want[description.Name]; ok {
			want[description.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("runtime tool %s is missing", name)
		}
	}
}

func TestPluginAndMCPToolsAreDiscoveredAndExecuted(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".paper-agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"version": 1,
		"plugins": []any{map[string]any{
			"name": "echo", "description": "echo JSON", "command": []string{os.Args[0], "-test.run=TestExternalToolHelper", "--", "command"},
			"env": map[string]string{"PAPER_AGENT_HELPER": "1"}, "input_schema": map[string]any{"type": "object", "properties": map[string]any{"value": map[string]string{"type": "string"}}, "required": []string{"value"}},
		}},
		"mcp_servers": []any{map[string]any{
			"name": "local", "command": []string{os.Args[0], "-test.run=TestExternalToolHelper", "--", "mcp"}, "env": map[string]string{"PAPER_AGENT_HELPER": "1"},
		}},
	}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "plugins.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	registry := tools.NewRegistry(tools.RegistryOptions{Approval: func(context.Context, tools.ApprovalRequest) (bool, error) { return true, nil }})
	if warnings, err := tools.RegisterExternalTools(context.Background(), registry, root); err != nil || len(warnings) != 0 {
		t.Fatalf("register warnings=%v err=%v", warnings, err)
	}
	command, err := registry.Execute(context.Background(), "plugin__cmd_echo__echo", map[string]any{"value": "works"})
	if err != nil || !strings.Contains(command.Result.(string), "works") {
		t.Fatalf("command=%#v err=%v", command, err)
	}
	mcp, err := registry.Execute(context.Background(), "plugin__mcp_local__echo", map[string]any{"value": "mcp works"})
	if err != nil || !strings.Contains(mcp.Result.(string), "mcp works") {
		t.Fatalf("mcp=%#v err=%v", mcp, err)
	}
}

func TestExternalToolHelper(t *testing.T) {
	if os.Getenv("PAPER_AGENT_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "command" {
		_, _ = io.Copy(os.Stdout, os.Stdin)
		os.Exit(0)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.ID == 0 {
			continue
		}
		var result any = map[string]any{}
		switch request.Method {
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "echo", "description": "echo", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"value": map[string]string{"type": "string"}}}}}}
		case "tools/call":
			arguments, _ := request.Params["arguments"].(map[string]any)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": fmt.Sprint(arguments["value"])}}}
		}
		response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		fmt.Println(string(response))
	}
	os.Exit(0)
}
