package feishu

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var errCallbackAuthentication = errors.New("feishu callback authentication failed")

type Callback struct {
	Challenge string
	Message   *MessageEvent
	Action    *ActionEvent
}

type MessageEvent struct {
	EventID      string
	TenantKey    string
	MessageID    string
	RootID       string
	ParentID     string
	ChatID       string
	ChatType     string
	SenderOpenID string
	Text         string
	MessageType  string
	Mentions     []MessageMention
	Resources    []MessageResource
}

type MessageResource struct {
	Key  string
	Type string
}

// MessageMention is a user or special target mentioned in a Feishu message.
// Feishu represents the @all target as the literal identifier "all".
type MessageMention struct {
	Key     string
	Name    string
	OpenID  string
	UserID  string
	UnionID string
}

func (m MessageMention) IsAll() bool {
	if m.OpenID != "" || m.UserID != "" || m.UnionID != "" {
		return strings.EqualFold(m.OpenID, "all") ||
			strings.EqualFold(m.UserID, "all") ||
			strings.EqualFold(m.UnionID, "all")
	}
	return strings.EqualFold(strings.TrimSpace(m.Name), "all") ||
		strings.TrimSpace(m.Name) == "所有人"
}

// HasExplicitMention reports whether the message contains a mention other
// than @all. The group-at event is delivered for both @bot and @all, so this
// is the adapter-level boundary that keeps @all from creating tasks.
func (m MessageEvent) HasExplicitMention() bool {
	for _, mention := range m.Mentions {
		if !mention.IsAll() {
			return true
		}
	}
	return false
}

type ActionEvent struct {
	EventID       string
	TenantKey     string
	Action        string
	TaskID        string
	ToolID        string
	ToolName      string
	ParamsPreview string
	Output        string
	MessageID     string
	CallbackToken string
}

func ParseCallback(body []byte, verificationToken string) (Callback, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return Callback{}, err
	}
	if err := verifyToken(root, verificationToken); err != nil {
		return Callback{}, err
	}

	if challenge, _ := root["challenge"].(string); challenge != "" {
		return Callback{Challenge: challenge}, nil
	}

	eventType := stringAt(root, "header", "event_type")
	if eventType == "" {
		eventType, _ = root["type"].(string)
	}
	switch {
	case eventType == "im.message.receive_v1":
		msg, err := parseMessageEvent(root)
		return Callback{Message: msg}, err
	case eventType == "card.action.trigger" || eventType == "card.action":
		action, err := parseActionEvent(root)
		return Callback{Action: action}, err
	default:
		// Some Feishu card callback examples omit event_type; try action shape
		// before declaring it unsupported.
		if hasActionPayload(root) {
			action, err := parseActionEvent(root)
			return Callback{Action: action}, err
		}
		return Callback{}, fmt.Errorf("unsupported feishu callback type %q", eventType)
	}
}

func verifyToken(root map[string]any, expected string) error {
	if expected == "" {
		return nil
	}
	token, _ := root["token"].(string)
	if token == "" {
		token = stringAt(root, "header", "token")
	}
	if token == "" {
		return fmt.Errorf("%w: verification token is missing", errCallbackAuthentication)
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		return fmt.Errorf("%w: verification token mismatch", errCallbackAuthentication)
	}
	return nil
}

func parseMessageEvent(root map[string]any) (*MessageEvent, error) {
	event, _ := root["event"].(map[string]any)
	message, _ := event["message"].(map[string]any)
	sender, _ := event["sender"].(map[string]any)
	senderID, _ := sender["sender_id"].(map[string]any)

	msg := &MessageEvent{
		EventID:      stringAt(root, "header", "event_id"),
		TenantKey:    stringAt(root, "header", "tenant_key"),
		MessageID:    stringValue(message["message_id"]),
		RootID:       stringValue(message["root_id"]),
		ParentID:     stringValue(message["parent_id"]),
		ChatID:       stringValue(message["chat_id"]),
		ChatType:     stringValue(message["chat_type"]),
		SenderOpenID: stringValue(senderID["open_id"]),
		MessageType:  stringValue(message["message_type"]),
		Mentions:     parseMessageMentions(message["mentions"]),
	}
	if msg.EventID == "" {
		msg.EventID, _ = root["uuid"].(string)
	}
	if msg.MessageID == "" || msg.ChatID == "" {
		return nil, errors.New("missing feishu message_id or chat_id")
	}
	msg.Text, msg.Resources = extractMessageContent(msg.MessageType, stringValue(message["content"]))
	if msg.Text == "" {
		if len(msg.Resources) > 0 {
			msg.Text = "[图片消息]"
		} else {
			msg.Text = "[非文本消息]"
		}
	}
	return msg, nil
}

func parseMessageMentions(value any) []MessageMention {
	rawMentions, ok := value.([]any)
	if !ok {
		return nil
	}
	mentions := make([]MessageMention, 0, len(rawMentions))
	for _, raw := range rawMentions {
		mention, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		var openID, userID, unionID string
		switch idValue := mention["id"].(type) {
		case string:
			openID = idValue
		case map[string]any:
			openID = stringValue(idValue["open_id"])
			userID = stringValue(idValue["user_id"])
			unionID = stringValue(idValue["union_id"])
		}
		mentions = append(mentions, MessageMention{
			Key:     stringValue(mention["key"]),
			Name:    stringValue(mention["name"]),
			OpenID:  openID,
			UserID:  userID,
			UnionID: unionID,
		})
	}
	return mentions
}

func (m MessageEvent) ReferencedMessageID() string {
	for _, id := range []string{m.ParentID, m.RootID} {
		if id != "" && id != m.MessageID {
			return id
		}
	}
	return ""
}

func extractMessageText(messageType, content string) string {
	text, _ := extractMessageContent(messageType, content)
	return text
}

func extractMessageContent(messageType, content string) (string, []MessageResource) {
	messageType = strings.ToLower(strings.TrimSpace(messageType))
	if messageType != "" && messageType != "text" && messageType != "interactive" &&
		messageType != "image" && messageType != "post" {
		return "", nil
	}
	var payload any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return strings.TrimSpace(content), nil
	}
	if messageType == "interactive" {
		return extractInteractiveCardText(payload), nil
	}
	if messageType == "image" {
		if object, ok := payload.(map[string]any); ok {
			if key := strings.TrimSpace(stringValue(object["image_key"])); key != "" {
				return "", []MessageResource{{Key: key, Type: "image"}}
			}
		}
		return "", nil
	}
	if messageType == "post" {
		var lines []string
		var resources []MessageResource
		collectPostContent(payload, &lines, &resources)
		return strings.Join(lines, "\n"), dedupeResources(resources)
	}
	if object, ok := payload.(map[string]any); ok {
		return strings.TrimSpace(stringValue(object["text"])), nil
	}
	return "", nil
}

func collectPostContent(value any, lines *[]string, resources *[]MessageResource) {
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			collectPostContent(child, lines, resources)
		}
	case map[string]any:
		tag := strings.ToLower(strings.TrimSpace(stringValue(node["tag"])))
		switch tag {
		case "img", "image":
			if key := strings.TrimSpace(stringValue(node["image_key"])); key != "" {
				*resources = append(*resources, MessageResource{Key: key, Type: "image"})
			}
			return
		case "text":
			appendPostText(lines, stringValue(node["text"]))
			return
		case "a":
			label := strings.TrimSpace(stringValue(node["text"]))
			href := strings.TrimSpace(firstNonEmpty(stringValue(node["href"]), stringValue(node["url"])))
			switch {
			case label != "" && href != "":
				appendPostText(lines, label+" ("+href+")")
			case label != "":
				appendPostText(lines, label)
			case href != "":
				appendPostText(lines, href)
			}
			return
		case "at":
			name := strings.TrimSpace(firstNonEmpty(stringValue(node["user_name"]), stringValue(node["name"])))
			if name != "" {
				appendPostText(lines, "@"+name)
			}
			return
		}

		for _, key := range []string{"title", "text"} {
			appendPostText(lines, stringValue(node[key]))
		}
		for _, key := range []string{"content", "elements", "body", "items"} {
			if child, ok := node[key]; ok {
				collectPostContent(child, lines, resources)
			}
		}

		// Rich posts are commonly wrapped by locale keys such as zh_cn. Walk
		// unhandled containers while excluding metadata and already-visited
		// content fields.
		keys := make([]string, 0, len(node))
		for key := range node {
			switch key {
			case "tag", "title", "text", "content", "elements", "body", "items",
				"image_key", "href", "url", "user_id", "user_name", "name", "style":
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			collectPostContent(node[key], lines, resources)
		}
	}
}

func appendPostText(lines *[]string, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if len(*lines) > 0 && (*lines)[len(*lines)-1] == text {
		return
	}
	*lines = append(*lines, text)
}

func dedupeResources(resources []MessageResource) []MessageResource {
	seen := make(map[string]bool, len(resources))
	out := make([]MessageResource, 0, len(resources))
	for _, resource := range resources {
		key := resource.Type + "\x00" + resource.Key
		if resource.Key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, resource)
	}
	return out
}

func extractInteractiveCardText(payload any) string {
	// Some card APIs wrap the actual card under a JSON-encoded "card" field.
	if object, ok := payload.(map[string]any); ok {
		if encoded, ok := object["card"].(string); ok {
			var nested any
			if json.Unmarshal([]byte(encoded), &nested) == nil {
				payload = nested
			}
		}
	}

	lines := make([]string, 0, 8)
	collectCardText(payload, &lines)
	return strings.Join(lines, "\n\n")
}

func collectCardText(value any, lines *[]string) {
	switch node := value.(type) {
	case []any:
		for _, item := range node {
			collectCardText(item, lines)
		}
	case map[string]any:
		tag := strings.ToLower(stringValue(node["tag"]))
		if isCardTextTag(tag) {
			appendCardText(lines, stringValue(node["content"]))
			return
		}

		// Button values are control data for callbacks, not user-facing content.
		// Keep the visible label and link, but skip value/task IDs.
		if tag == "button" {
			collectCardText(node["text"], lines)
			collectCardText(node["alt"], lines)
			if link := firstNonEmpty(stringValue(node["url"]), stringValue(node["href"])); link != "" {
				appendCardText(lines, "链接："+link)
			}
			collectCardBehaviorLinks(node["behaviors"], lines)
			return
		}

		orderedKeys := []string{
			"header", "title", "subtitle", "text", "label", "alt",
			"fields", "elements", "body", "actions", "columns", "rows",
			"contents", "i18n_elements", "i18n", "card", "data", "content",
		}
		visited := make(map[string]bool, len(orderedKeys))
		for _, key := range orderedKeys {
			child, ok := node[key]
			if !ok {
				continue
			}
			visited[key] = true
			if text, ok := child.(string); ok {
				appendCardText(lines, text)
				continue
			}
			collectCardText(child, lines)
		}

		// Preserve forward compatibility for new card containers while keeping
		// metadata such as tag, value, and template out of the extracted text.
		remainingKeys := make([]string, 0, len(node))
		for key := range node {
			if visited[key] || isCardMetadataKey(key) {
				continue
			}
			remainingKeys = append(remainingKeys, key)
		}
		sort.Strings(remainingKeys)
		for _, key := range remainingKeys {
			collectCardText(node[key], lines)
		}
	}
}

func collectCardBehaviorLinks(value any, lines *[]string) {
	behaviors, ok := value.([]any)
	if !ok {
		return
	}
	for _, raw := range behaviors {
		behavior, ok := raw.(map[string]any)
		if !ok || strings.ToLower(stringValue(behavior["type"])) != "open_url" {
			continue
		}
		link := firstNonEmpty(
			stringValue(behavior["default_url"]),
			stringValue(behavior["url"]),
			stringValue(behavior["pc_url"]),
			stringValue(behavior["ios_url"]),
			stringValue(behavior["android_url"]),
		)
		if link != "" {
			appendCardText(lines, "链接："+link)
		}
	}
}

func isCardTextTag(tag string) bool {
	switch tag {
	case "plain_text", "lark_md", "markdown", "text":
		return true
	default:
		return false
	}
}

func isCardMetadataKey(key string) bool {
	switch key {
	case "tag", "type", "value", "template", "color", "size", "flex_mode", "is_short", "wide_screen_mode", "update_multi":
		return true
	default:
		return false
	}
}

func appendCardText(lines *[]string, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	for _, existing := range *lines {
		if existing == text {
			return
		}
	}
	*lines = append(*lines, text)
}

func parseActionEvent(root map[string]any) (*ActionEvent, error) {
	value := actionValue(root)
	action := &ActionEvent{
		EventID:       stringAt(root, "header", "event_id"),
		TenantKey:     stringAt(root, "header", "tenant_key"),
		Action:        strings.ToLower(stringValue(value["action"])),
		TaskID:        stringValue(value["task_id"]),
		ToolID:        stringValue(value["tool_id"]),
		ToolName:      stringValue(value["tool_name"]),
		ParamsPreview: stringValue(value["params_preview"]),
		Output:        stringValue(value["output"]),
		MessageID: firstNonEmpty(
			stringAt(root, "event", "context", "open_message_id"),
			stringAt(root, "event", "open_message_id"),
			stringValue(root["open_message_id"]),
		),
		CallbackToken: firstNonEmpty(
			stringAt(root, "event", "token"),
			stringValue(root["event_token"]),
		),
	}
	if action.EventID == "" {
		action.EventID = stringValue(root["uuid"])
	}
	if action.Action == "" {
		action.Action = strings.ToLower(stringValue(value["type"]))
	}
	if action.TaskID == "" || action.ToolID == "" {
		return nil, errors.New("missing task_id or tool_id in feishu action")
	}
	return action, nil
}

func hasActionPayload(root map[string]any) bool {
	value := actionValue(root)
	return len(value) > 0
}

func actionValue(root map[string]any) map[string]any {
	for _, path := range [][]string{
		{"event", "action", "value"},
		{"action", "value"},
		{"event", "action"},
		{"action"},
	} {
		if m := mapAt(root, path...); len(m) > 0 {
			return m
		}
	}
	return nil
}

func stringAt(root map[string]any, path ...string) string {
	cur := root
	for i, part := range path {
		value, ok := cur[part]
		if !ok {
			return ""
		}
		if i == len(path)-1 {
			return stringValue(value)
		}
		next, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		cur = next
	}
	return ""
}

func mapAt(root map[string]any, path ...string) map[string]any {
	cur := root
	for _, part := range path {
		value, ok := cur[part]
		if !ok {
			return nil
		}
		next, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

func stringValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func SessionKey(msg MessageEvent) string {
	tenant := msg.TenantKey
	if tenant == "" {
		tenant = "default"
	}
	return tenant + ":" + msg.ChatID
}
