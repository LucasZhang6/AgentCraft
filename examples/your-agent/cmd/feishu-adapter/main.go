package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/integrations/feishu"
)

func main() {
	cfg := feishu.ConfigFromEnv()
	flag.StringVar(&cfg.Addr, "addr", cfg.Addr, "listen address for Feishu callbacks")
	flag.StringVar(&cfg.AppID, "feishu-app-id", cfg.AppID, "Feishu app id (or FEISHU_APP_ID)")
	flag.StringVar(&cfg.AppSecret, "feishu-app-secret", cfg.AppSecret, "Feishu app secret (or FEISHU_APP_SECRET)")
	flag.StringVar(&cfg.VerificationToken, "feishu-verification-token", cfg.VerificationToken, "Feishu verification token (or FEISHU_VERIFICATION_TOKEN)")
	flag.StringVar(&cfg.FeishuBaseURL, "feishu-base-url", cfg.FeishuBaseURL, "Feishu Open Platform base URL")
	flag.StringVar(&cfg.YourAgentBaseURL, "your-agent-url", cfg.YourAgentBaseURL, "Your Agent HTTP API base URL")
	flag.StringVar(&cfg.YourAgentAccessID, "your-agent-access-id", cfg.YourAgentAccessID, "Your Agent access id used to obtain a bearer token")
	flag.StringVar(&cfg.DBPath, "db", cfg.DBPath, "adapter SQLite database path")
	flag.StringVar(&cfg.Mode, "mode", cfg.Mode, "Your Agent mode: chat, agent, or plan")
	flag.DurationVar(&cfg.PollInterval, "poll-interval", cfg.PollInterval, "Your Agent task polling interval")
	flag.DurationVar(&cfg.PollTimeout, "poll-timeout", cfg.PollTimeout, "Your Agent task polling timeout")
	flag.BoolVar(&cfg.AutoApprove, "auto-approve", cfg.AutoApprove, "forward auto_approve=true; use only in trusted environments")
	flag.Parse()

	logger := log.New(os.Stderr, "[feishu-adapter] ", log.LstdFlags)
	adapter, err := feishu.NewAdapter(cfg, logger)
	if err != nil {
		logger.Fatalf("init adapter: %v", err)
	}
	defer adapter.Close()

	resolved := cfg.WithDefaults()
	server := &http.Server{
		Addr:              resolved.Addr,
		Handler:           adapter.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Printf("listening on %s, webhook path /feishu/events, your-agent=%s", server.Addr, resolved.YourAgentBaseURL)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("server failed: %v", err)
	}
}
