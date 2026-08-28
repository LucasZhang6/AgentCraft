// Package feishu implements a small sidecar adapter between Feishu callbacks
// and the local YourAgent HTTP API. It intentionally stays outside the core
// directchat/session runtime so Feishu credentials and callback concerns do not
// leak into the CLI agent loop.
package feishu

import (
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	Addr              string
	AppID             string
	AppSecret         string
	VerificationToken string
	FeishuBaseURL     string
	YourAgentBaseURL  string
	YourAgentAccessID string
	DBPath            string
	Mode              string
	PollInterval      time.Duration
	PollTimeout       time.Duration
	AutoApprove       bool

	// DisablePolling is used by tests and controlled runners. Production should
	// leave it false so tasks complete back into Feishu automatically.
	DisablePolling bool
}

func ConfigFromEnv() Config {
	return Config{
		Addr:              envDefault("FEISHU_ADAPTER_ADDR", ":18790"),
		AppID:             os.Getenv("FEISHU_APP_ID"),
		AppSecret:         os.Getenv("FEISHU_APP_SECRET"),
		VerificationToken: os.Getenv("FEISHU_VERIFICATION_TOKEN"),
		FeishuBaseURL:     envDefault("FEISHU_BASE_URL", "https://open.feishu.cn"),
		YourAgentBaseURL:  envDefault("YOUR_AGENT_BASE_URL", "http://127.0.0.1:18080"),
		YourAgentAccessID: os.Getenv("YOUR_AGENT_ACCESS_ID"),
		DBPath:            envDefault("FEISHU_ADAPTER_DB", defaultDBPath()),
		Mode:              envDefault("YOUR_AGENT_FEISHU_MODE", "agent"),
		PollInterval:      durationEnv("FEISHU_ADAPTER_POLL_INTERVAL", 2*time.Second),
		PollTimeout:       durationEnv("FEISHU_ADAPTER_POLL_TIMEOUT", 30*time.Minute),
		AutoApprove:       boolEnv("YOUR_AGENT_FEISHU_AUTO_APPROVE"),
	}
}

func (c Config) WithDefaults() Config {
	if c.Addr == "" {
		c.Addr = ":18790"
	}
	if c.FeishuBaseURL == "" {
		c.FeishuBaseURL = "https://open.feishu.cn"
	}
	if c.YourAgentBaseURL == "" {
		c.YourAgentBaseURL = "http://127.0.0.1:18080"
	}
	if c.DBPath == "" {
		c.DBPath = defaultDBPath()
	}
	if c.Mode == "" {
		c.Mode = "agent"
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 2 * time.Second
	}
	if c.PollTimeout <= 0 {
		c.PollTimeout = 30 * time.Minute
	}
	return c
}

func envDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return d
}

func boolEnv(key string) bool {
	switch os.Getenv(key) {
	case "1", "true", "TRUE", "yes", "YES", "y", "Y":
		return true
	default:
		return false
	}
}

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".your-agent-feishu-adapter.db")
	}
	return filepath.Join(home, ".config", "your-agent", "feishu-adapter.db")
}
