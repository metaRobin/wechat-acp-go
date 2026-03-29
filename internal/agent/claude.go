package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// ClaudeBackend communicates directly with Claude Code CLI using
// `claude -p --output-format stream-json` and `--resume` for session continuity.
type ClaudeBackend struct {
	mu        sync.Mutex
	sessionID string // Claude Code session ID for --resume
	cwd       string
	env       []string
	logger    *slog.Logger
	model     string
	cmd       *exec.Cmd // current running command (for Kill)
}

// ClaudeOpts configures the Claude Code CLI backend.
type ClaudeOpts struct {
	Cwd    string
	Env    map[string]string
	Model  string // optional: "sonnet", "opus", etc.
	Logger *slog.Logger
}

// NewClaudeBackend creates a Backend that calls `claude` CLI directly.
// Uses OAuth authentication from Claude Code (no API key needed).
func NewClaudeBackend(opts ClaudeOpts) *ClaudeBackend {
	env := mergeEnv(os.Environ(), opts.Env)
	return &ClaudeBackend{
		cwd:    opts.Cwd,
		env:    env,
		model:  opts.Model,
		logger: opts.Logger,
	}
}

// claudeStreamMessage represents a single JSON line from `--output-format stream-json`.
type claudeStreamMessage struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	SessionID string `json:"session_id,omitempty"`

	// For type "assistant"
	Message *struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content,omitempty"`
		StopReason string `json:"stop_reason,omitempty"`
	} `json:"message,omitempty"`

	// For type "content_block_delta"
	Delta *struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"delta,omitempty"`

	// For type "result"
	StopReason   string  `json:"stop_reason,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	DurationMs   int64   `json:"duration_ms,omitempty"`
	Result       string  `json:"result,omitempty"`
	IsError      bool    `json:"is_error,omitempty"`
}

func (b *ClaudeBackend) Prompt(ctx context.Context, text string, onText func(chunk string)) (*PromptResult, error) {
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--permission-mode", "default"}

	b.mu.Lock()
	if b.sessionID != "" {
		args = append(args, "--resume", b.sessionID)
	}
	if b.model != "" {
		args = append(args, "--model", b.model)
	}
	b.mu.Unlock()

	// Append the prompt text
	args = append(args, text)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = b.cwd
	cmd.Env = b.env

	// Capture stderr for error reporting
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude: %w", err)
	}

	b.mu.Lock()
	b.cmd = cmd
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.cmd = nil
		b.mu.Unlock()
	}()

	var resultText strings.Builder
	stopReason := "end"

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		b.logger.Debug("claude_stream_line", "line", truncate(line, 200))

		var msg claudeStreamMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			b.logger.Warn("claude_stream_parse_error", "line", truncate(line, 200), "error", err)
			continue
		}

		// Capture session ID from any message that has it
		if msg.SessionID != "" {
			b.mu.Lock()
			if b.sessionID == "" {
				b.sessionID = msg.SessionID
				b.logger.Debug("claude_session_id", "id", msg.SessionID)
			}
			b.mu.Unlock()
		}

		switch msg.Type {
		case "system":
			// Init message with session info — session_id already captured above

		case "content_block_delta":
			if msg.Delta != nil && msg.Delta.Text != "" {
				resultText.WriteString(msg.Delta.Text)
				if onText != nil {
					onText(msg.Delta.Text)
				}
			}

		case "assistant":
			// Message with content array — extract text blocks
			if msg.Message != nil {
				for _, c := range msg.Message.Content {
					if c.Type == "text" && c.Text != "" {
						resultText.WriteString(c.Text)
						if onText != nil {
							onText(c.Text)
						}
					}
				}
			}

		case "result":
			// Final result — use result text if we didn't capture streaming content
			if msg.Result != "" && resultText.Len() == 0 {
				resultText.WriteString(msg.Result)
			}
			if msg.StopReason != "" {
				stopReason = msg.StopReason
			}
			if msg.IsError {
				stopReason = "error"
			}
			b.logger.Debug("claude_result",
				"cost_usd", msg.TotalCostUSD,
				"duration_ms", msg.DurationMs,
				"subtype", msg.Subtype,
				"stop_reason", msg.StopReason,
			)
		}
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return &PromptResult{Text: resultText.String(), StopReason: "cancelled"}, nil
		}
		// If we got some text, return it with the error note
		if resultText.Len() > 0 {
			return &PromptResult{Text: resultText.String(), StopReason: "error"}, nil
		}
		errDetail := stderrBuf.String()
		if errDetail != "" {
			return nil, fmt.Errorf("claude error: %s", strings.TrimSpace(errDetail))
		}
		return nil, fmt.Errorf("claude exited: %w", err)
	}

	return &PromptResult{
		Text:       strings.TrimSpace(resultText.String()),
		StopReason: stopReason,
	}, nil
}

func (b *ClaudeBackend) Kill() {
	b.mu.Lock()
	cmd := b.cmd
	b.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func mergeEnv(base []string, extra map[string]string) []string {
	env := make([]string, len(base), len(base)+len(extra))
	copy(env, base)
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
