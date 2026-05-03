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

	"github.com/lcy/anthropic-openai-proxy/config"
	"github.com/lcy/anthropic-openai-proxy/internal/handler"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: acp <command> [args...]\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  serve         Start the proxy server\n")
		fmt.Fprintf(os.Stderr, "  codex [args]  Start proxy and launch Codex with proper config\n")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		runServe()
	case "codex":
		runCodex()
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
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", h.HandleResponses)
	mux.HandleFunc("/health", h.HandleHealth)

	addr := ":" + cfg.Port
	log.Printf("acp proxy listening on %s (model: %s)", addr, cfg.DefaultModel)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func runCodex() {
	cfg := config.Load()
	if cfg.AnthropicKey == "" {
		log.Fatal("No API key found. Set ANTHROPIC_API_KEY or configure ~/.claude/settings.json")
	}

	// Find a free port
	port, err := findFreePort()
	if err != nil {
		log.Fatalf("failed to find free port: %v", err)
	}

	h := handler.New(cfg.AnthropicURL, cfg.AnthropicKey, cfg.DefaultModel)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", h.HandleResponses)
	mux.HandleFunc("/health", h.HandleHealth)

	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{Addr: addr, Handler: mux}

	// Start proxy in background
	go func() {
		log.Printf("acp proxy started on http://localhost%s (model: %s)", addr, cfg.DefaultModel)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("proxy error: %v", err)
		}
	}()

	// Wait for proxy to be ready
	if !waitForReady(port, 5*time.Second) {
		log.Fatal("proxy failed to start in time")
	}

	// Build Codex command with environment
	codexArgs := codexArgsWithModel(os.Args[2:], cfg.DefaultModel)
	cmd := exec.Command("codex", codexArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env,
		fmt.Sprintf("OPENAI_BASE_URL=http://localhost:%d/v1", port),
		"OPENAI_API_KEY=acp-proxy",
	)

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	codexDone := make(chan error, 1)
	go func() {
		codexDone <- cmd.Run()
	}()

	select {
	case err := <-codexDone:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				log.Printf("codex exited with code %d", exitErr.ExitCode())
			} else {
				log.Printf("codex error: %v", err)
			}
		}
	case sig := <-sigCh:
		log.Printf("received signal %v, shutting down...", sig)
		if cmd.Process != nil {
			cmd.Process.Signal(sig)
		}
		<-codexDone
	}

	// Shutdown proxy
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("proxy shutdown error: %v", err)
	}
	log.Printf("acp proxy stopped")
}

func codexArgsWithModel(args []string, model string) []string {
	if model == "" || hasCodexModelArg(args) {
		return args
	}

	out := make([]string, 0, len(args)+2)
	out = append(out, "--model", model)
	out = append(out, args...)
	return out
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
		if strings.HasPrefix(arg, "-m") && arg != "-m" {
			return true
		}
	}
	return false
}

func findFreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
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
