package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type PaperAgentClient struct {
	baseURL  string
	accessID string
	client   *http.Client

	mu    sync.Mutex
	token string
}

type ExecuteRequest struct {
	Message     string   `json:"message"`
	Images      []string `json:"images,omitempty"`
	Mode        string   `json:"mode,omitempty"`
	AutoApprove bool     `json:"auto_approve"`
	Async       bool     `json:"async"`
	SessionID   string   `json:"session_id,omitempty"`
}

type ExecuteResponse struct {
	Success   bool     `json:"success"`
	TaskID    string   `json:"task_id"`
	SessionID string   `json:"session_id"`
	Result    string   `json:"result"`
	Messages  []string `json:"messages"`
	Error     string   `json:"error"`
}

type PendingApproval struct {
	ToolID        string `json:"tool_id"`
	ToolName      string `json:"tool_name"`
	AgentName     string `json:"agent_name"`
	ParamsPreview string `json:"params_preview"`
}

type StatusResponse struct {
	TaskID          string           `json:"task_id"`
	Status          string           `json:"status"`
	Messages        []string         `json:"messages"`
	Complete        bool             `json:"complete"`
	Success         bool             `json:"success"`
	Error           string           `json:"error"`
	Result          string           `json:"result"`
	PendingApproval *PendingApproval `json:"pending_approval,omitempty"`
}

func NewPaperAgentClient(baseURL, accessID string) *PaperAgentClient {
	return &PaperAgentClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		accessID: accessID,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *PaperAgentClient) Execute(ctx context.Context, req ExecuteRequest) (ExecuteResponse, error) {
	var out ExecuteResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/agent/execute", req, &out)
	if err != nil {
		return out, err
	}
	if !out.Success && out.Error != "" {
		return out, errors.New(out.Error)
	}
	return out, nil
}

func (c *PaperAgentClient) Status(ctx context.Context, taskID string) (StatusResponse, error) {
	var out StatusResponse
	err := c.doJSON(ctx, http.MethodGet, "/api/agent/status?task_id="+taskID, nil, &out)
	return out, err
}

func (c *PaperAgentClient) Approve(ctx context.Context, taskID, toolID string, approved bool, output string) error {
	return c.approve(ctx, taskID, toolID, approved, false, output)
}

func (c *PaperAgentClient) ApproveAll(ctx context.Context, taskID, toolID, output string) error {
	return c.approve(ctx, taskID, toolID, true, true, output)
}

func (c *PaperAgentClient) approve(ctx context.Context, taskID, toolID string, approved, approveAll bool, output string) error {
	body := map[string]any{
		"task_id":     taskID,
		"tool_id":     toolID,
		"approved":    approved,
		"approve_all": approveAll,
		"output":      output,
	}
	return c.doJSON(ctx, http.MethodPost, "/api/agent/approve", body, nil)
}

func (c *PaperAgentClient) Cancel(ctx context.Context, taskID string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/agent/cancel?task_id="+taskID, nil, nil)
}

func (c *PaperAgentClient) doJSON(ctx context.Context, method, path string, in, out any) error {
	return c.doJSONRetry(ctx, method, path, in, out, true)
}

func (c *PaperAgentClient) doJSONRetry(ctx context.Context, method, path string, in, out any, retryAuth bool) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	var body *bytes.Reader
	if in == nil {
		body = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := c.currentToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized && c.accessID != "" && retryAuth {
		c.clearToken()
		return c.doJSONRetry(ctx, method, path, in, out, false)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Paper Agent HTTP %s %s returned %s", method, path, resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *PaperAgentClient) ensureAuth(ctx context.Context) error {
	if c.accessID == "" || c.currentToken() != "" {
		return nil
	}
	body, _ := json.Marshal(map[string]string{"access_id": c.accessID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Paper Agent auth returned %s", resp.Status)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if out.Token == "" {
		return fmt.Errorf("Paper Agent auth response missing token")
	}
	c.mu.Lock()
	c.token = out.Token
	c.mu.Unlock()
	return nil
}

func (c *PaperAgentClient) currentToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

func (c *PaperAgentClient) clearToken() {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}
