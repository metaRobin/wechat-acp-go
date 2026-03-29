# wechat-router-go

Bridge WeChat (via iLink Bot API) to AI coding agents. Single binary, multi-bot, group chat ready.

## Features

- **Multi-Agent**: Built-in presets for Claude, Codex, Gemini, Copilot, Qwen, OpenCode — or bring your own
- **Dynamic Agent Switching**: Users switch agents mid-conversation via `/use claude`, `/use gemini`
- **Multi-bot**: Run multiple WeChat bot instances from a single TOML config
- **Group Chat**: @bot trigger, keyword trigger, or respond-to-all modes
- **Session Management**: Per-user sessions with idle timeout, max concurrency, and queue overflow protection
- **Message Queue**: Non-blocking enqueue with configurable queue size/timeout, busy rejection, and `/cancel` support
- **Multimodal**: Receives images/files from WeChat; sends long code blocks as downloadable files
- **SQLite Persistence**: Session state and message history stored locally, survives restarts
- **Web Management UI**: Built-in HTTP dashboard to view sessions and conversation history
- **Daemon Mode**: Background operation with PID file and log redirect
- **Single Binary**: Pure Go, no CGO required, cross-compiles to linux/amd64 and darwin/arm64

## Quick Start

```bash
# Build
make build

# Run with a built-in agent preset
./wechat-router --agent claude

# Run with web management UI enabled
./wechat-router --agent claude --web

# Run with a custom agent command
./wechat-router --agent "my-agent --flag"

# Run with config file
./wechat-router --config config.toml
```

On first run, a QR code URL is printed. Scan it with WeChat to log in. Credentials are cached for subsequent runs.

## WeChat Commands

Users can send these commands in WeChat chat:

| Command | Description |
|---------|-------------|
| `/use <agent>` | Switch to a different agent (e.g. `/use claude`) |
| `/agents` | List all available agents |
| `/status` | Show current session info |
| `/cancel` | Cancel the in-flight agent request |
| `/clear` | Clear session history and restart |

## Built-in Agent Presets

```bash
./wechat-router agents
```

| ID | Command | Description |
|----|---------|-------------|
| claude | `claude` | Claude Code (Anthropic) |
| codex | `codex` | Codex CLI (OpenAI) |
| copilot | `github-copilot` | GitHub Copilot |
| gemini | `gemini -p` | Gemini CLI (Google) |
| opencode | `opencode run` | OpenCode |
| qwen | `qwen` | Qwen Code (Alibaba) |

## Configuration

Create a `config.toml` file (see `config.example.toml` for full reference):

```toml
[global]
storage_dir = "~/.wechat-router-go"
log_level = "info"

[[bot]]
name = "my-bot"
agent = "claude"

[bot.session]
idle_timeout = "24h"
max_concurrent = 10
queue_size = 3          # max pending messages per session
queue_timeout = "2m"    # max wait time in queue

[bot.group]
enabled = true
trigger = "@bot"  # "@bot", keyword string, or "all"

[bot.web]
enabled = true
addr = "127.0.0.1:8970"
```

### Multi-bot

```toml
[[bot]]
name = "claude-bot"
agent = "claude"

[[bot]]
name = "gemini-bot"
agent = "gemini"
```

### Custom Agent

```toml
[bot.agent_config]
command = "/usr/local/bin/my-agent"
args = ["--mode", "chat"]
cwd = "/home/user/workspace"
show_thoughts = true

[bot.agent_config.env]
MY_API_KEY = "sk-..."
```

## CLI Flags

| Flag | Description |
|------|-------------|
| `--agent` | Agent preset name or raw command |
| `--config` | Path to TOML config file |
| `--cwd` | Working directory for agent |
| `--login` | Force re-login with new QR code |
| `--daemon` | Run in background after login |
| `--idle-timeout` | Session idle timeout (e.g. `30m`, `1h`) |
| `--max-sessions` | Max concurrent user sessions |
| `--show-thoughts` | Forward agent thinking to WeChat |
| `--web` | Enable web management UI |
| `--web-addr` | Web UI listen address (default: `127.0.0.1:8970`) |
| `-v, --verbose` | Enable debug logging |

## Web Management UI

Enable with `--web` flag or `[bot.web] enabled = true` in config. Access at `http://127.0.0.1:8970`.

Features:
- View all sessions (active + historical) with status indicators
- Browse conversation message history
- Delete sessions and their message history
- Auto-refresh every 30 seconds

## Architecture

```
WeChat iLink API
       │
       ▼
   wechatbot.Bot  ──── long-poll messages
       │
       ▼
     Router  ──── session key routing (u:{userID} / g:{groupID})
       │                + command parsing (/use, /cancel, /clear, ...)
       ▼
  SessionManager  ──── goroutine-per-session, bounded channel queue
       │                + idle timeout eviction + queue overflow rejection
       ▼
   Agent Backend  ──── Claude/Codex native SDK or generic CLI (stdin/stdout)
       │
       ▼
  AI Response  ──── streamed back via typing indicator + text/file segments

  Web Server (optional)  ──── GET/DELETE /api/sessions, embedded HTML frontend
```

**Key packages:**

| Package | Purpose |
|---------|---------|
| `cmd/wechat-router` | CLI entry point (cobra) |
| `internal/bridge` | Orchestrates bot + sessions + router + web |
| `internal/router` | Message routing, group trigger, command dispatch |
| `internal/session` | Session lifecycle, manager, SQLite store |
| `internal/agent` | Agent backends (Claude SDK, Codex SDK, generic CLI) |
| `internal/adapter` | WeChat message formatting, code block extraction |
| `internal/config` | TOML config parsing and agent preset resolution |
| `internal/web` | Embedded HTTP management server and frontend |

## Development

```bash
make build       # Build binary
make test        # Run tests with race detector
make lint        # Run go vet + staticcheck
make cross       # Cross-compile (linux/amd64, darwin/arm64)
make clean       # Remove build artifacts
```

## License

MIT
