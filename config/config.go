package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// ClaudeSettings mirrors ~/.claude/settings.json
type ClaudeSettings struct {
	Env map[string]string `json:"env"`
}

// Config holds the application configuration.
type Config struct {
	Port         string
	AnthropicURL string
	AnthropicKey string
	DefaultModel string
}

// Load reads configuration, with Claude Code settings as fallback.
func Load() *Config {
	cfg := &Config{
		Port: getEnv("PORT", "45376"),
	}

	claudeSettings := loadClaudeSettings()

	// API Key: env var > claude settings
	cfg.AnthropicKey = getEnv("ANTHROPIC_API_KEY", "")
	if cfg.AnthropicKey == "" {
		cfg.AnthropicKey = getEnv("ANTHROPIC_AUTH_TOKEN", "")
	}
	if cfg.AnthropicKey == "" && claudeSettings != nil {
		cfg.AnthropicKey = claudeSettings.Env["ANTHROPIC_AUTH_TOKEN"]
	}

	// Base URL: env var > claude settings
	cfg.AnthropicURL = getEnv("ANTHROPIC_BASE_URL", "")
	if cfg.AnthropicURL == "" && claudeSettings != nil {
		cfg.AnthropicURL = claudeSettings.Env["ANTHROPIC_BASE_URL"]
	}
	if cfg.AnthropicURL == "" {
		cfg.AnthropicURL = "https://api.anthropic.com"
	}

	// Default model: env var > claude settings
	cfg.DefaultModel = getEnv("ANTHROPIC_MODEL", "")
	if cfg.DefaultModel == "" && claudeSettings != nil {
		cfg.DefaultModel = claudeSettings.Env["ANTHROPIC_MODEL"]
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "claude-sonnet-4-20250514"
	}

	return cfg
}

func loadClaudeSettings() *ClaudeSettings {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	path := filepath.Join(home, ".claude", "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Failed to read claude settings at %s: %v", path, err)
		}
		return nil
	}

	var s ClaudeSettings
	if err := json.Unmarshal(data, &s); err != nil {
		log.Printf("Failed to parse claude settings: %v", err)
		return nil
	}

	return &s
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
