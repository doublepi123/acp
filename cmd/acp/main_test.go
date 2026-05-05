package main

import (
	"testing"
)

func TestHasCodexModelArg(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"no args", nil, false},
		{"no model flag", []string{"hello"}, false},
		{"-m flag", []string{"-m", "claude"}, true},
		{"--model flag", []string{"--model", "claude"}, true},
		{"--model= flag", []string{"--model=claude"}, true},
		{"-m= flag", []string{"-m=claude"}, true},
		{"model after --", []string{"--", "-m", "claude"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasCodexModelArg(tt.args); got != tt.want {
				t.Fatalf("hasCodexModelArg(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestCodexProxyConfigArgs(t *testing.T) {
	args := codexProxyConfigArgs(8080)
	if len(args) != 12 {
		t.Fatalf("len(args) = %d, want 12", len(args))
	}
	if args[0] != "-c" || args[1] != `model_provider="acp"` {
		t.Fatalf("first pair = %q %q, want -c model_provider=\"acp\"", args[0], args[1])
	}
	// Check base_url contains the port
	found := false
	for _, a := range args {
		if a == `model_providers.acp.base_url="http://localhost:8080/v1"` {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("args = %v, want base_url with port 8080", args)
	}
}

func TestCodexArgsWithProxy(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		model string
		port  int
	}{
		{"adds model", nil, "claude-test", 8080},
		{"skips when user passes --model", []string{"--model", "user-model"}, "claude-test", 8080},
		{"empty model", nil, "", 8080},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := codexArgsWithProxy(tt.args, tt.model, tt.port)
			if len(got) < 12 {
				t.Fatalf("len(args) = %d, want at least 12", len(got))
			}
		})
	}
}

func TestFindFreePort(t *testing.T) {
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("findFreePort() error = %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("findFreePort() = %d, want valid port", port)
	}
}
