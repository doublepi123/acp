package main

import (
	"strings"
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

func TestSetEnvMany(t *testing.T) {
	tests := []struct {
		name       string
		env        []string
		overrides  map[string]string
		wantKeyVal map[string]string
		wantLen    int
	}{
		{
			name:       "empty env",
			env:        nil,
			overrides:  map[string]string{"FOO": "bar"},
			wantKeyVal: map[string]string{"FOO": "bar"},
			wantLen:    1,
		},
		{
			name:       "override existing",
			env:        []string{"FOO=old", "BAR=baz"},
			overrides:  map[string]string{"FOO": "new"},
			wantKeyVal: map[string]string{"FOO": "new", "BAR": "baz"},
			wantLen:    2,
		},
		{
			name:       "add new key",
			env:        []string{"EXISTING=val"},
			overrides:  map[string]string{"NEW": "val"},
			wantKeyVal: map[string]string{"EXISTING": "val", "NEW": "val"},
			wantLen:    2,
		},
		{
			name:       "remove duplicate keys",
			env:        []string{"KEY=first", "KEY=second"},
			overrides:  map[string]string{"KEY": "third"},
			wantKeyVal: map[string]string{"KEY": "third"},
			wantLen:    1,
		},
		{
			name:       "no overrides",
			env:        []string{"A=1", "B=2"},
			overrides:  map[string]string{},
			wantKeyVal: map[string]string{"A": "1", "B": "2"},
			wantLen:    2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := setEnvMany(tt.env, tt.overrides)
			if len(result) != tt.wantLen {
				t.Fatalf("len(result) = %d, want %d", len(result), tt.wantLen)
			}
			got := make(map[string]string)
			for _, e := range result {
				k, v, ok := strings.Cut(e, "=")
				if !ok {
					t.Fatalf("malformed env entry: %q", e)
				}
				got[k] = v
			}
			for k, wantV := range tt.wantKeyVal {
				if got[k] != wantV {
					t.Fatalf("got %s=%q, want %s=%q", k, got[k], k, wantV)
				}
			}
		})
	}
}