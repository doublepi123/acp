# AGENTS.md

## Build & Test

```bash
# Build
GOCACHE=/tmp/go-build-cache go build -buildvcs=false -o acp ./cmd/acp

# Run all tests with coverage (must stay >= 80%)
go test -cover -count=1 ./...

# Single package test
go test -cover -count=1 ./internal/handler/...
```

**Coverage threshold: >= 80% overall.** Run `go test -coverprofile=coverage.out -count=1 ./... && go tool cover -func=coverage.out | tail -1` to verify before committing.

## Architecture

```
cmd/acp/          CLI entry point, Codex launcher, upgrade logic
config/           Config loading from env vars + ~/.claude/settings.json
internal/handler/ HTTP proxy server (OpenAI Responses API → Anthropic Messages API)
internal/translate/ API request/response translation layer
internal/types/   Shared Go types for both APIs
```

- Single binary: `acp` (module: `github.com/doublepi123/acp`)
- Translates OpenAI Response API ↔ Anthropic Messages API
- Entry point: `cmd/acp/main.go`

## Config Priority (high to low)

1. `ANTHROPIC_API_KEY` / `ANTHROPIC_BASE_URL` / `ANTHROPIC_MODEL` env vars
2. `~/.claude/settings.json` → `env.ANTHROPIC_AUTH_TOKEN` / `env.ANTHROPIC_BASE_URL` / `env.ANTHROPIC_MODEL`
3. Defaults: `https://api.anthropic.com`, `claude-sonnet-4-20250514`

## Testing Conventions

- Uses only standard library `testing` (no testify/gomega)
- Table-driven tests, assertions via `t.Fatalf`
- HTTP mocking: implement `RoundTripper` interface (see `handler_test.go` roundTripFunc pattern)
- `internal/handler` tests use `httptest` for request/response recording
- `cmd/acp` tests use `httptest` server for network-dependent upgrade tests
- `CustomTools` / `ApplyPatchTools` fields in `AnthropicMessageRequest` are tagged `json:"-"` — never serialized; used only for internal routing during stream conversion
- Stream conversion logic lives in `internal/handler/handler.go` (not translate), including event state management

## Key Gotchas

- `upgrade.go` binary name: when `GOOS == "windows"`, the code appends `.exe` to the command name automatically — don't pass `acp.exe` as the command name
- The pre-built `acp` binary at repo root is gitignored
- `GOCACHE` is explicitly set to avoid polluting the default cache during builds
