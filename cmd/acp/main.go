package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/doublepi123/acp/config"
	"github.com/doublepi123/acp/internal/handler"
)

// version is set at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: acp <command> [args...]\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  version       Show current version\n")
		fmt.Fprintf(os.Stderr, "  serve         Start the proxy server\n")
		fmt.Fprintf(os.Stderr, "  codex [args]  Start proxy and launch Codex with proper config\n")
		fmt.Fprintf(os.Stderr, "  upgrade       Upgrade acp from the latest release\n")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Printf("acp %s\n", version)
	case "serve":
		runServe()
	case "codex":
		runCodex()
	case "upgrade":
		runUpgrade()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func runServe() {
	cfg := config.Load()
	if cfg.AnthropicKey == "" {
		log.Fatal("No API key found. Set ANTHROPIC_API_KEY or configure ~/.claude/settings.json")
	}

	h := handler.New(cfg.AnthropicURL, cfg.AnthropicKey, cfg.DefaultModel)
	if token := os.Getenv("ACP_PROXY_TOKEN"); token != "" {
		h.WithProxyToken(token)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", h.HandleResponses)
	mux.HandleFunc("/health", h.HandleHealth)

	addr := cfg.Host + ":" + cfg.Port
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("received signal %v, shutting down...", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}()

	log.Printf("acp proxy listening on %s (model: %s)", addr, cfg.DefaultModel)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
	log.Printf("acp proxy stopped")
}

func runCodex() {
	cfg := config.Load()
	if cfg.AnthropicKey == "" {
		log.Fatal("No API key found. Set ANTHROPIC_API_KEY or configure ~/.claude/settings.json")
	}

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("failed to find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	h := handler.New(cfg.AnthropicURL, cfg.AnthropicKey, cfg.DefaultModel)
	if token := os.Getenv("ACP_PROXY_TOKEN"); token != "" {
		h.WithProxyToken(token)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", h.HandleResponses)
	mux.HandleFunc("/health", h.HandleHealth)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Start proxy in background on already-bound listener (avoids TOCTOU race)
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("acp proxy started on http://%s (model: %s)", addr, cfg.DefaultModel)
		serverErr <- srv.Serve(listener)
	}()

	// Wait for proxy to be ready
	if !waitForReady(port, 5*time.Second) {
		select {
		case err := <-serverErr:
			if err != nil && err != http.ErrServerClosed {
				log.Fatalf("proxy failed to start: %v", err)
			}
		default:
		}
		log.Fatal("proxy failed to start in time")
	}
	codexHome, cleanupCodexHome, err := prepareIsolatedCodexHome()
	if err != nil {
		log.Fatalf("failed to prepare Codex home: %v", err)
	}
	defer cleanupCodexHome()

	// Build Codex command with a temporary provider that points at this proxy.
	codexArgs := codexArgsWithProxy(os.Args[2:], cfg.DefaultModel, port)
	cmd := exec.Command("codex", codexArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.Env = setEnvMany(os.Environ(), map[string]string{
		"CODEX_HOME":       codexHome,
		"OPENAI_BASE_URL":  fmt.Sprintf("http://localhost:%d/v1", port),
		"OPENAI_API_KEY":   "acp-proxy",
	})

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	codexDone := make(chan error, 1)
	go func() {
		codexDone <- cmd.Run()
	}()

	exitCode := 0
	select {
	case err := <-codexDone:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
				log.Printf("codex exited with code %d", exitCode)
			} else {
				exitCode = 1
				log.Printf("codex error: %v", err)
			}
		}
	case sig := <-sigCh:
		log.Printf("received signal %v, shutting down...", sig)
		if cmd.Process != nil {
			cmd.Process.Signal(sig)
		}
		select {
		case <-codexDone:
		case <-time.After(30 * time.Second):
			log.Printf("codex did not exit in time, killing...")
			cmd.Process.Kill()
			<-codexDone
		}
	}

	// Shutdown proxy
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("proxy shutdown error: %v", err)
	}
	log.Printf("acp proxy stopped")
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func codexArgsWithProxy(args []string, model string, port int) []string {
	out := make([]string, 0, len(args)+12)
	out = append(out, codexProxyConfigArgs(port)...)

	if model == "" || hasCodexModelArg(args) {
		out = append(out, args...)
		return out
	}

	out = append(out, "--model", model)
	out = append(out, args...)
	return out
}

func codexProxyConfigArgs(port int) []string {
	baseURL := fmt.Sprintf("http://localhost:%d/v1", port)
	return []string{
		"-c", `model_provider="acp"`,
		"-c", `model_providers.acp.name="ACP"`,
		"-c", fmt.Sprintf(`model_providers.acp.base_url="%s"`, baseURL),
		"-c", `model_providers.acp.env_key="OPENAI_API_KEY"`,
		"-c", `model_providers.acp.wire_api="responses"`,
		"-c", `forced_login_method="api"`,
	}
}

func hasCodexModelArg(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "-m" || arg == "--model" {
			return true
		}
		if strings.HasPrefix(arg, "--model=") {
			return true
		}
		if strings.HasPrefix(arg, "-m=") {
			return true
		}
	}
	return false
}

// setEnvMany returns a new environment slice with the given keys set to the
// given values. Existing entries for those keys are replaced (last occurrence
// wins on most platforms, but we remove all prior occurrences to avoid ambiguity).
func setEnvMany(env []string, overrides map[string]string) []string {
	prefixes := make([]string, 0, len(overrides))
	for k := range overrides {
		prefixes = append(prefixes, k+"=")
	}
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		keep := true
		for _, p := range prefixes {
			if strings.HasPrefix(e, p) {
				keep = false
				break
			}
		}
		if keep {
			filtered = append(filtered, e)
		}
	}
	result := make([]string, len(filtered), len(filtered)+len(overrides))
	copy(result, filtered)
	for k, v := range overrides {
		result = append(result, k+"="+v)
	}
	return result
}

func waitForReady(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://localhost:%d/health", port)
	client := &http.Client{Timeout: 500 * time.Millisecond}

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
