package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/agent"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/app"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	goalstore "github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/goal"
	inputreader "github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/input"
	memorystore "github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/memory"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/planning"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/session"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/tools"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/tui"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/ui"
)

var labels = map[string]string{
	"plan_created":           "结构化计划已生成并校验",
	"plan_resumed":           "已恢复持久化计划",
	"plan_replanned":         "只读计划已依据替代证据重规划",
	"scheduler_wave_started": "Scheduler 已调度就绪步骤",
	"scheduler_wave_failed":  "Scheduler 执行失败",
	"memory_retrieved":       "已按预算检索长期记忆",
	"native_tool_started":    "模型请求原生工具调用",
	"native_tool_succeeded":  "原生工具调用成功",
	"native_tool_failed":     "原生工具调用失败",
	"report_evaluated":       "报告已评估",
	"memory_written":         "已写入长期记忆",
	"context_compacted":      "工作上下文已压缩",
	"goal_continued":         "Goal 已进入下一轮",
	"metrics_recorded":       "运行指标已记录",
	"run_completed":          "任务完成",
	"run_stopped":            "任务停止",
}

type config struct {
	dataDir               string
	maxSteps              int
	maxGoalTurns          int
	tokenBudget           int
	contextObservations   int
	provider              string
	model                 string
	fallbackModels        string
	baseURL               string
	maxOutputTokens       int
	toolTimeout           time.Duration
	toolOutputBytes       int
	approval              string
	interactive           bool
	tui                   bool
	sessionID             string
	sessionTriggerBytes   int
	sessionTriggerTokens  int
	sessionMinRecentTurns int
}

type turnOutput struct {
	result      agent.Result
	runID       string
	logPath     string
	memoryPath  string
	metricsPath string
	successRate float64
	successful  int
	totalRuns   int
	compaction  session.Info
	streamed    bool
}

func main() {
	if err := run(); err != nil {
		ui.New().PrintError("%v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := parseFlags()
	terminal := ui.New()
	summarizer, err := app.NewSessionSummarizer(app.Config{
		Provider: cfg.provider, Model: cfg.model, APIKey: os.Getenv("OPENAI_API_KEY"), BaseURL: cfg.baseURL,
		FallbackModels: splitCSV(cfg.fallbackModels),
	})
	if err != nil {
		return fmt.Errorf("initialize session summarizer: %w", err)
	}
	store, err := session.NewStore(filepath.Join(cfg.dataDir, "sessions.db"), session.Config{
		TriggerBytes: cfg.sessionTriggerBytes, TriggerTokens: cfg.sessionTriggerTokens,
		MinRecentTurns: cfg.sessionMinRecentTurns, Summarizer: summarizer,
	})
	if err != nil {
		return fmt.Errorf("open session store: %w", err)
	}
	defer store.Close()
	goals, err := goalstore.NewStore(filepath.Join(cfg.dataDir, "goals.db"))
	if err != nil {
		return fmt.Errorf("open goal store: %w", err)
	}
	defer goals.Close()

	if cfg.tui {
		return runTUI(cfg, store, goals)
	}
	if cfg.interactive {
		if info, statErr := os.Stdin.Stat(); statErr == nil && info.Mode()&os.ModeCharDevice != 0 {
			reader, readerErr := inputreader.New(filepath.Join(cfg.dataDir, "history"))
			if readerErr != nil {
				return fmt.Errorf("initialize readline: %w", readerErr)
			}
			defer reader.Close()
			terminal.AttachReader(reader)
		}
		return runInteractive(cfg, store, goals, terminal)
	}
	goal := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if goal == "" {
		goal = "解读一篇关于 Agent Memory 的代表性论文"
	}
	sessionID, err := store.Ensure(context.Background(), cfg.sessionID)
	if err != nil {
		return err
	}
	terminal.PrintInfo("目标：%s", goal)
	terminal.PrintInfo("Session：%s", sessionID)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	output, err := executeTurn(ctx, cfg, store, sessionID, goal, terminal, nil, nil, "")
	if err != nil {
		return fmt.Errorf("run agent: %w", err)
	}
	printTurnResult(terminal, output)
	return nil
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.dataDir, "data-dir", envOr("AGENT_DATA_DIR", ".agent-data"), "directory for memory, session, metrics, and trajectory data")
	flag.IntVar(&cfg.maxSteps, "max-steps", 6, "maximum Agent Loop steps per goal turn")
	flag.IntVar(&cfg.maxGoalTurns, "goal-turns", 0, "maximum continuation turns for one goal; 0 means unlimited")
	flag.IntVar(&cfg.tokenBudget, "token-budget", 0, "maximum provider tokens for the run; 0 means unlimited")
	flag.IntVar(&cfg.contextObservations, "context-observations", 4, "recent observations kept before compaction")
	flag.StringVar(&cfg.provider, "provider", envOr("YOUR_AGENT_PROVIDER", "demo"), "model provider: demo or openai")
	flag.StringVar(&cfg.model, "model", os.Getenv("OPENAI_MODEL"), "OpenAI model id; required for provider=openai")
	flag.StringVar(&cfg.fallbackModels, "fallback-models", os.Getenv("OPENAI_FALLBACK_MODELS"), "comma-separated fallback model ids")
	flag.StringVar(&cfg.baseURL, "base-url", os.Getenv("OPENAI_BASE_URL"), "OpenAI-compatible API base URL")
	flag.IntVar(&cfg.maxOutputTokens, "max-output-tokens", 4096, "maximum output tokens per model call")
	flag.DurationVar(&cfg.toolTimeout, "tool-timeout", 30*time.Second, "timeout for each tool call")
	flag.IntVar(&cfg.toolOutputBytes, "tool-output-bytes", 64*1024, "maximum serialized bytes returned by one tool")
	flag.StringVar(&cfg.approval, "approval", "ask", "write/dangerous tool approval: ask, deny, or allow")
	flag.BoolVar(&cfg.interactive, "interactive", false, "start a persistent terminal session")
	flag.BoolVar(&cfg.tui, "tui", false, "start the full-screen terminal UI")
	flag.StringVar(&cfg.sessionID, "session-id", os.Getenv("YOUR_AGENT_SESSION_ID"), "reuse a persisted session")
	flag.IntVar(&cfg.sessionTriggerBytes, "session-trigger-bytes", session.DefaultTriggerBytes, "compact the model session view above this byte count")
	flag.IntVar(&cfg.sessionTriggerTokens, "session-trigger-tokens", session.DefaultTriggerTokens, "compact after a provider input reaches this token count")
	flag.IntVar(&cfg.sessionMinRecentTurns, "session-recent-turns", session.DefaultMinRecentTurns, "user turns retained after session compaction")
	flag.Parse()
	if strings.TrimSpace(cfg.dataDir) == "" {
		cfg.dataDir = ".agent-data"
	}
	return cfg
}

func runInteractive(cfg config, store *session.Store, goals *goalstore.Store, terminal *ui.UI) error {
	sessionID, err := store.Ensure(context.Background(), cfg.sessionID)
	if err != nil {
		return err
	}
	terminal.PrintBanner()
	terminal.PrintInfo("Session %s | provider=%s | model=%s", sessionID, cfg.provider, displayModel(cfg.model))
	terminal.Println("输入 /help 查看命令；运行中按 Ctrl+C 可取消当前任务。")
	var lastOutput *turnOutput

	for {
		line, readErr := terminal.Prompt()
		if line == "" && errors.Is(readErr, io.EOF) {
			terminal.Println("")
			return nil
		}
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		goalRun := false
		resumePlanID := ""
		switch {
		case lower == "/exit" || lower == "/quit" || lower == "/q":
			return nil
		case lower == "/help" || lower == "/h":
			terminal.PrintHelp()
			continue
		case lower == "/paste":
			line, readErr = terminal.PromptMultiline()
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				terminal.PrintError("读取多行输入：%v", readErr)
				continue
			}
			if strings.TrimSpace(line) == "" {
				continue
			}
			lower = strings.ToLower(line)
		case lower == "/new":
			sessionID, err = store.Ensure(context.Background(), "")
			if err != nil {
				terminal.PrintError("new session: %v", err)
				continue
			}
			terminal.PrintSuccess("已开启 Session %s", sessionID)
			continue
		case lower == "/session" || strings.HasPrefix(lower, "/session "):
			sessionID = handleSessionCommand(context.Background(), terminal, store, sessionID, line)
			continue
		case lower == "/status":
			terminal.PrintInfo("provider=%s | model=%s | base_url=%s", cfg.provider, displayModel(cfg.model), displayBaseURL(cfg.baseURL))
			printSessionStatus(context.Background(), terminal, store, sessionID)
			continue
		case lower == "/memory" || strings.HasPrefix(lower, "/memory "):
			handleMemoryCommand(context.Background(), terminal, cfg.dataDir, line)
			continue
		case lower == "/model" || strings.HasPrefix(lower, "/model "):
			handleModelCommand(terminal, store, &cfg, line)
			continue
		case lower == "/todo" || strings.HasPrefix(lower, "/todo "):
			handleTodoCommand(terminal, lastOutput, line)
			continue
		case lower == "/plan" || strings.HasPrefix(lower, "/plan "):
			resumeInput, planID, shouldRun := handlePlanCommand(context.Background(), terminal, cfg.dataDir, sessionID, line)
			if !shouldRun {
				continue
			}
			line = resumeInput
			resumePlanID = planID
		case lower == "/goal status":
			printPersistedGoal(context.Background(), terminal, goals, sessionID)
			continue
		case lower == "/goal pause":
			item, err := goals.Pause(context.Background(), sessionID)
			if err != nil {
				terminal.PrintError("pause goal: %v", err)
			} else {
				terminal.PrintSuccess("Goal %s 已暂停。", item.ID)
			}
			continue
		case lower == "/goal resume" || lower == "/goal resume --auto":
			item, err := goals.Resume(context.Background(), sessionID, strings.HasSuffix(lower, "--auto"))
			if err != nil {
				terminal.PrintError("resume goal: %v", err)
			} else {
				terminal.PrintSuccess("Goal %s 已恢复；auto_resume=%t。", item.ID, item.AutoResume)
			}
			continue
		case lower == "/goal clear":
			if err := goals.Clear(context.Background(), sessionID); err != nil && !errors.Is(err, sql.ErrNoRows) {
				terminal.PrintError("clear goal: %v", err)
			} else {
				lastOutput = nil
				terminal.PrintSuccess("已清除当前 Session 的持久化 Goal；Session 历史保持不变。")
			}
			continue
		case lower == "/goal":
			terminal.PrintWarning("Usage: /goal <objective>")
			continue
		case strings.HasPrefix(lower, "/goal "):
			objective := strings.TrimSpace(line[len("/goal "):])
			if objective == "" {
				terminal.PrintWarning("Usage: /goal <objective>")
				continue
			}
			item, err := goals.Set(context.Background(), sessionID, objective)
			if err != nil {
				terminal.PrintError("set goal: %v", err)
				continue
			}
			line = item.ContinuationPrompt("Start the goal and verify every completion condition.")
			goalRun = true
		case lower == "/cancel":
			terminal.PrintInfo("当前没有运行中的任务；任务运行时按 Ctrl+C 即可取消。")
			continue
		case lower == "/clear":
			terminal.Print("\033[2J\033[H")
			terminal.PrintBanner()
			continue
		}
		if !goalRun && resumePlanID == "" {
			item, err := goals.Get(context.Background(), sessionID)
			switch {
			case err == nil && item.State == goalstore.Paused:
				terminal.PrintWarning("当前 Goal 已暂停；使用 /goal resume 后继续，或 /goal clear 清除。")
				continue
			case err == nil && item.Active():
				line = item.ContinuationPrompt(line)
				goalRun = true
			case err != nil && !errors.Is(err, sql.ErrNoRows):
				terminal.PrintError("load goal: %v", err)
				continue
			}
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		stopSpinner := terminal.Spinner("Your Agent 正在处理")
		output, err := executeTurn(ctx, cfg, store, sessionID, line, terminal, stopSpinner, nil, resumePlanID)
		stopSpinner()
		cancel()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				terminal.PrintWarning("当前任务已取消，Session 保持可继续使用。")
			} else {
				terminal.PrintError("任务失败：%v", err)
			}
			continue
		}
		printTurnResult(terminal, output)
		lastOutput = &output
		if goalRun {
			if _, err := goals.Achieve(context.Background(), sessionID, output.result.Metrics.TotalTokens, output.result.Goal.Turns, "agent completed and evaluator passed"); err != nil {
				terminal.PrintError("persist goal completion: %v", err)
			}
		}
	}
}

func runTUI(cfg config, store *session.Store, goals *goalstore.Store) error {
	sessionID, err := store.Ensure(context.Background(), cfg.sessionID)
	if err != nil {
		return err
	}
	handler := func(ctx context.Context, input string, onDelta func(string)) (string, error) {
		goalRun := false
		prompt := strings.TrimSpace(input)
		if strings.HasPrefix(strings.ToLower(prompt), "/goal ") {
			item, err := goals.Set(ctx, sessionID, strings.TrimSpace(prompt[len("/goal "):]))
			if err != nil {
				return "", err
			}
			prompt = item.ContinuationPrompt("Start the goal and verify every completion condition.")
			goalRun = true
		} else if item, err := goals.Get(ctx, sessionID); err == nil {
			if item.State == goalstore.Paused {
				return "", errors.New("goal is paused; resume it in the standard CLI or Web UI")
			}
			if item.Active() {
				prompt = item.ContinuationPrompt(prompt)
				goalRun = true
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		quiet := ui.New()
		quiet.SetOut(io.Discard)
		quiet.SetErr(io.Discard)
		output, err := executeTurn(ctx, cfg, store, sessionID, prompt, quiet, nil, onDelta, "")
		if err != nil {
			return "", err
		}
		if goalRun {
			if _, err := goals.Achieve(ctx, sessionID, output.result.Metrics.TotalTokens, output.result.Goal.Turns, "agent completed and evaluator passed"); err != nil {
				return "", err
			}
		}
		return output.result.Report, nil
	}
	return tui.Run(context.Background(), sessionID, displayModel(cfg.model), handler)
}

func printSessionStatus(ctx context.Context, terminal *ui.UI, store *session.Store, sessionID string) {
	status, err := store.Status(ctx, sessionID)
	if err != nil {
		terminal.PrintError("session status: %v", err)
		return
	}
	title := status.Title
	if title == "" {
		title = "(untitled)"
	}
	terminal.PrintInfo("session=%s | title=%s | turns=%d | messages=%d | last_status=%s | updated=%s",
		status.SessionID, title, status.TurnCount, status.MessageCount, status.LastTurnStatus, status.UpdatedAt.Local().Format(time.RFC3339))
	terminal.PrintInfo("累计模型 Token=%d（输入 %d / 输出 %d）| 工具调用=%d | 失败=%d | 耗时=%dms",
		status.TotalTokens, status.TotalInputTokens, status.TotalOutputTokens,
		status.TotalToolCalls, status.TotalToolFailures, status.TotalDurationMS)
	terminal.PrintInfo("canonical events=%d | runtime events=%d | successful turns=%d | last_input_tokens=%d",
		status.CanonicalEventCount, status.EventCount, status.SuccessfulTurns, status.LastInputTokens)
	terminal.PrintInfo("summary=%s | calls=%d | tokens=%d（输入 %d / 输出 %d）| latency=%dms",
		status.SummaryState, status.SummaryCalls, status.SummaryInputTokens+status.SummaryOutputTokens,
		status.SummaryInputTokens, status.SummaryOutputTokens, status.SummaryLatencyMS)
}

func handleSessionCommand(ctx context.Context, terminal *ui.UI, store *session.Store, sessionID, command string) string {
	parts := strings.Fields(command)
	if len(parts) == 1 || (len(parts) == 2 && strings.EqualFold(parts[1], "info")) {
		printSessionStatus(ctx, terminal, store, sessionID)
		return sessionID
	}
	switch strings.ToLower(parts[1]) {
	case "list", "ls":
		items, err := store.List(ctx, 10)
		if err != nil {
			terminal.PrintError("list sessions: %v", err)
			return sessionID
		}
		if len(items) == 0 {
			terminal.PrintInfo("没有已保存的 Session。")
			return sessionID
		}
		for _, item := range items {
			marker := " "
			if item.SessionID == sessionID {
				marker = "*"
			}
			title := item.Title
			if title == "" {
				title = "(untitled)"
			}
			terminal.Println("%s %s | %s | %d turns / %d messages | %s | %s", marker, item.SessionID, title,
				item.TurnCount, item.MessageCount, item.LastTurnStatus, item.UpdatedAt.Local().Format("2006-01-02 15:04"))
		}
	case "save":
		if len(parts) < 3 {
			terminal.PrintWarning("Usage: /session save <title>")
			return sessionID
		}
		title := strings.Join(parts[2:], " ")
		if err := store.UpdateTitle(ctx, sessionID, title); err != nil {
			terminal.PrintError("save session title: %v", err)
			return sessionID
		}
		terminal.PrintSuccess("Session 标题已更新：%s", title)
	case "fork":
		title := ""
		if len(parts) > 2 {
			title = strings.Join(parts[2:], " ")
		}
		forkID, err := store.Fork(ctx, sessionID, title)
		if err != nil {
			terminal.PrintError("fork session: %v", err)
			return sessionID
		}
		terminal.PrintSuccess("已 Fork Session %s -> %s", sessionID, forkID)
		return forkID
	default:
		terminal.PrintWarning("Unknown subcommand. Use: /session, /session list, /session save <title>, /session fork [title]")
	}
	return sessionID
}

func handleMemoryCommand(ctx context.Context, terminal *ui.UI, dataDir, command string) {
	path := filepath.Join(dataDir, "memory.db")
	store, err := memorystore.NewStore(path)
	if err != nil {
		terminal.PrintError("open memory: %v", err)
		return
	}
	defer store.Close()
	parts := strings.Fields(command)
	if len(parts) == 1 {
		items, err := store.List(ctx)
		if err != nil {
			terminal.PrintError("list memory: %v", err)
			return
		}
		printMemories(terminal, items, 10)
		return
	}
	switch strings.ToLower(parts[1]) {
	case "add":
		if len(parts) < 3 {
			terminal.PrintWarning("Usage: /memory add <text>")
			return
		}
		value := strings.Join(parts[2:], " ")
		item, err := store.Remember(ctx, domain.MemoryInput{
			Key: fmt.Sprintf("manual_%d", time.Now().UnixNano()), Value: value,
			Source: "cli:user", Confidence: 1, Scope: "project",
		})
		if err != nil {
			terminal.PrintError("add memory: %v", err)
			return
		}
		terminal.PrintSuccess("已写入 Memory %s", item.ID)
	case "search":
		if len(parts) < 3 {
			terminal.PrintWarning("Usage: /memory search <query>")
			return
		}
		query := strings.Join(parts[2:], " ")
		items, err := store.Retrieve(ctx, domain.MemoryQuery{
			Text: query, Scopes: []string{"user", "project", "learning-preference"}, Limit: 8, LimitBytes: 4000,
		})
		if err != nil {
			terminal.PrintError("search memory: %v", err)
			return
		}
		printMemories(terminal, items, 8)
	case "forget":
		if len(parts) != 3 {
			terminal.PrintWarning("Usage: /memory forget <id>")
			return
		}
		if err := store.Forget(ctx, parts[2]); err != nil {
			terminal.PrintError("forget memory: %v", err)
			return
		}
		terminal.PrintSuccess("已归档 Memory %s", parts[2])
	default:
		terminal.PrintWarning("Unknown subcommand. Use: /memory, /memory add <text>, /memory search <query>, /memory forget <id>")
	}
}

func printMemories(terminal *ui.UI, items []domain.Memory, limit int) {
	shown := 0
	for _, item := range items {
		if item.Status != domain.MemoryActive || shown >= limit {
			continue
		}
		terminal.Println("%s | %s | %s | %s", item.ID, item.Scope, item.Key, shorten(item.Value, 120))
		shown++
	}
	if shown == 0 {
		terminal.PrintInfo("没有匹配的长期记忆。")
	}
}

func handleModelCommand(terminal *ui.UI, store *session.Store, cfg *config, command string) {
	parts := strings.Fields(command)
	if len(parts) == 1 || (len(parts) == 2 && strings.EqualFold(parts[1], "list")) {
		terminal.PrintInfo("provider=%s | model=%s | base_url=%s", cfg.provider, displayModel(cfg.model), displayBaseURL(cfg.baseURL))
		return
	}
	if len(parts) == 3 && strings.EqualFold(parts[1], "use") {
		if !strings.EqualFold(cfg.provider, "openai") {
			terminal.PrintWarning("/model use 只在 provider=openai 时生效；当前 provider=%s", cfg.provider)
			return
		}
		modelID := strings.TrimSpace(parts[2])
		summarizer, err := app.NewSessionSummarizer(app.Config{
			Provider: cfg.provider, Model: modelID, FallbackModels: splitCSV(cfg.fallbackModels), APIKey: os.Getenv("OPENAI_API_KEY"), BaseURL: cfg.baseURL,
		})
		if err != nil {
			terminal.PrintError("switch model: %v", err)
			return
		}
		cfg.model = modelID
		store.SetSummarizer(summarizer)
		terminal.PrintSuccess("后续任务将使用模型：%s", cfg.model)
		return
	}
	terminal.PrintWarning("Unknown subcommand. Use: /model, /model list, /model use <id>")
}

func handleTodoCommand(terminal *ui.UI, output *turnOutput, command string) {
	if output == nil {
		terminal.PrintInfo("还没有可展示的结构化计划。")
		return
	}
	parts := strings.Fields(command)
	completed := 0
	for _, step := range output.result.Plan {
		if step.Status == domain.PlanCompleted {
			completed++
		}
	}
	if len(parts) > 1 && strings.EqualFold(parts[1], "stats") {
		terminal.PrintInfo("Plan：%d/%d completed | run status=%s", completed, len(output.result.Plan), output.result.Status)
		return
	}
	for _, step := range output.result.Plan {
		terminal.Println("[%s] %s: %s", step.Status, step.ID, step.Description)
	}
}

func handlePlanCommand(ctx context.Context, terminal *ui.UI, dataDir, sessionID, command string) (string, string, bool) {
	store, err := planning.NewStore(filepath.Join(dataDir, "plans.db"))
	if err != nil {
		terminal.PrintError("open plan store: %v", err)
		return "", "", false
	}
	defer store.Close()
	parts := strings.Fields(command)
	action := "show"
	if len(parts) > 1 {
		action = strings.ToLower(parts[1])
	}
	load := func(optionalIndex int) (planning.Plan, error) {
		if len(parts) > optionalIndex {
			return store.Get(ctx, parts[optionalIndex])
		}
		return store.Latest(ctx, sessionID)
	}
	switch action {
	case "list", "ls":
		items, err := store.List(ctx, sessionID, 20)
		if err != nil {
			terminal.PrintError("list plans: %v", err)
			return "", "", false
		}
		for _, item := range items {
			terminal.Println("%s | %s | %d steps | %s", item.ID, item.Status, len(item.Steps), shorten(item.Objective, 80))
		}
		if len(items) == 0 {
			terminal.PrintInfo("当前 Session 没有持久化计划。")
		}
	case "show":
		item, err := load(2)
		if err != nil {
			terminal.PrintError("show plan: %v", err)
			return "", "", false
		}
		printStoredPlan(terminal, item)
	case "resume":
		item, err := load(2)
		if err != nil {
			terminal.PrintError("resume plan: %v", err)
			return "", "", false
		}
		if item.SessionID != sessionID {
			terminal.PrintError("resume plan: plan %s belongs to another Session", item.ID)
			return "", "", false
		}
		return fmt.Sprintf("Resume persisted plan %s: %s", item.ID, item.Objective), item.ID, true
	case "accept":
		if len(parts) < 4 {
			terminal.PrintWarning("Usage: /plan accept <plan-id> <step-id> [evidence]")
			return "", "", false
		}
		evidence := "accepted in CLI"
		if len(parts) > 4 {
			evidence = strings.Join(parts[4:], " ")
		}
		item, err := (planning.Scheduler{Store: store}).Accept(ctx, parts[2], parts[3], evidence)
		if err != nil {
			terminal.PrintError("accept step: %v", err)
			return "", "", false
		}
		terminal.PrintSuccess("Plan %s / step %s 已人工验收；状态=%s。", item.ID, parts[3], item.Status)
	default:
		terminal.PrintWarning("Use: /plan [show] [id], /plan list, /plan resume [id], /plan accept <id> <step> [evidence]")
	}
	return "", "", false
}

func printStoredPlan(terminal *ui.UI, item planning.Plan) {
	terminal.PrintInfo("Plan %s | status=%s | run=%s", item.ID, item.Status, item.RunID)
	terminal.Println("%s", item.Objective)
	for _, step := range item.Steps {
		terminal.Println("[%s] %s (%s): %s", step.Status, step.ID, step.AgentRole, step.Description)
		for _, evidence := range step.Evidence {
			terminal.Println("  evidence: %s", evidence)
		}
	}
}

func printPersistedGoal(ctx context.Context, terminal *ui.UI, store *goalstore.Store, sessionID string) {
	item, err := store.Get(ctx, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		terminal.PrintInfo("当前 Session 没有持久化 Goal。")
		return
	}
	if err != nil {
		terminal.PrintError("goal status: %v", err)
		return
	}
	terminal.PrintInfo("Goal %s | state=%s | auto_resume=%t | turns=%d | tokens=%d",
		item.ID, item.State, item.AutoResume, item.Iterations, item.TokensUsed)
	terminal.Println("%s", item.Objective)
}

func shorten(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

func executeTurn(
	ctx context.Context,
	cfg config,
	store *session.Store,
	sessionID string,
	input string,
	terminal *ui.UI,
	stopProgress func(),
	onStream func(string),
	resumePlanID string,
) (turnOutput, error) {
	view, err := store.BuildPrompt(ctx, sessionID, input)
	if err != nil {
		return turnOutput{}, err
	}
	var output turnOutput
	output.compaction = view.Info
	persistedTurn, err := store.BeginTurn(ctx, sessionID, "")
	if err != nil {
		return turnOutput{}, err
	}
	if err := persistedTurn.AddUser(input, nil); err != nil {
		return turnOutput{}, err
	}
	if view.Info.Compacted {
		layer := "L1 + L2"
		if view.Info.LLMSummaryUsed {
			layer = "L1 + L2 + L3"
		}
		terminal.PrintInfo("Session 压缩（%s）：丢弃送模消息 %d 条，%d -> %d bytes；完整历史仍在 SQLite。",
			layer, view.Info.DroppedMessages, view.Info.BytesBefore, view.Info.BytesAfter)
	}

	var runErr error
	var persistErr error
	var persistMu sync.Mutex
	recordPersistErr := func(err error) {
		if err == nil {
			return
		}
		persistMu.Lock()
		if persistErr == nil {
			persistErr = err
		}
		persistMu.Unlock()
	}
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			minRecent := 1
			if attempt == 2 {
				minRecent = 0
			}
			view, err = store.ForcePrompt(ctx, sessionID, input, minRecent)
			if err != nil {
				runErr = err
				break
			}
			output.compaction = view.Info
			terminal.PrintWarning("上下文超限恢复 %d/2：保留最近 %d 轮后重试。", attempt, minRecent)
		}

		var activeRunID string
		var streamOnce sync.Once
		printer := eventPrinter(terminal, stopProgress)
		runtime, err := app.New(app.Config{
			DataDir: cfg.dataDir, SessionID: sessionID, MaxSteps: cfg.maxSteps, MaxGoalTurns: cfg.maxGoalTurns,
			TokenBudget: cfg.tokenBudget, MaxRecentObservations: cfg.contextObservations,
			Provider: cfg.provider, Model: cfg.model, APIKey: os.Getenv("OPENAI_API_KEY"), BaseURL: cfg.baseURL,
			FallbackModels:  splitCSV(cfg.fallbackModels),
			MaxOutputTokens: cfg.maxOutputTokens, ToolTimeout: cfg.toolTimeout, ToolOutputBytes: cfg.toolOutputBytes,
			WorkDir: ".", Approval: approvalHandler(cfg.approval, terminal), Clarify: clarificationHandler(terminal),
			OnEvent: func(event domain.Event) {
				recordPersistErr(persistedTurn.AddRuntimeEvent(activeRunID, event))
				printer(event)
			},
			OnStream: func(delta string) {
				if onStream != nil {
					output.streamed = true
					onStream(delta)
					return
				}
				streamOnce.Do(func() {
					if stopProgress != nil {
						stopProgress()
					}
					output.streamed = true
					terminal.Println("")
				})
				terminal.Print("%s", delta)
			},
		})
		if err != nil {
			runErr = err
			break
		}
		activeRunID = runtime.RunID
		recordPersistErr(persistedTurn.BindRunID(runtime.RunID))
		var result agent.Result
		if resumePlanID != "" {
			result, err = runtime.Agent.ResumePlanWithSession(ctx, resumePlanID, nil, view.NativeHistory())
		} else {
			result, err = runtime.Agent.RunWithSession(ctx, input, nil, view.NativeHistory())
		}
		recordPersistErr(persistedTurn.MergeMetrics(result.Metrics))
		if err == nil && result.Status != "completed" {
			err = fmt.Errorf("agent stopped: %s", result.Reason)
		}
		persistMu.Lock()
		stagingErr := persistErr
		persistMu.Unlock()
		if err == nil && stagingErr != nil {
			err = stagingErr
		}
		if err == nil {
			summary, summaryErr := runtime.MetricsSummary(ctx)
			if summaryErr != nil {
				_ = runtime.Close()
				runErr = summaryErr
				break
			}
			output.result = result
			output.runID = runtime.RunID
			output.logPath = runtime.LogPath
			output.memoryPath = runtime.MemoryPath
			output.metricsPath = runtime.MetricsPath
			output.successRate = summary.TaskSuccessRate
			output.successful = summary.SuccessfulRuns
			output.totalRuns = summary.Runs
			_ = runtime.Close()
			if err := persistedTurn.AddAssistantText(result.Report); err != nil {
				return turnOutput{}, err
			}
			if err := commitSessionTurn(ctx, persistedTurn, session.TurnCompleted, ""); err != nil {
				return turnOutput{}, err
			}
			return output, nil
		}
		runErr = err
		_ = runtime.Close()
		if !isContextLengthError(err) || attempt == 2 {
			break
		}
	}
	status := sessionStatusForError(ctx, runErr)
	if err := commitSessionTurn(ctx, persistedTurn, status, errorText(runErr)); err != nil {
		return turnOutput{}, errors.Join(runErr, err)
	}
	return turnOutput{}, runErr
}

func commitSessionTurn(ctx context.Context, turn *session.Turn, status session.TurnStatus, failure string) error {
	commitCtx := ctx
	if commitCtx == nil || commitCtx.Err() != nil {
		var cancel context.CancelFunc
		commitCtx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
	}
	return turn.Commit(commitCtx, status, failure)
}

func sessionStatusForError(ctx context.Context, err error) session.TurnStatus {
	if errors.Is(err, context.DeadlineExceeded) || ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return session.TurnTimedOut
	}
	if errors.Is(err, context.Canceled) || ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return session.TurnCancelled
	}
	return session.TurnFailed
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func clarificationHandler(terminal *ui.UI) tools.ClarifyFunc {
	return func(ctx context.Context, question string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return terminal.PromptQuestion(question)
	}
}

func printTurnResult(terminal *ui.UI, output turnOutput) {
	if !output.streamed {
		terminal.Println("")
		terminal.PrintOutput(output.result.Report)
	}
	terminal.Println("")
	terminal.PrintInfo("评估 %.0f%% | 步数 %d | Goal 轮次 %d", output.result.Evaluation.Score*100, output.result.Steps, output.result.Goal.Turns)
	terminal.PrintInfo("模型调用 %d | Token %d（输入 %d / 输出 %d）| 总耗时 %dms",
		output.result.Metrics.LLMCalls, output.result.Metrics.TotalTokens,
		output.result.Metrics.InputTokens, output.result.Metrics.OutputTokens, output.result.Metrics.DurationMS)
	terminal.PrintInfo("Prompt cache 命中 %d tokens | cache 创建 %d tokens",
		output.result.Metrics.CacheReadInputTokens, output.result.Metrics.CacheCreationInputTokens)
	terminal.PrintInfo("工具调用 %d | 失败 %d | 工具耗时 %dms",
		output.result.Metrics.ToolCalls, output.result.Metrics.ToolFailures, output.result.Metrics.ToolDurationMS)
	terminal.PrintInfo("累计任务成功率 %.1f%%（%d/%d）", output.successRate*100, output.successful, output.totalRuns)
	terminal.PrintInfo("Run %s | 轨迹 %s", output.runID, absolute(output.logPath))
}

func eventPrinter(terminal *ui.UI, stopProgress func()) func(domain.Event) {
	return func(event domain.Event) {
		label, exists := labels[event.Type]
		if !exists {
			return
		}
		if stopProgress != nil {
			stopProgress()
		}
		if event.Type == "native_tool_failed" || event.Type == "scheduler_wave_failed" {
			terminal.PrintWarning("[%s] %s", event.Type, label)
			return
		}
		terminal.PrintThinking(fmt.Sprintf("[%s] %s", event.Type, label))
	}
}

func approvalHandler(mode string, terminal *ui.UI) tools.ApprovalFunc {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "allow":
		return func(context.Context, tools.ApprovalRequest) (bool, error) { return true, nil }
	case "deny":
		return func(context.Context, tools.ApprovalRequest) (bool, error) { return false, nil }
	case "ask", "":
		return func(ctx context.Context, request tools.ApprovalRequest) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			info, err := os.Stdin.Stat()
			if err != nil || info.Mode()&os.ModeCharDevice == 0 {
				return false, nil
			}
			return terminal.PromptApproval(request.Tool, request.Risk, request.Args), nil
		}
	default:
		return func(context.Context, tools.ApprovalRequest) (bool, error) {
			return false, fmt.Errorf("unsupported approval mode %q", mode)
		}
	}
}

func isContextLengthError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"context length", "context_length", "maximum context", "too many tokens", "prompt is too long"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func displayModel(model string) string {
	if strings.TrimSpace(model) == "" {
		return "demo"
	}
	return model
}

func displayBaseURL(baseURL string) string {
	if strings.TrimSpace(baseURL) == "" {
		return "provider default"
	}
	return strings.TrimRight(baseURL, "/")
}

func absolute(path string) string {
	value, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return value
}
