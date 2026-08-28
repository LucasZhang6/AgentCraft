package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/server"
)

func main() {
	addr := flag.String("addr", envOr("YOUR_AGENT_SERVER_ADDR", "127.0.0.1:18080"), "HTTP listen address")
	dataDir := flag.String("data-dir", envOr("AGENT_DATA_DIR", ".agent-data"), "runtime data directory")
	workDir := flag.String("work-dir", envOr("YOUR_AGENT_WORK_DIR", "."), "workspace root for file, search, shell, and terminal tools")
	accessID := flag.String("access-id", os.Getenv("YOUR_AGENT_ACCESS_ID"), "HTTP access id")
	provider := flag.String("provider", envOr("YOUR_AGENT_PROVIDER", "demo"), "model provider")
	model := flag.String("model", os.Getenv("OPENAI_MODEL"), "OpenAI model id")
	fallbackModels := flag.String("fallback-models", os.Getenv("OPENAI_FALLBACK_MODELS"), "comma-separated fallback model ids")
	baseURL := flag.String("base-url", os.Getenv("OPENAI_BASE_URL"), "OpenAI-compatible base URL")
	maxQueuedTasks := flag.Int("max-queued-tasks", envInt("YOUR_AGENT_MAX_QUEUED_TASKS", 256), "maximum queued HTTP tasks")
	maxSessionTasks := flag.Int("max-session-tasks", envInt("YOUR_AGENT_MAX_SESSION_TASKS", 8), "maximum queued/running tasks per session")
	maxConcurrentTasks := flag.Int("max-concurrent-tasks", envInt("YOUR_AGENT_MAX_CONCURRENT_TASKS", 4), "maximum concurrently running HTTP tasks")
	flag.Parse()

	logger := log.New(os.Stderr, "[your-agent-server] ", log.LstdFlags)
	service, err := server.New(server.Config{
		DataDir: *dataDir, WorkDir: *workDir, AccessID: *accessID, Provider: *provider, Model: *model, FallbackModels: splitCSV(*fallbackModels),
		APIKey: os.Getenv("OPENAI_API_KEY"), BaseURL: *baseURL,
		MaxOutputTokens: 4096, MaxSteps: 6, MaxGoalTurns: 0,
		ToolTimeout: 30 * time.Second, ToolOutputBytes: 64 * 1024, Logger: logger,
		MaxQueuedTasks: *maxQueuedTasks, MaxTasksPerSession: *maxSessionTasks, MaxConcurrentTasks: *maxConcurrentTasks,
	})
	if err != nil {
		logger.Fatalf("initialize: %v", err)
	}
	defer service.Close()
	httpServer := &http.Server{Addr: *addr, Handler: service.Handler(), ReadHeaderTimeout: 5 * time.Second}
	logger.Printf("listening on http://%s", *addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("serve: %v", err)
	}
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

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
