package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/multimodal"
)

type FeishuClient struct {
	baseURL   string
	appID     string
	appSecret string
	client    *http.Client

	mu           sync.Mutex
	tenantToken  string
	tokenExpiry  time.Time
	tokenRefresh chan struct{}
}

type FeishuMessageContent struct {
	MessageID string
	MsgType   string
	Text      string
	Deleted   bool
	Resources []MessageResource
}

type feishuMessageItem struct {
	MessageID string `json:"message_id"`
	MsgType   string `json:"msg_type"`
	Deleted   bool   `json:"deleted"`
	Body      struct {
		Content string `json:"content"`
	} `json:"body"`
}

type feishuGetMessageResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Items []feishuMessageItem `json:"items"`
	} `json:"data"`
}

func NewFeishuClient(baseURL, appID, appSecret string) *FeishuClient {
	return &FeishuClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		appID:     appID,
		appSecret: appSecret,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *FeishuClient) ReplyText(ctx context.Context, messageID, text string) error {
	content, _ := json.Marshal(map[string]string{"text": text})
	return c.reply(ctx, messageID, "text", string(content), false)
}

// SendTextToChat sends a new top-level message to a Feishu chat. It does not
// use the message reply endpoint, so the message is visible in the channel
// instead of being placed in the source message topic.
func (c *FeishuClient) SendTextToChat(ctx context.Context, chatID, text string) error {
	if chatID == "" {
		return fmt.Errorf("feishu chat ID is required")
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	body, _ := json.Marshal(map[string]string{
		"receive_id": chatID,
		"msg_type":   "text",
		"content":    string(content),
	})
	return c.sendToChat(ctx, body)
}

// SendCardToChat sends a new top-level interactive card to a Feishu chat.
// It uses the same channel-send endpoint as SendTextToChat, so the card is
// visible in the channel instead of being placed in the source message topic.
func (c *FeishuClient) SendCardToChat(ctx context.Context, chatID string, card map[string]any) error {
	if chatID == "" {
		return fmt.Errorf("feishu chat ID is required")
	}
	content, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("marshal feishu card: %w", err)
	}
	body, err := json.Marshal(map[string]string{
		"receive_id": chatID,
		"msg_type":   "interactive",
		"content":    string(content),
	})
	if err != nil {
		return fmt.Errorf("marshal feishu card message: %w", err)
	}
	return c.sendToChat(ctx, body)
}

func (c *FeishuClient) ReplyCard(ctx context.Context, messageID string, card map[string]any) error {
	content, _ := json.Marshal(card)
	return c.reply(ctx, messageID, "interactive", string(content), false)
}

func (c *FeishuClient) ReplyTextInThread(ctx context.Context, messageID, text string) error {
	content, _ := json.Marshal(map[string]string{"text": text})
	return c.reply(ctx, messageID, "text", string(content), true)
}

func (c *FeishuClient) ReplyCardInThread(ctx context.Context, messageID string, card map[string]any) error {
	content, _ := json.Marshal(card)
	return c.reply(ctx, messageID, "interactive", string(content), true)
}

func (c *FeishuClient) sendToChat(ctx context.Context, body []byte) error {
	if c.appID == "" || c.appSecret == "" {
		return fmt.Errorf("FEISHU_APP_ID and FEISHU_APP_SECRET are required")
	}
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	endpoint := c.baseURL + "/open-apis/im/v1/messages?receive_id_type=chat_id"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	return readFeishuAPIResult(resp, err, "feishu chat message send")
}

// UpdateCardByCallbackToken updates the card that produced a card.action.trigger
// callback. The callback token is short-lived, so callers should use it soon
// after the action is received.
func (c *FeishuClient) UpdateCardByCallbackToken(ctx context.Context, callbackToken string, card map[string]any) error {
	if callbackToken == "" {
		return fmt.Errorf("feishu card callback token is required")
	}
	if c.appID == "" || c.appSecret == "" {
		return fmt.Errorf("FEISHU_APP_ID and FEISHU_APP_SECRET are required")
	}
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"token": callbackToken, "card": card})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/open-apis/interactive/v1/card/update", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	return readFeishuAPIResult(resp, err, "feishu card update")
}

// UpdateCard is a fallback for callbacks that contain the card message ID but
// do not include a usable callback token.
func (c *FeishuClient) UpdateCard(ctx context.Context, messageID string, card map[string]any) error {
	if messageID == "" {
		return fmt.Errorf("feishu card message ID is required")
	}
	if c.appID == "" || c.appSecret == "" {
		return fmt.Errorf("FEISHU_APP_ID and FEISHU_APP_SECRET are required")
	}
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	content, err := json.Marshal(card)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{"content": string(content)})
	if err != nil {
		return err
	}
	endpoint := c.baseURL + "/open-apis/im/v1/messages/" + url.PathEscape(messageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	return readFeishuAPIResult(resp, err, "feishu message card update")
}

func (c *FeishuClient) GetMessageText(ctx context.Context, messageID string) (FeishuMessageContent, error) {
	if c.appID == "" || c.appSecret == "" {
		return FeishuMessageContent{}, fmt.Errorf("FEISHU_APP_ID and FEISHU_APP_SECRET are required")
	}
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return FeishuMessageContent{}, err
	}
	// Keep the legacy response path intact. Newer CardKit messages may expose
	// only a title there, in which case userDSL returns the visible card body.
	item, err := c.getMessage(ctx, token, messageID, "")
	if err != nil {
		return FeishuMessageContent{}, err
	}
	if strings.EqualFold(item.MsgType, "interactive") && !hasInteractiveCardElements(item.Body.Content) {
		if fullItem, fullErr := c.getMessage(ctx, token, messageID, "user"); fullErr == nil {
			item = fullItem
		}
	}
	if item.MessageID == "" {
		item.MessageID = messageID
	}

	text, resources := extractMessageContent(item.MsgType, item.Body.Content)
	if item.Deleted {
		text = "[引用消息已撤回或删除]"
		resources = nil
	} else if text == "" {
		if len(resources) > 0 {
			text = "[图片消息]"
		} else {
			text = "[非文本消息: " + item.MsgType + "]"
		}
	}
	return FeishuMessageContent{
		MessageID: item.MessageID,
		MsgType:   item.MsgType,
		Text:      text,
		Deleted:   item.Deleted,
		Resources: resources,
	}, nil
}

// DownloadMessageImage downloads an image embedded in a user message. Feishu
// exposes these through the message-resource API rather than the generic image
// API used for assets uploaded by the bot itself.
func (c *FeishuClient) DownloadMessageImage(ctx context.Context, messageID, imageKey string) (string, error) {
	if c.appID == "" || c.appSecret == "" {
		return "", fmt.Errorf("FEISHU_APP_ID and FEISHU_APP_SECRET are required")
	}
	if strings.TrimSpace(messageID) == "" || strings.TrimSpace(imageKey) == "" {
		return "", fmt.Errorf("feishu message ID and image key are required")
	}
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return "", err
	}
	endpoint := c.baseURL + "/open-apis/im/v1/messages/" + url.PathEscape(messageID) +
		"/resources/" + url.PathEscape(imageKey) + "?type=image"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("feishu image download returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, multimodal.DefaultMaxImageBytes+1))
	if err != nil {
		return "", err
	}
	image, err := multimodal.FromBytes(data, resp.Header.Get("Content-Type"), multimodal.DefaultMaxImageBytes)
	if err != nil {
		return "", fmt.Errorf("invalid feishu image resource: %w", err)
	}
	return image.DataURL, nil
}

func (c *FeishuClient) getMessage(ctx context.Context, token, messageID, cardContentType string) (feishuMessageItem, error) {
	endpoint := c.baseURL + "/open-apis/im/v1/messages/" + url.PathEscape(messageID)
	if cardContentType != "" {
		endpoint += "?" + url.Values{"card_content_type": []string{cardContentType}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return feishuMessageItem{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.client.Do(req)
	if err != nil {
		return feishuMessageItem{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return feishuMessageItem{}, fmt.Errorf("feishu get message returned %s", resp.Status)
	}
	var out feishuGetMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return feishuMessageItem{}, err
	}
	if out.Code != 0 {
		return feishuMessageItem{}, fmt.Errorf("feishu get message failed: code=%d msg=%s", out.Code, out.Msg)
	}
	if len(out.Data.Items) == 0 {
		return feishuMessageItem{}, fmt.Errorf("feishu get message returned no items")
	}
	return out.Data.Items[0], nil
}

func hasInteractiveCardElements(content string) bool {
	var payload any
	if json.Unmarshal([]byte(content), &payload) != nil {
		return true
	}
	return containsCardElements(payload)
}

func containsCardElements(value any) bool {
	switch node := value.(type) {
	case []any:
		return len(node) > 0
	case map[string]any:
		for _, key := range []string{"elements", "i18n_elements"} {
			if child, ok := node[key]; ok && containsCardElements(child) {
				return true
			}
		}
		for _, key := range []string{"body", "card", "data"} {
			child, ok := node[key]
			if !ok {
				continue
			}
			if encoded, ok := child.(string); ok {
				var nested any
				if json.Unmarshal([]byte(encoded), &nested) == nil && containsCardElements(nested) {
					return true
				}
				continue
			}
			if containsCardElements(child) {
				return true
			}
		}
	}
	return false
}

func (c *FeishuClient) reply(ctx context.Context, messageID, msgType, content string, inThread bool) error {
	if c.appID == "" || c.appSecret == "" {
		return fmt.Errorf("FEISHU_APP_ID and FEISHU_APP_SECRET are required")
	}
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	bodyPayload := map[string]any{
		"msg_type": msgType,
		"content":  content,
	}
	if inThread {
		bodyPayload["reply_in_thread"] = true
	}
	body, _ := json.Marshal(bodyPayload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/open-apis/im/v1/messages/"+messageID+"/reply", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errorBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("feishu reply returned %s: %s", resp.Status, strings.TrimSpace(string(errorBody)))
	}
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if out.Code != 0 {
		return fmt.Errorf("feishu reply failed: code=%d msg=%s", out.Code, out.Msg)
	}
	return nil
}

func readFeishuAPIResult(resp *http.Response, err error, operation string) error {
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("%s returned an empty response", operation)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %s: %s", operation, resp.Status, strings.TrimSpace(string(body)))
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return err
	}
	if out.Code != 0 {
		return fmt.Errorf("%s failed: code=%d msg=%s", operation, out.Code, out.Msg)
	}
	return nil
}

func (c *FeishuClient) tenantAccessToken(ctx context.Context) (string, error) {
	for {
		c.mu.Lock()
		if c.tenantToken != "" && time.Now().Before(c.tokenExpiry.Add(-time.Minute)) {
			token := c.tenantToken
			c.mu.Unlock()
			return token, nil
		}
		if refresh := c.tokenRefresh; refresh != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-refresh:
				// The leader completed (successfully or not). Re-check the cache;
				// if it is still empty this caller becomes the next leader.
				continue
			}
		}
		refresh := make(chan struct{})
		c.tokenRefresh = refresh
		c.mu.Unlock()

		token, expiry, err := c.fetchTenantAccessToken(ctx)
		c.mu.Lock()
		if err == nil {
			c.tenantToken = token
			c.tokenExpiry = expiry
		}
		c.tokenRefresh = nil
		close(refresh)
		c.mu.Unlock()
		return token, err
	}
}

func (c *FeishuClient) fetchTenantAccessToken(ctx context.Context) (string, time.Time, error) {
	body, _ := json.Marshal(map[string]string{
		"app_id":     c.appID,
		"app_secret": c.appSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("feishu tenant token returned %s", resp.Status)
	}
	var out struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", time.Time{}, err
	}
	if out.Code != 0 || out.TenantAccessToken == "" {
		return "", time.Time{}, fmt.Errorf("feishu tenant token failed: code=%d msg=%s", out.Code, out.Msg)
	}
	expires := time.Now().Add(time.Duration(out.Expire) * time.Second)
	if out.Expire == 0 {
		expires = time.Now().Add(90 * time.Minute)
	}
	return out.TenantAccessToken, expires, nil
}

func ApprovalCard(pa PendingApproval, taskID string) map[string]any {
	params := pa.ParamsPreview
	if params == "" {
		params = "(no params preview)"
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true, "update_multi": true},
		"header": map[string]any{
			"template": "orange",
			"title":    map[string]string{"tag": "plain_text", "content": "PaperAgent 工具审批"},
		},
		"elements": []any{
			map[string]string{
				"tag":     "markdown",
				"content": fmt.Sprintf("**工具**：%s\n\n**参数**：\n```text\n%s\n```", pa.ToolName, params),
			},
			map[string]any{
				"tag": "action",
				"actions": []any{
					map[string]any{
						"tag":  "button",
						"type": "primary",
						"text": map[string]string{"tag": "plain_text", "content": "批准"},
						"value": map[string]string{
							"action":         "approve",
							"task_id":        taskID,
							"tool_id":        pa.ToolID,
							"tool_name":      pa.ToolName,
							"params_preview": params,
						},
					},
					map[string]any{
						"tag":  "button",
						"type": "default",
						"text": map[string]string{"tag": "plain_text", "content": "本任务后续均批准"},
						"value": map[string]string{
							"action":         "approve_all",
							"task_id":        taskID,
							"tool_id":        pa.ToolID,
							"tool_name":      pa.ToolName,
							"params_preview": params,
						},
					},
					map[string]any{
						"tag":  "button",
						"type": "danger",
						"text": map[string]string{"tag": "plain_text", "content": "拒绝"},
						"value": map[string]string{
							"action":         "reject",
							"task_id":        taskID,
							"tool_id":        pa.ToolID,
							"tool_name":      pa.ToolName,
							"params_preview": params,
						},
					},
				},
			},
		},
	}
}

func ApprovalResultCard(action ActionEvent, approved bool) map[string]any {
	result := "已拒绝"
	template := "red"
	if approved {
		result = "已批准"
		template = "green"
	}
	if action.Action == "approve_all" {
		result = "已批准本任务后续操作"
	}
	toolName := action.ToolName
	if toolName == "" {
		toolName = action.ToolID
	}
	params := action.ParamsPreview
	if params == "" {
		params = "(no params preview)"
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true, "update_multi": true},
		"header": map[string]any{
			"template": template,
			"title":    map[string]string{"tag": "plain_text", "content": "PaperAgent 工具审批 · " + result},
		},
		"elements": []any{
			map[string]string{
				"tag":     "markdown",
				"content": fmt.Sprintf("**审批结果**：%s\n\n**工具**：%s\n\n**参数**：\n```text\n%s\n```", result, toolName, params),
			},
			map[string]any{
				"tag": "action",
				"actions": []any{
					map[string]any{
						"tag":      "button",
						"type":     "primary",
						"disabled": true,
						"text":     map[string]string{"tag": "plain_text", "content": "批准"},
					},
					map[string]any{
						"tag":      "button",
						"type":     "default",
						"disabled": true,
						"text":     map[string]string{"tag": "plain_text", "content": "本任务后续均批准"},
					},
					map[string]any{
						"tag":      "button",
						"type":     "danger",
						"disabled": true,
						"text":     map[string]string{"tag": "plain_text", "content": "拒绝"},
					},
				},
			},
		},
	}
}
