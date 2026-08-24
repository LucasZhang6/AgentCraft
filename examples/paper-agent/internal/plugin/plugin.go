package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const ToolPrefix = "plugin__"

type Manifest struct {
	Version int               `json:"version"`
	Plugins []CommandPlugin   `json:"plugins,omitempty"`
	MCP     []MCPServerConfig `json:"mcp_servers,omitempty"`
}

type CommandPlugin struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Command     []string          `json:"command"`
	InputSchema json.RawMessage   `json:"input_schema,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	TimeoutSec  int               `json:"timeout_seconds,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
}

type MCPServerConfig struct {
	Name       string            `json:"name"`
	Command    []string          `json:"command"`
	Env        map[string]string `json:"env,omitempty"`
	TimeoutSec int               `json:"timeout_seconds,omitempty"`
	Enabled    *bool             `json:"enabled,omitempty"`
}

type Tool struct {
	Name, Description, Kind, Source, MCPToolName string
	InputSchema                                  json.RawMessage
	Plugin                                       *CommandPlugin
	MCPServer                                    *MCPServerConfig
}

func (plugin CommandPlugin) IsEnabled() bool   { return plugin.Enabled == nil || *plugin.Enabled }
func (server MCPServerConfig) IsEnabled() bool { return server.Enabled == nil || *server.Enabled }

func userManifestPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "paper-agent", "plugins.json")
	}
	return ""
}

func projectManifestPath(cwd string) string {
	return filepath.Join(cwd, ".paper-agent", "plugins.json")
}

func LoadAll(cwd string) (*Manifest, []string, error) {
	if strings.TrimSpace(cwd) == "" {
		cwd, _ = os.Getwd()
	}
	merged := &Manifest{Version: 1}
	var loaded []string
	for _, path := range []string{userManifestPath(), projectManifestPath(cwd)} {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, loaded, err
		}
		var manifest Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, loaded, fmt.Errorf("parse %s: %w", path, err)
		}
		merged.Plugins = append(merged.Plugins, manifest.Plugins...)
		merged.MCP = append(merged.MCP, manifest.MCP...)
		loaded = append(loaded, path)
	}
	if err := Validate(merged); err != nil {
		return nil, loaded, err
	}
	return merged, loaded, nil
}

func Validate(manifest *Manifest) error {
	if manifest == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, item := range manifest.Plugins {
		if !validName(item.Name) || len(item.Command) == 0 || strings.TrimSpace(item.Command[0]) == "" {
			return fmt.Errorf("invalid command plugin %q", item.Name)
		}
		if seen["plugin/"+item.Name] {
			return fmt.Errorf("duplicate command plugin %q", item.Name)
		}
		seen["plugin/"+item.Name] = true
		if len(item.InputSchema) > 0 && !json.Valid(item.InputSchema) {
			return fmt.Errorf("plugin %q has invalid input_schema", item.Name)
		}
	}
	for _, item := range manifest.MCP {
		if !validName(item.Name) || len(item.Command) == 0 || strings.TrimSpace(item.Command[0]) == "" {
			return fmt.Errorf("invalid MCP server %q", item.Name)
		}
		if seen["mcp/"+item.Name] {
			return fmt.Errorf("duplicate MCP server %q", item.Name)
		}
		seen["mcp/"+item.Name] = true
	}
	return nil
}

func validName(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func safeToolName(source, name string) string {
	return ToolPrefix + sanitize(source) + "__" + sanitize(name)
}
func sanitize(value string) string {
	var out strings.Builder
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			out.WriteRune(char)
		} else {
			out.WriteByte('_')
		}
	}
	return out.String()
}

func Tools(ctx context.Context, cwd string) ([]Tool, []string, []string, error) {
	manifest, paths, err := LoadAll(cwd)
	if err != nil {
		return nil, paths, nil, err
	}
	var result []Tool
	var warnings []string
	for index := range manifest.Plugins {
		item := manifest.Plugins[index]
		if !item.IsEnabled() {
			continue
		}
		schema := item.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		copy := item
		result = append(result, Tool{Name: safeToolName("cmd_"+item.Name, item.Name), Description: item.Description, Kind: "command", Source: item.Name, InputSchema: schema, Plugin: &copy})
	}
	for index := range manifest.MCP {
		server := manifest.MCP[index]
		if !server.IsEnabled() {
			continue
		}
		mcpTools, err := ListMCPTools(ctx, &server)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s tools/list failed: %v", server.Name, err))
			continue
		}
		for _, item := range mcpTools {
			schema := item.InputSchema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			copy := server
			result = append(result, Tool{Name: safeToolName("mcp_"+server.Name, item.Name), Description: item.Description, Kind: "mcp", Source: server.Name, MCPToolName: item.Name, InputSchema: schema, MCPServer: &copy})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, paths, warnings, nil
}

func ExecuteCommand(ctx context.Context, item *CommandPlugin, args json.RawMessage, cwd string) (string, error) {
	if item == nil || len(item.Command) == 0 {
		return "", errors.New("command plugin is not configured")
	}
	timeout := time.Duration(item.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	toolCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(toolCtx, item.Command[0], item.Command[1:]...)
	command.Dir, command.Env, command.Stdin = cwd, os.Environ(), bytes.NewReader(args)
	for key, value := range item.Env {
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.CombinedOutput()
	if toolCtx.Err() != nil {
		return string(output), fmt.Errorf("plugin timeout after %s: %w", timeout, toolCtx.Err())
	}
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}

type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}
type rpcResponse struct {
	ID     int             `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type stdioClient struct {
	command *exec.Cmd
	input   io.WriteCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
	next    int
}

func newStdioClient(ctx context.Context, server *MCPServerConfig) (*stdioClient, error) {
	if server == nil || len(server.Command) == 0 {
		return nil, errors.New("MCP command is required")
	}
	command := exec.CommandContext(ctx, server.Command[0], server.Command[1:]...)
	command.Env, command.Stderr = os.Environ(), io.Discard
	for key, value := range server.Env {
		command.Env = append(command.Env, key+"="+value)
	}
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	return &stdioClient{command: command, input: input, scanner: scanner, next: 1}, nil
}

func (client *stdioClient) close() {
	_ = client.input.Close()
	if client.command.Process != nil {
		_ = client.command.Process.Kill()
	}
	_ = client.command.Wait()
}

func (client *stdioClient) call(method string, params, target any) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	id := client.next
	client.next++
	data, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if _, err := client.input.Write(append(data, '\n')); err != nil {
		return err
	}
	for client.scanner.Scan() {
		line := bytes.TrimSpace(client.scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var response rpcResponse
		if json.Unmarshal(line, &response) != nil || response.ID != id {
			continue
		}
		if response.Error != nil {
			return fmt.Errorf("MCP %s error %d: %s", method, response.Error.Code, response.Error.Message)
		}
		if target != nil {
			return json.Unmarshal(response.Result, target)
		}
		return nil
	}
	if err := client.scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

func withMCPClient(ctx context.Context, server *MCPServerConfig, run func(*stdioClient) error) error {
	timeout := time.Duration(server.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	toolCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client, err := newStdioClient(toolCtx, server)
	if err != nil {
		return err
	}
	defer client.close()
	var initialized any
	if err := client.call("initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "paper-agent", "version": "1"}}, &initialized); err != nil {
		return fmt.Errorf("MCP initialize: %w", err)
	}
	notification, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"})
	_, _ = client.input.Write(append(notification, '\n'))
	return run(client)
}

func ListMCPTools(ctx context.Context, server *MCPServerConfig) ([]MCPTool, error) {
	var result struct {
		Tools []MCPTool `json:"tools"`
	}
	err := withMCPClient(ctx, server, func(client *stdioClient) error { return client.call("tools/list", map[string]any{}, &result) })
	return result.Tools, err
}

func CallMCPTool(ctx context.Context, server *MCPServerConfig, name string, args json.RawMessage) (string, error) {
	var arguments any = map[string]any{}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &arguments)
	}
	var result json.RawMessage
	err := withMCPClient(ctx, server, func(client *stdioClient) error {
		return client.call("tools/call", map[string]any{"name": name, "arguments": arguments}, &result)
	})
	if err != nil {
		return "", err
	}
	return formatMCPResult(result), nil
}

func formatMCPResult(raw json.RawMessage) string {
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(raw, &result) == nil {
		var parts []string
		for _, content := range result.Content {
			if content.Text != "" {
				parts = append(parts, content.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return string(raw)
}
