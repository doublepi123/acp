package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	dst := filepath.Join(tmpDir, "dst.txt")
	if err := copyFile(src, dst, 0o600); err != nil {
		t.Fatalf("copyFile error = %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("dst content = %q, want hello", string(data))
	}
}

func TestCopyFileSourceNotFound(t *testing.T) {
	err := copyFile("/nonexistent/path.txt", "/tmp/dst.txt", 0o600)
	if err == nil {
		t.Fatalf("copyFile error = nil, want error")
	}
}

func TestAcpCacheDirEnv(t *testing.T) {
	t.Setenv("ACP_CACHE_DIR", "/tmp/acp-test-cache")
	got, err := acpCacheDir()
	if err != nil {
		t.Fatalf("acpCacheDir error = %v", err)
	}
	if got != "/tmp/acp-test-cache" {
		t.Fatalf("acpCacheDir = %q, want /tmp/acp-test-cache", got)
	}
}

func TestAcpCacheDirDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := acpCacheDir()
	if err != nil {
		t.Fatalf("acpCacheDir error = %v", err)
	}
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".cache", "acp")
	if got != expected {
		t.Fatalf("acpCacheDir = %q, want %q", got, expected)
	}
}

func TestCopyCodexConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CODEX_HOME", tmpDir)
	// Write a config.toml in the CODEX_HOME
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[settings]"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	destHome := t.TempDir()
	if err := copyCodexConfig(destHome); err != nil {
		t.Fatalf("copyCodexConfig error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(destHome, "config.toml"))
	if err != nil {
		t.Fatalf("read copied config: %v", err)
	}
	if string(data) != "[settings]" {
		t.Fatalf("copied config = %q", string(data))
	}
}

func TestCopyCodexConfigNoSource(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CODEX_HOME", tmpDir)
	// No config.toml exists
	destHome := t.TempDir()
	err := copyCodexConfig(destHome)
	if err != nil {
		t.Fatalf("copyCodexConfig error = %v (should tolerate missing source)", err)
	}
}

func TestCopyCodexConfigDefaultHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Write a config.toml in the user home .codex dir
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("[settings]"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	destHome := t.TempDir()
	if err := copyCodexConfig(destHome); err != nil {
		t.Fatalf("copyCodexConfig error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(destHome, "config.toml"))
	if err != nil {
		t.Fatalf("read copied config: %v", err)
	}
	if string(data) != "[settings]" {
		t.Fatalf("copied config = %q", string(data))
	}
}

func TestWaitForReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer srv.Close()

	// This is hard to test directly since waitForReady takes port param
	// We can test indirectly through findFreePort + a real server
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("findFreePort error = %v", err)
	}
	_ = port
	// waitForReady with an invalid port should return false
	if waitForReady(1, 50*time.Millisecond) {
		t.Fatalf("waitForReady on port 1 should timeout")
	}
}

func TestPrepareIsolatedCodexHome(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ACP_CACHE_DIR", tmpDir)
	t.Setenv("CODEX_HOME", tmpDir)
	// Create a config.toml for it to copy
	if err := os.WriteFile(filepath.Join(tmpDir, "config.toml"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	codexHome, cleanup, err := prepareIsolatedCodexHome()
	if err != nil {
		t.Fatalf("prepareIsolatedCodexHome error = %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != "test" {
		t.Fatalf("config data = %q, want test", string(data))
	}
}
