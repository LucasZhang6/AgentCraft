package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/server"
)

func main() {
	addr := flag.String("addr", envOr("PAPER_AGENT_SERVER_ADDR", "127.0.0.1:18080"), "HTTP listen address")
	dataDir := flag.String("data-dir", envOr("AGENT_DATA_DIR", ".agent-data"), "runtime data directory")
	accessID := flag.String("access-id", os.Getenv("PAPER_AGENT_ACCESS_ID"), "HTTP access id")
	provider := flag.String("provider", envOr("PAPER_AGENT_PROVIDER", "demo"), "model provider")
	model := flag.String("model", os.Getenv("OPENAI_MODEL"), "OpenAI model id")
	fallbackModels := flag.String("fallback-models", os.Getenv("OPENAI_FALLBACK_MODELS"), "comma-separated fallback model ids")
	baseURL := flag.String("base-url", os.Getenv("OPENAI_BASE_URL"), "OpenAI-compatible base URL")
	flag.Parse()

	logger := log.New(os.Stderr, "[paper-agent-server] ", log.LstdFlags)
	service, err := server.New(server.Config{
		DataDir: *dataDir, AccessID: *accessID, Provider: *provider, Model: *model, FallbackModels: splitCSV(*fallbackModels),
		APIKey: os.Getenv("OPENAI_API_KEY"), BaseURL: *baseURL,
		MaxOutputTokens: 4096, MaxSteps: 6, MaxGoalTurns: 0,
		ToolTimeout: 30 * time.Second, ToolOutputBytes: 64 * 1024, Logger: logger,
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

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
