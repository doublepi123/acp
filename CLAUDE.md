# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test

```bash
# Build
make build
# or: GOCACHE=/tmp/go-build-cache go build -buildvcs=false -o acp ./cmd/acp

# Run all tests (coverage must stay >= 80%)
go test -cover -count=1 ./...

# Single package test
go test -cover -count=1 ./internal/handler/...

# Verify coverage threshold
go test -coverprofile=coverage.out -count=1 ./... && go tool cover -func=coverage.out | tail -1
```

No external dependencies — stdlib only. No linter configured.

## Architecture

`acp` is a single-binary Go proxy that translates between the **OpenAI Responses API** format and the **Anthropic Messages API** format, enabling OpenAI's Codex CLI to use Claude models.

```
Codex CLI → OpenAI Responses API → [acp proxy] → Anthropic Messages API → Anthropic API
```

### Package layout

- **`cmd/acp/`** — CLI entry point with 4 subcommands: `serve`, `codex`, `upgrade`, `version`
- **`config/`** — Config loading from env vars + `~/.claude/settings.json`
- **`internal/handler/`** — HTTP proxy server, auth, streaming SSE conversion
- **`internal/translate/`** — Bidirectional API translation (request/response format conversion, tool mapping)
- **`internal/types/`** — Go structs for both OpenAI and Anthropic API types

### Key design decisions

- Streaming conversion is stateful: `streamState` in `handler.go` tracks event sequence, block types, and accumulated content across SSE events
- `CustomTools` and `ApplyPatchTools` fields on `AnthropicMessageRequest` use `json:"-"` tags — they are never serialized to Anthropic, only used for internal routing during stream conversion
- The `codex` subcommand binds to a random free port (pre-binding the listener to avoid TOCTOU), polls `/health` for readiness, creates an isolated `CODEX_HOME`, injects model provider config, forwards signals, and cleans up on exit
- `resolveModel()` substitutes the default model when the request model is empty or `codex-auto-review`
- Self-upgrade via GitHub releases with SHA-256 checksum verification and atomic binary replacement
- On Windows, `upgrade.go` appends `.exe` automatically — don't pass `acp.exe` as the command name

### Config priority (high to low)

1. Env vars: `ANTHROPIC_API_KEY`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_MODEL`
2. `~/.claude/settings.json` → `env.ANTHROPIC_AUTH_TOKEN`, `env.ANTHROPIC_BASE_URL`, `env.ANTHROPIC_MODEL`
3. Defaults: `https://api.anthropic.com`, `claude-sonnet-4-20250514`

## Testing conventions

- Stdlib `testing` only (no testify/gomega)
- Table-driven tests with `t.Fatalf` for assertions
- HTTP mocking: implement `RoundTripper` interface (see `handler_test.go` `roundTripFunc` pattern)
- `internal/handler` tests use `httptest` for request/response recording
- `cmd/acp` tests use `httptest` server for network-dependent upgrade tests