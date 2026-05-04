package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWithEnvVars(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "https://test.api.com")
	t.Setenv("ANTHROPIC_MODEL", "claude-test-model")

	cfg := Load()

	if cfg.AnthropicKey != "test-key" {
		t.Fatalf("AnthropicKey = %q, want %q", cfg.AnthropicKey, "test-key")
	}
	if cfg.AnthropicURL != "https://test.api.com" {
		t.Fatalf("AnthropicURL = %q, want %q", cfg.AnthropicURL, "https://test.api.com")
	}
	if cfg.DefaultModel != "claude-test-model" {
		t.Fatalf("DefaultModel = %q, want %q", cfg.DefaultModel, "claude-test-model")
	}
}

func TestLoadDefaults(t *testing.T) {
	// Clear all relevant env vars
	for _, k := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL", "HOST", "PORT"} {
		t.Setenv(k, "")
	}
	// Point HOME to a temp dir with no .claude/settings.json
	t.Setenv("HOME", t.TempDir())

	cfg := Load()

	if cfg.Host != "127.0.0.1" {
		t.Fatalf("Host = %q, want %q", cfg.Host, "127.0.0.1")
	}
	if cfg.Port != "45376" {
		t.Fatalf("Port = %q, want %q", cfg.Port, "45376")
	}
	if cfg.AnthropicKey != "" {
		t.Fatalf("AnthropicKey = %q, want empty", cfg.AnthropicKey)
	}
	if cfg.AnthropicURL != "https://api.anthropic.com" {
		t.Fatalf("AnthropicURL = %q, want %q", cfg.AnthropicURL, "https://api.anthropic.com")
	}
	if cfg.DefaultModel != "claude-sonnet-4-20250514" {
		t.Fatalf("DefaultModel = %q, want %q", cfg.DefaultModel, "claude-sonnet-4-20250514")
	}
}

func TestLoadFromClaudeSettings(t *testing.T) {
	// Clear env vars
	for _, k := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL"} {
		t.Setenv(k, "")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	settings := `{"env":{"ANTHROPIC_AUTH_TOKEN":"claude-key","ANTHROPIC_BASE_URL":"https://claude.api.com","ANTHROPIC_MODEL":"claude-from-settings"}}`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	cfg := Load()

	if cfg.AnthropicKey != "claude-key" {
		t.Fatalf("AnthropicKey = %q, want %q", cfg.AnthropicKey, "claude-key")
	}
	if cfg.AnthropicURL != "https://claude.api.com" {
		t.Fatalf("AnthropicURL = %q, want %q", cfg.AnthropicURL, "https://claude.api.com")
	}
	if cfg.DefaultModel != "claude-from-settings" {
		t.Fatalf("DefaultModel = %q, want %q", cfg.DefaultModel, "claude-from-settings")
	}
}

func TestLoadEnvVarsOverrideClaudeSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	settings := `{"env":{"ANTHROPIC_AUTH_TOKEN":"claude-key","ANTHROPIC_BASE_URL":"https://claude.api.com","ANTHROPIC_MODEL":"claude-model"}}`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	t.Setenv("ANTHROPIC_BASE_URL", "https://env.api.com")
	t.Setenv("ANTHROPIC_MODEL", "env-model")

	cfg := Load()

	if cfg.AnthropicKey != "env-key" {
		t.Fatalf("AnthropicKey = %q, want %q (env should override claude settings)", cfg.AnthropicKey, "env-key")
	}
	if cfg.AnthropicURL != "https://env.api.com" {
		t.Fatalf("AnthropicURL = %q, want %q", cfg.AnthropicURL, "https://env.api.com")
	}
	if cfg.DefaultModel != "env-model" {
		t.Fatalf("DefaultModel = %q, want %q", cfg.DefaultModel, "env-model")
	}
}

func TestLoadCustomHostPort(t *testing.T) {
	t.Setenv("HOST", "0.0.0.0")
	t.Setenv("PORT", "8080")
	t.Setenv("HOME", t.TempDir())

	cfg := Load()

	if cfg.Host != "0.0.0.0" {
		t.Fatalf("Host = %q, want %q", cfg.Host, "0.0.0.0")
	}
	if cfg.Port != "8080" {
		t.Fatalf("Port = %q, want %q", cfg.Port, "8080")
	}
}

func TestLoadMalformedClaudeSettings(t *testing.T) {
	for _, k := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL"} {
		t.Setenv(k, "")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	cfg := Load()

	// Should fall back to defaults
	if cfg.AnthropicURL != "https://api.anthropic.com" {
		t.Fatalf("AnthropicURL = %q, want default", cfg.AnthropicURL)
	}
}
