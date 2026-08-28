package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/multimodal"
)

const maxReferencedMessageChars = 4000

var cardMarkdownLinkPattern = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)\s]+)\)`)
var markdownBoldPattern = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
var markdownInlineCodePattern = regexp.MustCompile("`([^`\n]+)`")

type Adapter struct {
	cfg       Config
	store     *Store
	yourAgent *YourAgentClient
	feishu    *FeishuClient
	logger    *log.Logger

	ctx       context.Context
	cancel    context.CancelFunc
	pollMu    sync.Mutex
	closing   bool
	pollWG    sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

func NewAdapter(cfg Config, logger *log.Logger) (*Adapter, error) {
	cfg = cfg.WithDefaults()
	store, err := NewStore(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Adapter{
		cfg:       cfg,
		store:     store,
		yourAgent: NewYourAgentClient(cfg.YourAgentBaseURL, cfg.YourAgentAccessID),
		feishu:    NewFeishuClient(cfg.FeishuBaseURL, cfg.AppID, cfg.AppSecret),
		logger:    logger,
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

func (a *Adapter) Close() error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		a.pollMu.Lock()
		a.closing = true
		a.cancel()
		a.pollMu.Unlock()
		a.pollWG.Wait()
		if a.store != nil {
			a.closeErr = a.store.Close()
		}
	})
	return a.closeErr
}

func (a *Adapter) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/feishu/events", a.handleCallback)
	mux.HandleFunc("/feishu/card-callback", a.handleCallback)
	return mux
}

func (a *Adapter) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cb, err := ParseCallback(body, a.cfg.VerificationToken)
	if err != nil {
		a.logger.Printf("feishu callback ignored: %v", err)
		if errors.Is(err, errCallbackAuthentication) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "invalid feishu callback authentication",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ignored": err.Error()})
		return
	}
	if cb.Challenge != "" {
		writeJSON(w, http.StatusOK, map[string]string{"challenge": cb.Challenge})
		return
	}
	if cb.Action != nil {
		action := *cb.Action
		if err := a.handleAction(r.Context(), action); err != nil {
			a.logger.Printf("feishu action failed: %v", err)
			writeJSON(w, http.StatusOK, map[string]any{"toast": map[string]string{"type": "error", "content": err.Error()}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"toast": map[string]string{"type": "success", "content": "已提交"}})
		go a.updateApprovalCard(action)
		return
	}
	if cb.Message != nil {
		a.logger.Printf("received feishu message event_id=%s message_id=%s chat_id=%s chat_type=%s", cb.Message.EventID, cb.Message.MessageID, cb.Message.ChatID, cb.Message.ChatType)
		go a.handleMessage(context.Background(), *cb.Message)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Adapter) handleMessage(ctx context.Context, msg MessageEvent) {
	accepted, err := a.store.MarkEvent(msg.EventID)
	if err != nil {
		a.logger.Printf("mark event failed: %v", err)
	}
	if !accepted {
		a.logger.Printf("skip duplicate feishu event event_id=%s message_id=%s", msg.EventID, msg.MessageID)
		return
	}
	if msg.ChatType == "group" && !msg.HasExplicitMention() {
		a.logger.Printf("ignore feishu group message without explicit bot mention event_id=%s message_id=%s", msg.EventID, msg.MessageID)
		return
	}
	a.logger.Printf("processing feishu message event_id=%s message_id=%s chat_id=%s", msg.EventID, msg.MessageID, msg.ChatID)

	text := strings.TrimSpace(msg.Text)
	key := SessionKey(msg)
	switch {
	case text == "":
		a.replyText(ctx, msg.MessageID, "我收到了消息，但没有识别到可处理的文本或图片。")
		return
	case isCommand(text, "/help"):
		a.replyText(ctx, msg.MessageID, helpText())
		return
	case isCommand(text, "/new"):
		_ = a.store.ClearSession(key)
		a.replyText(ctx, msg.MessageID, "已为这个飞书会话开启新的 YourAgent session。")
		return
	case isCommand(text, "/status"):
		a.replyStatus(ctx, msg, key)
		return
	case isCommand(text, "/cancel"):
		a.cancelLastTask(ctx, msg, key)
		return
	}

	mapping, err := a.store.GetMapping(key)
	if err != nil {
		a.replyText(ctx, msg.MessageID, "读取会话映射失败："+err.Error())
		return
	}
	sessionID := ""
	if mapping != nil {
		sessionID = mapping.SessionID
	}
	messageForYourAgent, images, err := a.inputForYourAgent(ctx, msg, text)
	if err != nil {
		a.replyText(ctx, msg.MessageID, "读取飞书图片失败："+err.Error())
		return
	}
	exec, err := a.yourAgent.Execute(ctx, ExecuteRequest{
		Message:     messageForYourAgent,
		Images:      images,
		Mode:        a.cfg.Mode,
		AutoApprove: a.cfg.AutoApprove,
		Async:       true,
		SessionID:   sessionID,
	})
	if err != nil {
		a.replyText(ctx, msg.MessageID, "提交 YourAgent 任务失败："+err.Error())
		return
	}
	if err := a.store.UpsertMapping(Mapping{
		Key:        key,
		TenantKey:  msg.TenantKey,
		ChatID:     msg.ChatID,
		ChatType:   msg.ChatType,
		UserID:     msg.SenderOpenID,
		SessionID:  exec.SessionID,
		LastTaskID: exec.TaskID,
	}); err != nil {
		a.logger.Printf("upsert mapping failed: %v", err)
	}
	a.replyTextInThread(ctx, msg.MessageID, fmt.Sprintf("已提交 YourAgent 任务：%s\n输入 /status 查看进度，/cancel 取消。", exec.TaskID))
	if !a.cfg.DisablePolling {
		a.pollMu.Lock()
		if a.closing {
			a.pollMu.Unlock()
			return
		}
		a.pollWG.Add(1)
		a.pollMu.Unlock()
		go func() {
			defer a.pollWG.Done()
			a.pollTask(a.ctx, msg.ChatID, msg.MessageID, exec.TaskID)
		}()
	}
}

func (a *Adapter) inputForYourAgent(ctx context.Context, msg MessageEvent, text string) (string, []string, error) {
	refID := msg.ReferencedMessageID()
	if refID == "" {
		images, err := a.downloadMessageImages(ctx, msg.MessageID, msg.Resources)
		return text, images, err
	}
	ref, err := a.feishu.GetMessageText(ctx, refID)
	if err != nil {
		a.logger.Printf("load referenced feishu message %s failed: %v", refID, err)
		message := strings.Join([]string{
			"[飞书引用消息]",
			"读取失败：" + err.Error(),
			"",
			"[当前消息]",
			text,
		}, "\n")
		images, imageErr := a.downloadMessageImages(ctx, msg.MessageID, msg.Resources)
		return message, images, imageErr
	}
	refText := truncateRunes(strings.TrimSpace(ref.Text), maxReferencedMessageChars)
	if refText == "" {
		refText = "[空消息]"
	}
	message := strings.Join([]string{
		"[飞书引用消息]",
		refText,
		"",
		"[当前消息]",
		text,
	}, "\n")
	refImages, err := a.downloadMessageImages(ctx, ref.MessageID, ref.Resources)
	if err != nil {
		return "", nil, fmt.Errorf("download referenced message images: %w", err)
	}
	currentImages, err := a.downloadMessageImages(ctx, msg.MessageID, msg.Resources)
	if err != nil {
		return "", nil, fmt.Errorf("download current message images: %w", err)
	}
	images := append(refImages, currentImages...)
	images, err = multimodal.NormalizeAll(
		images,
		multimodal.DefaultMaxImages,
		multimodal.DefaultMaxImageBytes,
		multimodal.DefaultMaxTotalBytes,
	)
	if err != nil {
		return "", nil, err
	}
	return message, images, nil
}

func (a *Adapter) downloadMessageImages(ctx context.Context, messageID string, resources []MessageResource) ([]string, error) {
	images := make([]string, 0, len(resources))
	for _, resource := range resources {
		if !strings.EqualFold(resource.Type, "image") {
			continue
		}
		image, err := a.feishu.DownloadMessageImage(ctx, messageID, resource.Key)
		if err != nil {
			return nil, fmt.Errorf("resource %s: %w", resource.Key, err)
		}
		images = append(images, image)
	}
	return multimodal.NormalizeAll(
		images,
		multimodal.DefaultMaxImages,
		multimodal.DefaultMaxImageBytes,
		multimodal.DefaultMaxTotalBytes,
	)
}

func (a *Adapter) replyStatus(ctx context.Context, msg MessageEvent, key string) {
	mapping, err := a.store.GetMapping(key)
	if err != nil || mapping == nil || mapping.LastTaskID == "" {
		a.replyText(ctx, msg.MessageID, "当前飞书会话没有正在跟踪的 YourAgent 任务。")
		return
	}
	status, err := a.yourAgent.Status(ctx, mapping.LastTaskID)
	if err != nil {
		a.replyText(ctx, msg.MessageID, "查询任务失败："+err.Error())
		return
	}
	a.replyText(ctx, msg.MessageID, formatStatus(status))
}

func (a *Adapter) cancelLastTask(ctx context.Context, msg MessageEvent, key string) {
	mapping, err := a.store.GetMapping(key)
	if err != nil || mapping == nil || mapping.LastTaskID == "" {
		a.replyText(ctx, msg.MessageID, "当前飞书会话没有可取消的任务。")
		return
	}
	if err := a.yourAgent.Cancel(ctx, mapping.LastTaskID); err != nil {
		a.replyText(ctx, msg.MessageID, "取消任务失败："+err.Error())
		return
	}
	a.replyText(ctx, msg.MessageID, "已发送取消请求："+mapping.LastTaskID)
}

func (a *Adapter) handleAction(ctx context.Context, action ActionEvent) error {
	accepted, err := a.store.MarkEvent(action.EventID)
	if err != nil {
		return err
	}
	if !accepted {
		return nil
	}
	switch action.Action {
	case "approve":
		return a.yourAgent.Approve(ctx, action.TaskID, action.ToolID, true, "")
	case "approve_all":
		return a.yourAgent.ApproveAll(ctx, action.TaskID, action.ToolID, "Approved remaining operations for this task from Feishu")
	case "reject", "deny":
		output := action.Output
		if output == "" {
			output = "Rejected from Feishu"
		}
		return a.yourAgent.Approve(ctx, action.TaskID, action.ToolID, false, output)
	default:
		return fmt.Errorf("unsupported action %q", action.Action)
	}
}

func (a *Adapter) updateApprovalCard(action ActionEvent) {
	if action.CallbackToken == "" && action.MessageID == "" {
		a.logger.Printf("skip feishu approval card update: callback token and message ID are missing task_id=%s tool_id=%s", action.TaskID, action.ToolID)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	approved := action.Action == "approve" || action.Action == "approve_all"
	card := ApprovalResultCard(action, approved)
	if action.CallbackToken != "" {
		if err := a.feishu.UpdateCardByCallbackToken(ctx, action.CallbackToken, card); err == nil {
			return
		} else {
			a.logger.Printf("update feishu approval card by callback token failed task_id=%s tool_id=%s: %v", action.TaskID, action.ToolID, err)
		}
	}
	if action.MessageID != "" {
		if err := a.feishu.UpdateCard(ctx, action.MessageID, card); err != nil {
			a.logger.Printf("update feishu approval card by message ID failed message_id=%s task_id=%s tool_id=%s: %v", action.MessageID, action.TaskID, action.ToolID, err)
		}
	}
}

func (a *Adapter) pollTask(ctx context.Context, chatID, replyToMessageID, taskID string) {
	ctx, cancel := context.WithTimeout(ctx, a.cfg.PollTimeout)
	defer cancel()
	ticker := time.NewTicker(a.cfg.PollInterval)
	defer ticker.Stop()

	approvalSentFor := map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			// Adapter shutdown is intentional: do not emit a misleading timeout
			// card while the process is stopping. A real polling deadline still
			// reports the timeout to the chat.
			if errors.Is(ctx.Err(), context.Canceled) {
				return
			}
			message := "YourAgent 任务轮询超时：" + taskID
			a.replyCardToChat(context.Background(), chatID, resultCard("YourAgent 任务超时", "orange", message), message)
			return
		case <-ticker.C:
		}
		status, err := a.yourAgent.Status(ctx, taskID)
		if err != nil {
			a.logger.Printf("status poll failed for %s: %v", taskID, err)
			continue
		}
		if status.PendingApproval != nil && !approvalSentFor[status.PendingApproval.ToolID] {
			approvalSentFor[status.PendingApproval.ToolID] = true
			card := ApprovalCard(*status.PendingApproval, taskID)
			a.replyCardInThread(ctx, replyToMessageID, card)
			continue
		}
		if status.Complete {
			message := formatFinal(status)
			a.replyCardToChat(context.Background(), chatID, finalResultCard(status), message)
			return
		}
	}
}

func (a *Adapter) replyText(ctx context.Context, messageID, text string) {
	if err := a.feishu.ReplyText(ctx, messageID, text); err != nil {
		a.logger.Printf("send feishu text reply failed message_id=%s: %v", messageID, err)
	}
}

func (a *Adapter) replyTextToChat(ctx context.Context, chatID, text string) {
	if err := a.feishu.SendTextToChat(ctx, chatID, text); err != nil {
		a.logger.Printf("send feishu channel text failed chat_id=%s: %v", chatID, err)
	}
}

func (a *Adapter) replyCardToChat(ctx context.Context, chatID string, card map[string]any, fallback string) {
	if err := a.feishu.SendCardToChat(ctx, chatID, card); err != nil {
		a.logger.Printf("send feishu channel card failed chat_id=%s: %v", chatID, err)
		a.replyTextToChat(ctx, chatID, fallback)
	}
}

func (a *Adapter) replyTextInThread(ctx context.Context, messageID, text string) {
	if err := a.feishu.ReplyTextInThread(ctx, messageID, text); err != nil {
		a.logger.Printf("send feishu thread text reply failed message_id=%s: %v", messageID, err)
	}
}

func (a *Adapter) replyCardInThread(ctx context.Context, messageID string, card map[string]any) {
	if err := a.feishu.ReplyCardInThread(ctx, messageID, card); err != nil {
		a.logger.Printf("send feishu thread card reply failed message_id=%s: %v", messageID, err)
	}
}

func isCommand(text, cmd string) bool {
	return text == cmd || strings.HasPrefix(text, cmd+" ")
}

func helpText() string {
	return strings.Join([]string{
		"YourAgent 飞书机器人命令：",
		"/help 查看帮助",
		"/new 开启新的 YourAgent session",
		"/status 查看当前任务状态",
		"/cancel 取消当前任务",
		"直接发送自然语言即可创建 agent 任务。",
	}, "\n")
}

func formatStatus(status StatusResponse) string {
	lines := []string{
		"YourAgent 任务状态：" + status.Status,
		"Task: " + status.TaskID,
	}
	if status.PendingApproval != nil {
		lines = append(lines, "等待审批："+status.PendingApproval.ToolName)
	}
	if status.Error != "" {
		lines = append(lines, "错误："+status.Error)
	}
	return strings.Join(lines, "\n")
}

func formatFinal(status StatusResponse) string {
	switch status.Status {
	case "paused":
		return trimForFeishu("YourAgent 任务已暂停：\n" + terminalStatusContent(status, "任务已暂停，等待下一步指令。"))
	case "waiting_for_input":
		return trimForFeishu("YourAgent 任务等待输入：\n" + terminalStatusContent(status, "任务需要补充信息后才能继续。"))
	}
	if !status.Success {
		if status.Error != "" {
			return trimForFeishu("YourAgent 任务失败：\n" + formatPlainTextForFeishu(status.Error))
		}
		return "YourAgent 任务失败。"
	}
	if status.Result != "" {
		return trimForFeishu("YourAgent 任务完成：\n" + formatPlainTextForFeishu(status.Result))
	}
	if len(status.Messages) > 0 {
		return trimForFeishu("YourAgent 任务完成：\n" + formatPlainTextForFeishu(strings.Join(status.Messages, "\n")))
	}
	return "YourAgent 任务完成。"
}

func finalResultCard(status StatusResponse) map[string]any {
	switch status.Status {
	case "paused":
		return resultCard(
			"YourAgent 任务已暂停",
			"orange",
			terminalStatusContent(status, "任务已暂停，等待下一步指令。"),
		)
	case "waiting_for_input":
		return resultCard(
			"YourAgent 任务等待输入",
			"blue",
			terminalStatusContent(status, "任务需要补充信息后才能继续。"),
		)
	}
	if !status.Success {
		content := status.Error
		if content == "" {
			content = "任务失败。"
		}
		return resultCard("YourAgent 任务失败", "red", content)
	}

	content := status.Result
	if content == "" && len(status.Messages) > 0 {
		content = strings.Join(status.Messages, "\n")
	}
	if content == "" {
		content = "任务已完成。"
	}
	return resultCard("YourAgent 任务完成", "green", content)
}

func terminalStatusContent(status StatusResponse, fallback string) string {
	if strings.TrimSpace(status.Result) != "" {
		return formatPlainTextForFeishu(status.Result)
	}
	if len(status.Messages) > 0 {
		return formatPlainTextForFeishu(strings.Join(status.Messages, "\n"))
	}
	return fallback
}

func resultCard(title, template, content string) map[string]any {
	elements := buildResultCardElements(content)
	if len(elements) == 0 {
		elements = []any{markdownDiv("任务已完成。")}
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": template,
			"title":    map[string]string{"tag": "plain_text", "content": title},
		},
		"elements": elements,
	}
}

func buildResultCardElements(content string) []any {
	lines := strings.Split(trimForFeishu(content), "\n")
	elements := make([]any, 0, 8)
	paragraph := make([]string, 0, 8)
	codeLines := make([]string, 0, 8)
	inFence := false

	flushParagraph := func() {
		text := strings.TrimSpace(strings.Join(paragraph, "\n"))
		paragraph = paragraph[:0]
		if text != "" {
			appendMarkdownDivs(&elements, text)
		}
	}
	flushCode := func() {
		text := strings.TrimSpace(strings.Join(codeLines, "\n"))
		codeLines = codeLines[:0]
		if text != "" {
			appendMarkdownDivs(&elements, "```\n"+text+"\n```")
		}
	}

	for i := 0; i < len(lines); {
		line := strings.TrimRight(lines[i], " \t")
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			if inFence {
				flushCode()
				inFence = false
			} else {
				flushParagraph()
				inFence = true
			}
			i++
			continue
		}
		if inFence {
			codeLines = append(codeLines, line)
			i++
			continue
		}

		if isTableStart(lines, i) {
			flushParagraph()
			headers := splitTableRow(lines[i])
			i += 2
			rows := make([][]string, 0)
			for i < len(lines) {
				cells, ok := tableCells(lines[i])
				if !ok {
					break
				}
				rows = append(rows, cells)
				i++
			}
			appendTableElements(&elements, headers, rows)
			continue
		}

		if heading, ok := markdownHeadingText(line); ok {
			flushParagraph()
			appendMarkdownDivs(&elements, "**"+normalizeCardInline(heading)+"**")
			i++
			continue
		}

		if trimmed == "" {
			flushParagraph()
			i++
			continue
		}
		paragraph = append(paragraph, normalizeCardMarkdownLine(line))
		i++
	}
	if inFence {
		flushCode()
	}
	flushParagraph()
	return elements
}

func appendTableElements(elements *[]any, headers []string, rows [][]string) {
	if len(rows) == 0 {
		appendMarkdownDivs(elements, normalizeCardInline(strings.Join(headers, " | ")))
		return
	}
	if len(headers) == 2 {
		fields := make([]any, 0, len(rows))
		for _, row := range rows {
			if len(row) < 2 {
				continue
			}
			label := strings.TrimSpace(stripInlineMarkdown(row[0]))
			value := strings.TrimSpace(normalizeCardInline(row[1]))
			if label == "" && value == "" {
				continue
			}
			if label == "" {
				label = "项目"
			}
			if value == "" {
				value = "-"
			}
			fields = append(fields, map[string]any{
				"is_short": len([]rune(stripInlineMarkdown(value))) <= 28,
				"text": map[string]string{
					"tag":     "lark_md",
					"content": "**" + label + "**\n" + value,
				},
			})
		}
		for len(fields) > 0 {
			n := len(fields)
			if n > 10 {
				n = 10
			}
			*elements = append(*elements, map[string]any{
				"tag":    "div",
				"fields": fields[:n],
			})
			fields = fields[n:]
		}
		return
	}

	blocks := make([]string, 0, len(rows))
	for _, row := range rows {
		blocks = append(blocks, formatTableRow(headers, row))
	}
	appendMarkdownDivs(elements, strings.Join(blocks, "\n\n"))
}

func appendMarkdownDivs(elements *[]any, content string) {
	for _, chunk := range chunkRunes(strings.TrimSpace(content), 2800) {
		if chunk != "" {
			*elements = append(*elements, markdownDiv(chunk))
		}
	}
}

func markdownDiv(content string) map[string]any {
	return map[string]any{
		"tag": "div",
		"text": map[string]string{
			"tag":     "lark_md",
			"content": content,
		},
	}
}

func chunkRunes(text string, max int) []string {
	if max <= 0 {
		return []string{text}
	}
	runes := []rune(text)
	if len(runes) <= max {
		return []string{text}
	}
	chunks := make([]string, 0, len(runes)/max+1)
	for len(runes) > 0 {
		n := max
		if len(runes) < n {
			n = len(runes)
		}
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}

// formatCardMarkdown keeps the result content intact while adapting common
// Markdown constructs that are not consistently rendered by legacy cards.
func formatCardMarkdown(content string) string {
	lines := strings.Split(trimForFeishu(content), "\n")
	formatted := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		if !inCodeFence(formatted) && isTableStart(lines, i) {
			headers := splitTableRow(lines[i])
			i += 2 // header and separator
			rows := make([]string, 0)
			for i < len(lines) {
				cells, ok := tableCells(lines[i])
				if !ok {
					break
				}
				rows = append(rows, formatTableRow(headers, cells))
				i++
			}
			if len(rows) == 0 {
				rows = []string{strings.Join(headers, " | ")}
			}
			formatted = append(formatted, strings.Join(rows, "\n\n"))
			continue
		}

		line := lines[i]
		if !inCodeFence(formatted) {
			line = normalizeCardMarkdownLine(line)
		}
		formatted = append(formatted, line)
		i++
	}
	return strings.Join(formatted, "\n")
}

func isTableStart(lines []string, index int) bool {
	if index+1 >= len(lines) {
		return false
	}
	_, headerOK := tableCells(lines[index])
	_, separatorOK := tableCells(lines[index+1])
	if !headerOK || !separatorOK {
		return false
	}
	for _, cell := range splitTableRow(lines[index+1]) {
		cell = strings.Trim(cell, ":")
		if len(cell) < 3 || strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
}

func tableCells(line string) ([]string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.Contains(trimmed, "|") || strings.HasPrefix(trimmed, "```") {
		return nil, false
	}
	return splitTableRow(trimmed), true
}

func splitTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func formatTableRow(headers, cells []string) string {
	if len(cells) >= 3 {
		fields := make([]string, 0, len(cells))
		title := normalizeCardLinks(strings.TrimSpace(stripInlineMarkdown(cells[0])))
		if title != "" {
			fields = append(fields, "**"+title+"**")
		}
		for i := 1; i < len(cells); i++ {
			label := fmt.Sprintf("字段%d", i+1)
			if i < len(headers) && headers[i] != "" {
				label = stripInlineMarkdown(headers[i])
			}
			fields = append(fields, "**"+label+"**："+normalizeCardInline(cells[i]))
		}
		return strings.Join(fields, "\n")
	}

	fields := make([]string, 0, len(cells))
	for i, cell := range cells {
		label := fmt.Sprintf("字段%d", i+1)
		if i < len(headers) && headers[i] != "" {
			label = stripInlineMarkdown(headers[i])
		}
		fields = append(fields, "**"+label+"**："+normalizeCardInline(cell))
	}
	return strings.Join(fields, "\n")
}

func normalizeCardMarkdownLine(line string) string {
	line = normalizeCardLinks(normalizeCardHeading(line))
	line = markdownInlineCodePattern.ReplaceAllString(line, "$1")
	return normalizeMarkdownListMarker(line)
}

func normalizeCardLinks(line string) string {
	return cardMarkdownLinkPattern.ReplaceAllString(line, "<a href='$2'>$1</a>")
}

func normalizeCardInline(line string) string {
	line = normalizeCardLinks(strings.TrimSpace(line))
	line = markdownInlineCodePattern.ReplaceAllString(line, "$1")
	return normalizeMarkdownListMarker(line)
}

func normalizeCardHeading(line string) string {
	if heading, ok := markdownHeadingText(line); ok {
		return "**" + heading + "**"
	}
	return line
}

func markdownHeadingText(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	hashCount := 0
	for hashCount < len(trimmed) && trimmed[hashCount] == '#' {
		hashCount++
	}
	if hashCount == 0 || hashCount > 6 || hashCount == len(trimmed) || trimmed[hashCount] != ' ' {
		return "", false
	}
	return strings.TrimSpace(trimmed[hashCount:]), true
}

func formatPlainTextForFeishu(content string) string {
	lines := strings.Split(trimForFeishu(content), "\n")
	out := make([]string, 0, len(lines))
	inFence := false

	for i := 0; i < len(lines); {
		line := strings.TrimRight(lines[i], " \t")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			i++
			continue
		}
		if !inFence && isTableStart(lines, i) {
			headers := splitTableRow(lines[i])
			i += 2
			rows := make([]string, 0)
			for i < len(lines) {
				cells, ok := tableCells(lines[i])
				if !ok {
					break
				}
				if formatted := formatPlainTableRow(headers, cells); formatted != "" {
					rows = append(rows, formatted)
				}
				i++
			}
			for rowIndex, row := range rows {
				if len(headers) >= 3 && rowIndex > 0 {
					out = append(out, "")
				}
				out = append(out, row)
			}
			continue
		}
		if !inFence {
			line = normalizePlainTextLine(line)
		}
		out = append(out, line)
		i++
	}
	return strings.TrimSpace(collapseBlankLines(out))
}

func formatPlainTableRow(headers, cells []string) string {
	if len(headers) == 2 && len(cells) >= 2 {
		label := strings.TrimSpace(stripInlineMarkdown(cells[0]))
		value := strings.TrimSpace(stripInlineMarkdown(cells[1]))
		if label == "" {
			return value
		}
		if value == "" {
			value = "-"
		}
		return label + "：" + value
	}
	if len(cells) >= 3 {
		lines := make([]string, 0, len(cells))
		if title := strings.TrimSpace(stripInlineMarkdown(cells[0])); title != "" {
			lines = append(lines, title)
		}
		for i := 1; i < len(cells); i++ {
			label := fmt.Sprintf("字段%d", i+1)
			if i < len(headers) && strings.TrimSpace(headers[i]) != "" {
				label = stripInlineMarkdown(headers[i])
			}
			lines = append(lines, "  "+label+"："+stripInlineMarkdown(cells[i]))
		}
		return strings.Join(lines, "\n")
	}
	fields := make([]string, 0, len(cells))
	for i, cell := range cells {
		label := fmt.Sprintf("字段%d", i+1)
		if i < len(headers) && strings.TrimSpace(headers[i]) != "" {
			label = stripInlineMarkdown(headers[i])
		}
		fields = append(fields, label+"："+stripInlineMarkdown(cell))
	}
	return strings.Join(fields, "；")
}

func normalizePlainTextLine(line string) string {
	if heading, ok := markdownHeadingText(line); ok {
		return heading
	}
	line = cardMarkdownLinkPattern.ReplaceAllString(line, "$1：$2")
	line = stripInlineMarkdown(line)
	return normalizeMarkdownListMarker(line)
}

func stripInlineMarkdown(line string) string {
	line = markdownBoldPattern.ReplaceAllString(line, "$1")
	line = markdownInlineCodePattern.ReplaceAllString(line, "$1")
	return strings.TrimSpace(line)
}

func normalizeMarkdownListMarker(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(trimmed)]
	if strings.HasPrefix(trimmed, "- ") {
		return indent + "• " + strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
	}
	return line
}

func collapseBlankLines(lines []string) string {
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		out = append(out, line)
		blank = false
	}
	return strings.Join(out, "\n")
}

func inCodeFence(lines []string) bool {
	inFence := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
	}
	return inFence
}

func trimForFeishu(text string) string {
	const max = 12000
	if len(text) <= max {
		return text
	}
	return text[:max-80] + "\n\n[输出过长，已截断；请在 Your Agent session 中查看完整内容]"
}

func truncateRunes(text string, max int) string {
	runes := []rune(text)
	if max <= 0 || len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "\n\n[引用消息过长，已截断]"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
