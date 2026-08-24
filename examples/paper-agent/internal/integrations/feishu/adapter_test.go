package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdapterCloseCancelsAndWaitsForPolling(t *testing.T) {
	started := make(chan struct{})
	var startedOnce sync.Once
	paperAgentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/execute":
			_ = json.NewEncoder(w).Encode(ExecuteResponse{Success: true, TaskID: "task-close", SessionID: "session-close"})
		case "/api/agent/status":
			startedOnce.Do(func() { close(started) })
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer paperAgentSrv.Close()
	feishuSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "tenant_access_token": "token", "expire": 3600})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
	}))
	defer feishuSrv.Close()

	adapter, err := NewAdapter(Config{
		FeishuBaseURL: feishuSrv.URL, PaperAgentBaseURL: paperAgentSrv.URL,
		AppID: "app", AppSecret: "secret", DBPath: t.TempDir() + "/feishu.db",
		PollInterval: time.Millisecond, PollTimeout: time.Minute,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"header":{"event_id":"evt-close","event_type":"im.message.receive_v1"},"event":{"sender":{"sender_id":{"open_id":"ou_test"}},"message":{"message_id":"om_test","chat_id":"oc_test","chat_type":"p2p","message_type":"text","content":"{\"text\":\"run\"}"}}}`)
	recorder := httptest.NewRecorder()
	adapter.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/feishu/events", bytes.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("callback status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("polling did not start")
	}

	done := make(chan error, 1)
	go func() { done <- adapter.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel and wait for polling")
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestFeishuClientTenantTokenRefreshSingleflight(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(30 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "tenant_access_token": "shared-token", "expire": 3600,
		})
	}))
	defer server.Close()

	client := NewFeishuClient(server.URL, "app", "secret")
	const callers = 20
	start := make(chan struct{})
	errCh := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			token, err := client.tenantAccessToken(context.Background())
			if err == nil && token != "shared-token" {
				err = fmt.Errorf("token=%q", token)
			}
			errCh <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("token endpoint requests=%d want=1", got)
	}
}

func TestCallbackEndpointRejectsMissingOrInvalidConfiguredToken(t *testing.T) {
	var paperAgentCalled atomic.Bool
	paperAgentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paperAgentCalled.Store(true)
		writeJSON(w, http.StatusOK, ExecuteResponse{
			Success: true, TaskID: "unexpected-task", SessionID: "unexpected-session",
		})
	}))
	defer paperAgentSrv.Close()

	adapter, err := NewAdapter(Config{
		VerificationToken: "expected-token",
		PaperAgentBaseURL: paperAgentSrv.URL,
		DBPath:            t.TempDir() + "/feishu.db",
		DisablePolling:    true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()

	tests := []struct {
		name   string
		header string
	}{
		{name: "missing"},
		{name: "mismatched", header: `,"token":"wrong-token"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{
				"header":{
					"event_id":"evt-unauthorized",
					"event_type":"im.message.receive_v1"` + tt.header + `
				},
				"event":{
					"sender":{"sender_id":{"open_id":"ou_attacker"}},
					"message":{
						"message_id":"om_unauthorized",
						"chat_id":"oc_unauthorized",
						"chat_type":"p2p",
						"message_type":"text",
						"content":"{\"text\":\"run a task\"}"
					}
				}
			}`
			req := httptest.NewRequest(http.MethodPost, "/feishu/events", strings.NewReader(body))
			recorder := httptest.NewRecorder()
			adapter.Handler().ServeHTTP(recorder, req)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d body=%s, want 401", recorder.Code, recorder.Body.String())
			}
		})
	}
	if paperAgentCalled.Load() {
		t.Fatal("unauthenticated callback reached the PaperAgent API")
	}
}

func TestAdapterMessageSubmitsPaperAgentTaskAndReplies(t *testing.T) {
	var executeSeen atomic.Bool
	var replySeen atomic.Bool

	paperAgentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/execute" {
			t.Fatalf("unexpected paperAgent path: %s", r.URL.Path)
		}
		var req ExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Message != "帮我检查测试" || !req.Async || req.Mode != "agent" {
			t.Fatalf("unexpected execute request: %+v", req)
		}
		executeSeen.Store(true)
		writeJSON(w, http.StatusOK, ExecuteResponse{Success: true, TaskID: "task1", SessionID: "sess1"})
	}))
	defer paperAgentSrv.Close()

	feishuSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
		case strings.HasPrefix(r.URL.Path, "/open-apis/im/v1/messages/om_1/reply"):
			if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("authorization = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if got, ok := body["reply_in_thread"].(bool); !ok || !got {
				t.Fatalf("reply_in_thread = %#v, want true", body["reply_in_thread"])
			}
			replySeen.Store(true)
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ok"})
		default:
			t.Fatalf("unexpected feishu path: %s", r.URL.Path)
		}
	}))
	defer feishuSrv.Close()

	adapter, err := NewAdapter(Config{
		FeishuBaseURL:     feishuSrv.URL,
		PaperAgentBaseURL: paperAgentSrv.URL,
		AppID:             "cli_test",
		AppSecret:         "secret",
		DBPath:            t.TempDir() + "/feishu.db",
		DisablePolling:    true,
		PollInterval:      time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()

	adapter.handleMessage(context.Background(), MessageEvent{
		EventID:      "evt1",
		TenantKey:    "tenant1",
		MessageID:    "om_1",
		ChatID:       "oc_1",
		ChatType:     "p2p",
		SenderOpenID: "ou_1",
		Text:         "帮我检查测试",
	})

	if !executeSeen.Load() || !replySeen.Load() {
		t.Fatalf("executeSeen=%v replySeen=%v", executeSeen.Load(), replySeen.Load())
	}
	mapping, err := adapter.store.GetMapping("tenant1:oc_1")
	if err != nil {
		t.Fatal(err)
	}
	if mapping == nil || mapping.SessionID != "sess1" || mapping.LastTaskID != "task1" {
		t.Fatalf("mapping = %+v", mapping)
	}
}

func TestAdapterDownloadsFeishuImageAndForwardsItToPaperAgent(t *testing.T) {
	var executeSeen atomic.Bool
	imageBytes := []byte("\x89PNG\r\n\x1a\nfeishu-image")

	paperAgentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/execute" {
			t.Fatalf("unexpected paperAgent path: %s", r.URL.Path)
		}
		var req ExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Message != "请分析这张图" || len(req.Images) != 1 {
			t.Fatalf("unexpected execute request: %+v", req)
		}
		if !strings.HasPrefix(req.Images[0], "data:image/png;base64,") {
			t.Fatalf("image = %q", req.Images[0])
		}
		executeSeen.Store(true)
		writeJSON(w, http.StatusOK, ExecuteResponse{Success: true, TaskID: "task-image", SessionID: "sess-image"})
	}))
	defer paperAgentSrv.Close()

	feishuSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
		case r.Method == http.MethodGet && r.URL.Path == "/open-apis/im/v1/messages/om_image/resources/img_key":
			if got := r.URL.Query().Get("type"); got != "image" {
				t.Fatalf("resource type = %q", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("authorization = %q", got)
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageBytes)
		case strings.HasPrefix(r.URL.Path, "/open-apis/im/v1/messages/om_image/reply"):
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ok"})
		default:
			t.Fatalf("unexpected feishu path: %s", r.URL.Path)
		}
	}))
	defer feishuSrv.Close()

	adapter, err := NewAdapter(Config{
		FeishuBaseURL:     feishuSrv.URL,
		PaperAgentBaseURL: paperAgentSrv.URL,
		AppID:             "cli_test",
		AppSecret:         "secret",
		DBPath:            t.TempDir() + "/feishu.db",
		DisablePolling:    true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()

	adapter.handleMessage(context.Background(), MessageEvent{
		EventID:      "evt-image",
		MessageID:    "om_image",
		ChatID:       "oc_image",
		ChatType:     "p2p",
		SenderOpenID: "ou_1",
		Text:         "请分析这张图",
		Resources:    []MessageResource{{Key: "img_key", Type: "image"}},
	})
	if !executeSeen.Load() {
		t.Fatal("PaperAgent execute endpoint was not called")
	}
}

func TestAdapterPollTaskPublishesFinalResultToChannel(t *testing.T) {
	paperAgentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/status" {
			t.Fatalf("unexpected paperAgent path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, StatusResponse{
			TaskID:   "task-final",
			Status:   "completed",
			Complete: true,
			Success:  true,
			Result:   "频道可见结果",
		})
	}))
	defer paperAgentSrv.Close()

	replySeen := make(chan map[string]any, 1)
	feishuSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
		case r.URL.Path == "/open-apis/im/v1/messages":
			if got := r.URL.Query().Get("receive_id_type"); got != "chat_id" {
				t.Fatalf("receive_id_type = %q, want chat_id", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["receive_id"] != "oc-source" {
				t.Fatalf("receive_id = %#v, want oc-source", body["receive_id"])
			}
			replySeen <- body
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ok"})
		default:
			t.Fatalf("unexpected feishu path: %s", r.URL.Path)
		}
	}))
	defer feishuSrv.Close()

	adapter, err := NewAdapter(Config{
		FeishuBaseURL:     feishuSrv.URL,
		PaperAgentBaseURL: paperAgentSrv.URL,
		AppID:             "cli_test",
		AppSecret:         "secret",
		DBPath:            t.TempDir() + "/feishu.db",
		PollInterval:      time.Millisecond,
		PollTimeout:       time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()

	adapter.pollTask(context.Background(), "oc-source", "om-source", "task-final")

	select {
	case body := <-replySeen:
		if body["msg_type"] != "interactive" {
			t.Fatalf("msg_type = %#v, want interactive", body["msg_type"])
		}
		if _, exists := body["reply_in_thread"]; exists {
			t.Fatalf("final result should be a channel reply, body = %#v", body)
		}
		contentJSON, ok := body["content"].(string)
		if !ok {
			t.Fatalf("final content = %#v", body["content"])
		}
		var card map[string]any
		if err := json.Unmarshal([]byte(contentJSON), &card); err != nil {
			t.Fatalf("decode final card: %v", err)
		}
		header, ok := card["header"].(map[string]any)
		if !ok || header["template"] != "green" {
			t.Fatalf("final card header = %#v", card["header"])
		}
		elements, ok := card["elements"].([]any)
		if !ok || len(elements) == 0 {
			t.Fatalf("final card elements = %#v", card["elements"])
		}
		encoded, err := json.Marshal(card)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), "频道可见结果") {
			t.Fatalf("final card content missing result: %s", encoded)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for final channel reply")
	}
}

func TestFinalResultCardNormalizesLegacyMarkdown(t *testing.T) {
	card := finalResultCard(StatusResponse{
		Complete: true,
		Success:  true,
		Result:   "## 失败摘要\n\n| 项目 | 内容 |\n|---|---|\n| Job | [构建链接](https://jenkins.example/job/28) |\n\n```text\nDEPLOY_METHOD: parameter not set\n```",
	})
	elements, ok := card["elements"].([]any)
	if !ok || len(elements) < 3 {
		t.Fatalf("card elements = %#v", card["elements"])
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	content := string(encoded)
	if strings.Contains(content, "## 失败摘要") || strings.Contains(content, "| 项目 | 内容 |") || strings.Contains(content, "|---|---|") {
		t.Fatalf("legacy-incompatible markdown was not normalized: %s", content)
	}
	for _, want := range []string{"lark_md", "fields", "失败摘要", "Job", "构建链接", "https://jenkins.example/job/28", "DEPLOY_METHOD: parameter not set"} {
		if !strings.Contains(content, want) {
			t.Fatalf("normalized card content missing %q: %s", want, content)
		}
	}
}

func TestFormatFinalNormalizesPlainTextFallback(t *testing.T) {
	got := formatFinal(StatusResponse{
		Complete: true,
		Success:  true,
		Result: strings.Join([]string{
			"我查到今天北京天气大致是：",
			"",
			"| 项目 | 情况 |",
			"|---|---|",
			"| 天气 | 多云转中雨 |",
			"| 气温 | 约 25-31°C |",
			"| 空气质量 | 约 59，良/可接受 |",
			"",
			"**结论：今天不太适合安排长时间户外游玩。**",
			"",
			"- 带伞或轻便雨衣；",
			"- 关注临近预报。",
		}, "\n"),
	})
	for _, bad := range []string{"| 项目 | 情况 |", "|---|---|", "**结论", "- 带伞"} {
		if strings.Contains(got, bad) {
			t.Fatalf("plain fallback still contains raw markdown %q: %q", bad, got)
		}
	}
	for _, want := range []string{"PaperAgent 任务完成：", "天气：多云转中雨", "气温：约 25-31°C", "结论：今天不太适合安排长时间户外游玩。", "• 带伞或轻便雨衣；"} {
		if !strings.Contains(got, want) {
			t.Fatalf("plain fallback missing %q: %q", want, got)
		}
	}
}

func TestPausedAndWaitingStatusesAreNotRenderedAsFailures(t *testing.T) {
	tests := []struct {
		status StatusResponse
		title  string
		color  string
	}{
		{
			status: StatusResponse{
				Status: "paused", Complete: true, Success: false,
				Result: "请选择下一步方向。",
			},
			title: "PaperAgent 任务已暂停",
			color: "orange",
		},
		{
			status: StatusResponse{
				Status: "waiting_for_input", Complete: true, Success: false,
				Result: "请提供目标环境。",
			},
			title: "PaperAgent 任务等待输入",
			color: "blue",
		},
	}
	for _, test := range tests {
		t.Run(test.status.Status, func(t *testing.T) {
			fallback := formatFinal(test.status)
			if strings.Contains(fallback, "任务失败") || !strings.Contains(fallback, test.title) {
				t.Fatalf("fallback = %q", fallback)
			}
			card := finalResultCard(test.status)
			header, ok := card["header"].(map[string]any)
			if !ok || header["template"] != test.color {
				t.Fatalf("card header = %#v", card["header"])
			}
			encoded, err := json.Marshal(card)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(encoded), test.title) {
				t.Fatalf("card does not contain %q: %s", test.title, encoded)
			}
		})
	}
}

func TestMachineLoadResultIsReadableInCardAndFallback(t *testing.T) {
	result := strings.Join([]string{
		"当前机器负载情况：",
		"",
		"| 项目 | 当前值 | 判断 |",
		"|---|---:|---|",
		"| CPU 核数 | 8 cores | - |",
		"| Load Average | `5.80 / 3.87 / 2.39` | 中等偏高，但未超过 8 核容量 |",
		"| 内存总量 | 约 31.0 GiB | - |",
		"| 可用内存 | 约 21.8 GiB | 很充足 |",
		"| 根分区磁盘 | 200G 总量，已用 145G，可用 56G | 使用率 73%，偏高但未告急 |",
		"",
		"结论：**CPU 当前有一定压力，但还没到过载；内存很充足；磁盘空间需要留意。**",
	}, "\n")

	card := finalResultCard(StatusResponse{Complete: true, Success: true, Result: result})
	encoded, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	cardContent := string(encoded)
	for _, bad := range []string{"| 项目 | 当前值 | 判断 |", "|---|---:|---|", "`5.80 / 3.87 / 2.39`"} {
		if strings.Contains(cardContent, bad) {
			t.Fatalf("card still contains unsupported markdown %q: %s", bad, cardContent)
		}
	}
	for _, want := range []string{"lark_md", "CPU 核数", "当前值", "8 cores", "Load Average", "5.80 / 3.87 / 2.39", "判断", "中等偏高，但未超过 8 核容量", "结论"} {
		if !strings.Contains(cardContent, want) {
			t.Fatalf("card missing %q: %s", want, cardContent)
		}
	}

	fallback := formatFinal(StatusResponse{Complete: true, Success: true, Result: result})
	for _, bad := range []string{"| 项目 | 当前值 | 判断 |", "|---|---:|---|", "`5.80 / 3.87 / 2.39`", "**CPU 当前"} {
		if strings.Contains(fallback, bad) {
			t.Fatalf("fallback still contains unsupported markdown %q: %q", bad, fallback)
		}
	}
	for _, want := range []string{
		"CPU 核数\n  当前值：8 cores\n  判断：-",
		"Load Average\n  当前值：5.80 / 3.87 / 2.39\n  判断：中等偏高，但未超过 8 核容量",
		"结论：CPU 当前有一定压力，但还没到过载；内存很充足；磁盘空间需要留意。",
	} {
		if !strings.Contains(fallback, want) {
			t.Fatalf("fallback missing %q: %q", want, fallback)
		}
	}
}

func TestAdapterMessageIncludesReferencedInteractiveCardBody(t *testing.T) {
	var executeSeen atomic.Bool
	var quoteFetchSeen atomic.Bool
	var replySeen atomic.Bool

	paperAgentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/execute" {
			t.Fatalf("unexpected paperAgent path: %s", r.URL.Path)
		}
		var req ExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(req.Message, "[飞书引用消息]\n示例告警 | demo-service：实例重启异常") {
			t.Fatalf("referenced message missing from execute request: %q", req.Message)
		}
		for _, want := range []string{
			"环境： agentcraft-demo",
			"pod： demo-worker-7d9f6b8c4d-abc12",
			"指标： restart_count",
			"链接：https://grafana.example/alert/123",
			"[当前消息]\n实例重启异常的原因是什么，demo 环境",
		} {
			if !strings.Contains(req.Message, want) {
				t.Fatalf("execute request missing %q: %q", want, req.Message)
			}
		}
		if strings.Contains(req.Message, "must-not-leak") {
			t.Fatalf("interactive card control data leaked into execute request: %q", req.Message)
		}
		executeSeen.Store(true)
		writeJSON(w, http.StatusOK, ExecuteResponse{Success: true, TaskID: "task2", SessionID: "sess2"})
	}))
	defer paperAgentSrv.Close()

	feishuSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
		case r.Method == http.MethodGet && r.URL.Path == "/open-apis/im/v1/messages/om_parent":
			if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("authorization = %q", got)
			}
			content := `{"title":"示例告警 | demo-service：实例重启异常"}`
			switch got := r.URL.Query().Get("card_content_type"); got {
			case "":
				// Feishu's default representation contains only the card summary.
			case "user":
				content = `{"schema":"2.0","header":{"title":{"tag":"plain_text","content":"示例告警 | demo-service：实例重启异常"}},"body":{"elements":[{"tag":"markdown","content":"服务： demo-service\n环境： agentcraft-demo\n实例： 192.0.2.10:8080\npod： demo-worker-7d9f6b8c4d-abc12\n优先级： P1\n严重度： critical\n指标： restart_count"},{"tag":"button","text":{"tag":"plain_text","content":"查看示例监控告警"},"behaviors":[{"type":"open_url","default_url":"https://grafana.example/alert/123","internal_payload":"must-not-leak"}] }]}}`
			default:
				t.Fatalf("unexpected card_content_type: %q", got)
			}
			quoteFetchSeen.Store(true)
			writeJSON(w, http.StatusOK, map[string]any{
				"code": 0,
				"msg":  "ok",
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"message_id": "om_parent",
							"msg_type":   "interactive",
							"body": map[string]any{
								"content": content,
							},
						},
					},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/open-apis/im/v1/messages/om_2/reply"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if got, ok := body["reply_in_thread"].(bool); !ok || !got {
				t.Fatalf("reply_in_thread = %#v, want true", body["reply_in_thread"])
			}
			replySeen.Store(true)
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ok"})
		default:
			t.Fatalf("unexpected feishu path: %s", r.URL.Path)
		}
	}))
	defer feishuSrv.Close()

	adapter, err := NewAdapter(Config{
		FeishuBaseURL:     feishuSrv.URL,
		PaperAgentBaseURL: paperAgentSrv.URL,
		AppID:             "cli_test",
		AppSecret:         "secret",
		DBPath:            t.TempDir() + "/feishu.db",
		DisablePolling:    true,
		PollInterval:      time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()

	adapter.handleMessage(context.Background(), MessageEvent{
		EventID:      "evt2",
		TenantKey:    "tenant1",
		MessageID:    "om_2",
		ParentID:     "om_parent",
		ChatID:       "oc_1",
		ChatType:     "p2p",
		SenderOpenID: "ou_1",
		Text:         "实例重启异常的原因是什么，demo 环境",
	})

	if !executeSeen.Load() || !quoteFetchSeen.Load() || !replySeen.Load() {
		t.Fatalf("executeSeen=%v quoteFetchSeen=%v replySeen=%v", executeSeen.Load(), quoteFetchSeen.Load(), replySeen.Load())
	}
}

func TestAdapterIgnoresGroupMessageMentioningEveryone(t *testing.T) {
	var executeSeen atomic.Bool
	paperAgentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		executeSeen.Store(true)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer paperAgentSrv.Close()

	adapter, err := NewAdapter(Config{
		PaperAgentBaseURL: paperAgentSrv.URL,
		DBPath:            t.TempDir() + "/feishu.db",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()

	adapter.handleMessage(context.Background(), MessageEvent{
		EventID:      "evt-all",
		MessageID:    "om-all",
		ChatID:       "oc-group",
		ChatType:     "group",
		SenderOpenID: "ou-user",
		Text:         "这是测试群",
		Mentions:     []MessageMention{{OpenID: "all", Name: "所有人"}},
	})

	if executeSeen.Load() {
		t.Fatal("@all message should not execute a PaperAgent task")
	}
}

func TestAdapterAcceptsGroupMessageWithExplicitMention(t *testing.T) {
	var executeSeen atomic.Bool
	paperAgentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Message != "请继续处理" {
			t.Fatalf("message = %q, want explicit group message", req.Message)
		}
		executeSeen.Store(true)
		writeJSON(w, http.StatusOK, ExecuteResponse{Success: true, TaskID: "task-group", SessionID: "sess-group"})
	}))
	defer paperAgentSrv.Close()

	feishuSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
		case strings.HasPrefix(r.URL.Path, "/open-apis/im/v1/messages/om-bot/reply"):
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ok"})
		default:
			t.Fatalf("unexpected Feishu path: %s", r.URL.Path)
		}
	}))
	defer feishuSrv.Close()

	adapter, err := NewAdapter(Config{
		FeishuBaseURL:     feishuSrv.URL,
		PaperAgentBaseURL: paperAgentSrv.URL,
		AppID:             "cli_test",
		AppSecret:         "secret",
		DBPath:            t.TempDir() + "/feishu.db",
		DisablePolling:    true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()

	adapter.handleMessage(context.Background(), MessageEvent{
		EventID:      "evt-bot",
		MessageID:    "om-bot",
		ChatID:       "oc-group",
		ChatType:     "group",
		SenderOpenID: "ou-user",
		Text:         "请继续处理",
		Mentions:     []MessageMention{{OpenID: "ou_bot", Name: "ShadowSRE"}},
	})

	if !executeSeen.Load() {
		t.Fatal("explicit bot mention should execute a PaperAgent task")
	}
}

func TestAdapterApprovalActionUpdatesCard(t *testing.T) {
	var approveSeen atomic.Bool
	updateSeen := make(chan map[string]any, 1)
	paperAgentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/approve" {
			t.Fatalf("unexpected paperAgent path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["task_id"] != "task1" || body["tool_id"] != "tool1" || body["approved"] != true {
			t.Fatalf("unexpected approve body: %#v", body)
		}
		approveSeen.Store(true)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	defer paperAgentSrv.Close()

	feishuSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
		case "/open-apis/interactive/v1/card/update":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			updateSeen <- body
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ok"})
		default:
			t.Fatalf("unexpected feishu path: %s", r.URL.Path)
		}
	}))
	defer feishuSrv.Close()

	adapter, err := NewAdapter(Config{
		FeishuBaseURL:     feishuSrv.URL,
		PaperAgentBaseURL: paperAgentSrv.URL,
		AppID:             "cli_test",
		AppSecret:         "secret",
		VerificationToken: "verification-token",
		DBPath:            t.TempDir() + "/feishu.db",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()

	body := `{"header":{"event_id":"evt-approve","event_type":"card.action.trigger","token":"verification-token"},"event":{"token":"card-token","context":{"open_message_id":"om-card"},"action":{"value":{"action":"approve","task_id":"task1","tool_id":"tool1","tool_name":"bash","params_preview":"ls"}}}}`
	req := httptest.NewRequest(http.MethodPost, "/feishu/card-callback", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	adapter.handleCallback(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !approveSeen.Load() {
		t.Fatal("PaperAgent approve request was not sent")
	}

	select {
	case update := <-updateSeen:
		if update["token"] != "card-token" {
			t.Fatalf("update token = %#v", update["token"])
		}
		card, ok := update["card"].(map[string]any)
		if !ok {
			t.Fatalf("updated card = %#v", update["card"])
		}
		elements, ok := card["elements"].([]any)
		if !ok || len(elements) != 2 {
			t.Fatalf("updated card elements = %#v", card["elements"])
		}
		action, ok := elements[1].(map[string]any)
		if !ok {
			t.Fatalf("updated card action = %#v", elements[1])
		}
		for _, raw := range action["actions"].([]any) {
			if raw.(map[string]any)["disabled"] != true {
				t.Fatalf("updated button = %#v, want disabled=true", raw)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Feishu card update")
	}
}

func TestAdapterApproveAllAction(t *testing.T) {
	var gotBody map[string]any
	paperAgentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/approve" {
			t.Fatalf("unexpected paperAgent path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	defer paperAgentSrv.Close()

	adapter, err := NewAdapter(Config{
		FeishuBaseURL:     paperAgentSrv.URL,
		PaperAgentBaseURL: paperAgentSrv.URL,
		AppID:             "cli_test",
		AppSecret:         "secret",
		DBPath:            t.TempDir() + "/feishu.db",
		DisablePolling:    true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()

	err = adapter.handleAction(context.Background(), ActionEvent{
		EventID: "evt-approve-all",
		Action:  "approve_all",
		TaskID:  "task1",
		ToolID:  "tool1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["approved"] != true || gotBody["approve_all"] != true {
		t.Fatalf("approve-all body = %#v", gotBody)
	}
}

func TestFeishuClientReplyTextInThread(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
		case "/open-apis/im/v1/messages/om_thread/reply":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ok"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewFeishuClient(server.URL, "cli_test", "secret")
	if err := client.ReplyTextInThread(context.Background(), "om_thread", "thread reply"); err != nil {
		t.Fatal(err)
	}
	if got, ok := gotBody["reply_in_thread"].(bool); !ok || !got {
		t.Fatalf("reply_in_thread = %#v, want true", gotBody["reply_in_thread"])
	}
	if got := gotBody["msg_type"]; got != "text" {
		t.Fatalf("msg_type = %#v, want text", got)
	}
}

func TestFeishuClientGetMessageTextExtractsInteractiveCard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
		case "/open-apis/im/v1/messages/om-card":
			content := `{"title":"Jenkins 构建失败"}`
			switch got := r.URL.Query().Get("card_content_type"); got {
			case "":
			case "user":
				content = `{"schema":"2.0","header":{"title":{"tag":"plain_text","content":"Jenkins 构建失败"}},"body":[{"tag":"markdown","content":"失败阶段：Docker build\n原因：镜像构建失败"}]}`
			default:
				t.Fatalf("unexpected card_content_type: %q", got)
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"code": 0,
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"message_id": "om-card",
							"msg_type":   "interactive",
							"body": map[string]any{
								"content": content,
							},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewFeishuClient(server.URL, "cli_test", "secret")
	message, err := client.GetMessageText(context.Background(), "om-card")
	if err != nil {
		t.Fatal(err)
	}
	if message.MsgType != "interactive" {
		t.Fatalf("msg_type = %q, want interactive", message.MsgType)
	}
	for _, want := range []string{"Jenkins 构建失败", "失败阶段：Docker build", "原因：镜像构建失败"} {
		if !strings.Contains(message.Text, want) {
			t.Fatalf("card text missing %q: %q", want, message.Text)
		}
	}
}

func TestFeishuClientGetMessageTextPreservesLegacyInteractiveCard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
		case "/open-apis/im/v1/messages/om-legacy-card":
			if got := r.URL.Query().Get("card_content_type"); got != "" {
				t.Fatalf("complete legacy card should not be refetched, card_content_type = %q", got)
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"code": 0,
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"message_id": "om-legacy-card",
							"msg_type":   "interactive",
							"body": map[string]any{
								"content": `{"config":{"wide_screen_mode":true},"header":{"title":{"tag":"plain_text","content":"Demo App 发布失败"}},"elements":[{"tag":"div","text":{"tag":"lark_md","content":"❌ Demo App 发布失败！\n执行者：示例用户\n任务：demo-app #28\n服务：api\n环境：demo\n镜像版本：demo-v1\n分支：main\nK8s Context：arn:aws:eks:us-east-1:123456789012:cluster/example-demo"}},{"tag":"action","actions":[{"tag":"button","text":{"tag":"plain_text","content":"查看示例构建"},"url":"https://jenkins.example/job/demo-app/28/","value":{"build_token":"must-not-leak"}}]}]}`,
							},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewFeishuClient(server.URL, "cli_test", "secret")
	message, err := client.GetMessageText(context.Background(), "om-legacy-card")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Demo App 发布失败",
		"任务：demo-app #28",
		"环境：demo",
		"K8s Context：arn:aws:eks:us-east-1:123456789012:cluster/example-demo",
		"查看示例构建",
		"链接：https://jenkins.example/job/demo-app/28/",
	} {
		if !strings.Contains(message.Text, want) {
			t.Fatalf("legacy card text missing %q: %q", want, message.Text)
		}
	}
	if strings.Contains(message.Text, "must-not-leak") {
		t.Fatalf("legacy card control data leaked into text: %q", message.Text)
	}
}

func TestFeishuClientGetMessageTextFallsBackToLegacyCardSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
		case "/open-apis/im/v1/messages/om-legacy-summary":
			if r.URL.Query().Get("card_content_type") == "user" {
				writeJSON(w, http.StatusForbidden, map[string]any{"code": 230027, "msg": "card content unavailable"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"code": 0,
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"message_id": "om-legacy-summary",
							"msg_type":   "interactive",
							"body": map[string]any{
								"content": `{"title":"Demo App 发布失败\n任务：demo-app #28\n环境：demo"}`,
							},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewFeishuClient(server.URL, "cli_test", "secret")
	message, err := client.GetMessageText(context.Background(), "om-legacy-summary")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Demo App 发布失败", "任务：demo-app #28", "环境：demo"} {
		if !strings.Contains(message.Text, want) {
			t.Fatalf("legacy summary missing %q after full-card fallback: %q", want, message.Text)
		}
	}
}

func TestFeishuClientUpdateCardByCallbackToken(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
		case "/open-apis/interactive/v1/card/update":
			if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("authorization = %q", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ok"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewFeishuClient(server.URL, "cli_test", "secret")
	card := ApprovalResultCard(ActionEvent{ToolID: "tool1"}, true)
	if err := client.UpdateCardByCallbackToken(context.Background(), "card-token", card); err != nil {
		t.Fatal(err)
	}
	if got := gotBody["token"]; got != "card-token" {
		t.Fatalf("token = %#v, want card-token", got)
	}
	cardBody, ok := gotBody["card"].(map[string]any)
	if !ok {
		t.Fatalf("card = %#v", gotBody["card"])
	}
	config, ok := cardBody["config"].(map[string]any)
	if !ok || config["update_multi"] != true {
		t.Fatalf("card config = %#v, want update_multi=true", cardBody["config"])
	}
}

func TestApprovalResultCardDisablesActions(t *testing.T) {
	card := ApprovalResultCard(ActionEvent{ToolID: "tool1"}, true)
	elements, ok := card["elements"].([]any)
	if !ok || len(elements) != 2 {
		t.Fatalf("elements = %#v", card["elements"])
	}
	action, ok := elements[1].(map[string]any)
	if !ok {
		t.Fatalf("action element = %#v", elements[1])
	}
	actions, ok := action["actions"].([]any)
	if !ok || len(actions) != 3 {
		t.Fatalf("actions = %#v", action["actions"])
	}
	for _, raw := range actions {
		button, ok := raw.(map[string]any)
		if !ok || button["disabled"] != true {
			t.Fatalf("button = %#v, want disabled=true", raw)
		}
	}
}

func TestApprovalCardOffersTaskScopedApproval(t *testing.T) {
	card := ApprovalCard(PendingApproval{ToolID: "tool1", ToolName: "bash"}, "task1")
	elements := card["elements"].([]any)
	actions := elements[1].(map[string]any)["actions"].([]any)
	for _, raw := range actions {
		button := raw.(map[string]any)
		value, _ := button["value"].(map[string]string)
		if value["action"] == "approve_all" {
			return
		}
	}
	t.Fatal("approval card is missing approve_all action")
}
