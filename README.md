# wechat-acp-go

Bridge WeChat (via iLink Bot API) to any ACP-compatible AI agent. Single binary, multi-bot, group chat ready.

## Features

- **ACP Protocol**: Connects to any agent supporting [Agent Client Protocol](https://github.com/AgenTech/acp) (Claude Code, Copilot, Gemini CLI, etc.)
- **Multi-bot**: Run multiple WeChat bot instances from a single TOML config
- **Group Chat**: @bot trigger, keyword trigger, or respond-to-all modes (requires SDK support)
- **Session Management**: Per-user sessions with idle timeout and max concurrency control
- **SQLite Persistence**: Session state and message history stored locally
- **Daemon Mode**: Background operation with PID file and log redirect
- **Single Binary**: Pure Go, no CGO required, cross-compiles to linux/amd64 and darwin/arm64

## Quick Start

```bash
# Build
make build

# Run with a built-in agent preset
./wechat-acp --agent claude

# Run with a custom agent command
./wechat-acp --agent "my-agent --flag"

# Run with config file
./wechat-acp --config config.toml
```

On first run, a QR code URL is printed. Scan it with WeChat to log in. Credentials are cached for subsequent runs.

## Built-in Agent Presets

```bash
./wechat-acp agents
```

| ID | Command | Description |
|----|---------|-------------|
| copilot | `copilot-cli` | GitHub Copilot CLI |
| claude | `claude --acp` | Claude Code (Anthropic) |
| gemini | `gemini --acp` | Gemini CLI (Google) |
| qwen | `qwen --acp` | Qwen CLI (Alibaba) |
| codex | `codex --acp` | Codex CLI (OpenAI) |
| opencode | `opencode --acp` | OpenCode |

## Configuration

Create a `config.toml` file (see `config.example.toml` for full reference):

```toml
[global]
storage_dir = "~/.wechat-acp-go"
log_level = "info"

[[bot]]
name = "my-bot"
agent = "claude"

[bot.session]
idle_timeout = "24h"
max_concurrent = 10

[bot.group]
enabled = true
trigger = "@bot"  # "@bot", keyword string, or "all"
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

[bot.agent.env]
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
| `-v, --verbose` | Enable debug logging |

## Architecture

```
WeChat iLink API
       │
       ▼
   wechatbot.Bot  ──── long-poll messages
       │
       ▼
     Router  ──── session key routing (u:{userID} / g:{groupID})
       │
       ▼
  SessionManager  ──── goroutine-per-session, buffered channel
       │
       ▼
   ACP Agent  ──── stdio JSON-RPC (acp-go-sdk)
       │
       ▼
  AI Response  ──── streamed back via typing indicator + text segments
```

**Key packages:**

| Package | Purpose |
|---------|---------|
| `cmd/wechat-acp` | CLI entry point (cobra) |
| `internal/bridge` | Orchestrates bot + sessions + router |
| `internal/router` | Message routing and group trigger detection |
| `internal/session` | Session lifecycle, manager, SQLite store |
| `internal/acp` | ACP client implementation and agent process management |
| `internal/adapter` | WeChat ↔ ACP message format conversion |
| `internal/config` | TOML config parsing and agent preset resolution |

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
