# acp — Anthropic Codex Proxy

Translates OpenAI Response API ↔ Anthropic Messages API, enabling Codex to use Anthropic models.

Config is auto-loaded from `~/.claude/settings.json` — no manual env vars needed.

## 一键安装（推荐）

macOS / Linux：

```bash
curl -fsSL https://raw.githubusercontent.com/doublepi123/acp/main/scripts/install-release.sh | sh
```

默认安装到 `~/.local/bin/acp`。如需自定义目录：

```bash
curl -fsSL https://raw.githubusercontent.com/doublepi123/acp/main/scripts/install-release.sh | INSTALL_DIR=/usr/local/bin sh
```

## 从源码构建

```bash
git clone git@github.com:doublepi123/acp.git
cd acp
make build
```

## 使用

```bash
# 启动代理 + 拉起 Codex（自动读取 ~/.claude/settings.json）
acp codex

# 传递 codex 参数
acp codex "帮我写一个..."

# 升级到最新 release
acp upgrade

# 仅启动代理服务
acp serve
```

## acp codex 做了什么

1. 在随机空闲端口启动代理
2. 轮询 `/health` 确认就绪
3. 注入临时 Codex `model_provider`，指向本地代理的 OpenAI Responses API
4. 使用隔离的临时 `CODEX_HOME`，避免 Codex 读取或修改你的 ChatGPT 登录态
5. Codex 退出后自动关闭代理

`acp codex` 会默认把 Codex 模型设置为当前 `ANTHROPIC_MODEL`。如果 Codex 内部请求
`codex-auto-review`，代理会自动改用当前默认模型转发。

## 升级

```bash
acp upgrade
```

`acp upgrade` 会按当前系统架构下载 GitHub release 中的 `acp-<os>-<arch>.tar.gz`，
并替换当前正在运行的 `acp` 二进制。可通过 `REPO`, `GITHUB_BASE_URL`, `TAG` 覆盖下载来源。

## 配置来源（优先级从高到低）

| 来源 | 说明 |
|---|---|
| 环境变量 | `ANTHROPIC_API_KEY`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_MODEL` |
| `~/.claude/settings.json` | Claude Code 配置的 `env` 块 |

自动映射的字段：

| Claude 配置 key | 作用 |
|---|---|
| `env.ANTHROPIC_AUTH_TOKEN` | API Key |
| `env.ANTHROPIC_BASE_URL` | 代理目标地址 |
| `env.ANTHROPIC_MODEL` | 默认模型 |

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORT` | `45376` | 服务器端口（仅 serve 模式） |
| `ANTHROPIC_API_KEY` | 从 Claude 配置读取 | Anthropic API 密钥 |
| `ANTHROPIC_BASE_URL` | `https://api.anthropic.com` | Anthropic API 地址 |
| `ANTHROPIC_MODEL` | `claude-sonnet-4-20250514` | 默认模型 |
