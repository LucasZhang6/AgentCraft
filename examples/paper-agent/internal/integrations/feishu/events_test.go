package feishu

import (
	"errors"
	"strings"
	"testing"
)

func TestParseCallbackChallenge(t *testing.T) {
	cb, err := ParseCallback([]byte(`{"type":"url_verification","token":"tok","challenge":"abc"}`), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if cb.Challenge != "abc" {
		t.Fatalf("challenge = %q, want abc", cb.Challenge)
	}
}

func TestParseCallbackRejectsMissingOrMismatchedConfiguredToken(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing",
			body: `{"type":"url_verification","challenge":"abc"}`,
		},
		{
			name: "mismatched",
			body: `{"type":"url_verification","token":"wrong","challenge":"abc"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCallback([]byte(tt.body), "expected")
			if !errors.Is(err, errCallbackAuthentication) {
				t.Fatalf("error = %v, want callback authentication error", err)
			}
		})
	}
}

func TestParseCallbackMessageEvent(t *testing.T) {
	body := []byte(`{
		"schema":"2.0",
		"header":{"event_id":"evt1","event_type":"im.message.receive_v1","tenant_key":"tenant1","token":"tok"},
		"event":{
			"sender":{"sender_id":{"open_id":"ou_1"}},
			"message":{"message_id":"om_1","root_id":"om_root","parent_id":"om_parent","chat_id":"oc_1","chat_type":"p2p","message_type":"text","content":"{\"text\":\"hello paperAgent\"}","mentions":[{"key":"@_user_1","id":{"open_id":"ou_bot"},"name":"ShadowSRE"},{"key":"@_user_2","id":{"open_id":"all"},"name":"所有人"}]}
		}
	}`)
	cb, err := ParseCallback(body, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if cb.Message == nil {
		t.Fatalf("message callback missing")
	}
	if cb.Message.Text != "hello paperAgent" || cb.Message.ChatID != "oc_1" || cb.Message.SenderOpenID != "ou_1" {
		t.Fatalf("unexpected message: %+v", cb.Message)
	}
	if cb.Message.RootID != "om_root" || cb.Message.ParentID != "om_parent" || cb.Message.ReferencedMessageID() != "om_parent" {
		t.Fatalf("unexpected reference fields: %+v", cb.Message)
	}
	if len(cb.Message.Mentions) != 2 || cb.Message.Mentions[0].OpenID != "ou_bot" || cb.Message.Mentions[1].OpenID != "all" {
		t.Fatalf("unexpected mentions: %+v", cb.Message.Mentions)
	}
	if !cb.Message.HasExplicitMention() {
		t.Fatalf("message with bot mention should be explicit")
	}
}

func TestParseCallbackActionEvent(t *testing.T) {
	body := []byte(`{
		"header":{"event_id":"evt2","event_type":"card.action.trigger","tenant_key":"tenant1"},
		"event":{"token":"card-token","context":{"open_message_id":"om_card"},"action":{"value":{"action":"approve","task_id":"task1","tool_id":"tool1","tool_name":"bash","params_preview":"ls"}}}
	}`)
	cb, err := ParseCallback(body, "")
	if err != nil {
		t.Fatal(err)
	}
	if cb.Action == nil || cb.Action.Action != "approve" || cb.Action.TaskID != "task1" || cb.Action.ToolID != "tool1" || cb.Action.ToolName != "bash" || cb.Action.ParamsPreview != "ls" || cb.Action.MessageID != "om_card" || cb.Action.CallbackToken != "card-token" {
		t.Fatalf("unexpected action: %+v", cb.Action)
	}
}

func TestMessageEventReferencedMessageIDFallback(t *testing.T) {
	msg := MessageEvent{MessageID: "om_current", RootID: "om_root"}
	if got := msg.ReferencedMessageID(); got != "om_root" {
		t.Fatalf("ReferencedMessageID() = %q, want om_root", got)
	}
	msg.ParentID = "om_parent"
	if got := msg.ReferencedMessageID(); got != "om_parent" {
		t.Fatalf("ReferencedMessageID() = %q, want om_parent", got)
	}
	msg.ParentID = "om_current"
	if got := msg.ReferencedMessageID(); got != "om_root" {
		t.Fatalf("ReferencedMessageID() = %q, want om_root", got)
	}
}

func TestMessageEventOnlyAllMentionIsNotExplicit(t *testing.T) {
	msg := MessageEvent{
		ChatType: "group",
		Mentions: []MessageMention{{OpenID: "all", Name: "所有人"}},
	}
	if msg.HasExplicitMention() {
		t.Fatal("@all should not count as an explicit bot mention")
	}
	msg.Mentions = []MessageMention{{OpenID: "ou_user", Name: "所有人"}}
	if !msg.HasExplicitMention() {
		t.Fatal("a real user named 所有人 should count as an explicit mention")
	}
}

func TestParseCallbackImageMessage(t *testing.T) {
	body := []byte(`{
		"header":{"event_id":"evt-image","event_type":"im.message.receive_v1"},
		"event":{
			"sender":{"sender_id":{"open_id":"ou_1"}},
			"message":{"message_id":"om_image","chat_id":"oc_1","chat_type":"p2p","message_type":"image","content":"{\"image_key\":\"img_v2_test\"}"}
		}
	}`)
	cb, err := ParseCallback(body, "")
	if err != nil {
		t.Fatal(err)
	}
	if cb.Message == nil || cb.Message.Text != "[图片消息]" {
		t.Fatalf("message = %+v", cb.Message)
	}
	if len(cb.Message.Resources) != 1 ||
		cb.Message.Resources[0] != (MessageResource{Key: "img_v2_test", Type: "image"}) {
		t.Fatalf("resources = %+v", cb.Message.Resources)
	}
}

func TestExtractPostContentIncludesTextAndImages(t *testing.T) {
	content := `{
		"zh_cn":{
			"title":"电脑启动故障",
			"content":[
				[{"tag":"text","text":"开机时出现以下提示"},{"tag":"img","image_key":"img_first"}],
				[{"tag":"a","text":"排障文档","href":"https://example.com/runbook"},{"tag":"img","image_key":"img_second"}]
			]
		}
	}`
	text, resources := extractMessageContent("post", content)
	for _, want := range []string{"电脑启动故障", "开机时出现以下提示", "排障文档 (https://example.com/runbook)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("post text missing %q: %q", want, text)
		}
	}
	if len(resources) != 2 || resources[0].Key != "img_first" || resources[1].Key != "img_second" {
		t.Fatalf("resources = %+v", resources)
	}
}

func TestExtractInteractiveCardTextLegacyCard(t *testing.T) {
	content := `{
		"config":{"wide_screen_mode":true},
		"header":{"template":"red","title":{"tag":"plain_text","content":"demo-build 构建失败"}},
		"elements":[
			{"tag":"div","text":{"tag":"lark_md","content":"❌ 构建失败！\n任务：demo-build #596\n链接：https://jenkins.example/job/596/"}},
			{"tag":"action","actions":[{"tag":"button","text":{"tag":"plain_text","content":"查看详情"},"url":"https://jenkins.example/job/596/","value":{"task_id":"secret-task-id"}}]}
		]
	}`

	got := extractMessageText("interactive", content)
	for _, want := range []string{
		"demo-build 构建失败",
		"❌ 构建失败！",
		"任务：demo-build #596",
		"https://jenkins.example/job/596/",
		"查看详情",
		"链接：https://jenkins.example/job/596/",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive text missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "secret-task-id") {
		t.Fatalf("interactive control value leaked into text: %q", got)
	}
}

func TestExtractInteractiveCardTextV2Card(t *testing.T) {
	content := `{
		"schema":"2.0",
		"header":{"title":{"tag":"plain_text","content":"Jenkins 构建失败"}},
		"body":[
			{"tag":"markdown","content":"失败阶段：Docker build\n原因：镜像构建失败"},
			{"tag":"column_set","columns":[{"tag":"column","elements":[{"tag":"markdown","content":"执行者：张三"}]}]}
		]
	}`

	got := extractMessageText("interactive", content)
	for _, want := range []string{"Jenkins 构建失败", "失败阶段：Docker build", "原因：镜像构建失败", "执行者：张三"} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive v2 text missing %q: %q", want, got)
		}
	}
}

func TestExtractInteractiveCardTextV2AlertCard(t *testing.T) {
	content := `{
		"schema":"2.0",
		"header":{"title":{"tag":"plain_text","content":"示例告警 | demo-service：实例重启异常"}},
		"body":{"elements":[
			{"tag":"markdown","content":"**demo-service 告警：实例重启异常**"},
			{"tag":"markdown","content":"服务： demo-service\n环境： agentcraft-demo\n实例： 192.0.2.10:8080\npod： demo-worker-7d9f6b8c4d-abc12\n优先级： P1\n严重度： critical\n指标： restart_count\n当前值： 1.3333333333333333count\n阈值： 0count\n开始时间： 2025-01-01T00:00:00Z"},
			{"tag":"button","text":{"tag":"plain_text","content":"查看示例监控告警"},"behaviors":[{"type":"open_url","default_url":"https://grafana.example/alert/123","internal_payload":"must-not-leak"}],"value":{"alert_id":"must-not-leak"}}
		]}
	}`

	got := extractMessageText("interactive", content)
	for _, want := range []string{
		"示例告警 | demo-service：实例重启异常",
		"服务： demo-service",
		"环境： agentcraft-demo",
		"pod： demo-worker-7d9f6b8c4d-abc12",
		"严重度： critical",
		"指标： restart_count",
		"开始时间： 2025-01-01T00:00:00Z",
		"查看示例监控告警",
		"链接：https://grafana.example/alert/123",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive alert text missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "must-not-leak") {
		t.Fatalf("interactive alert control data leaked into text: %q", got)
	}
}
