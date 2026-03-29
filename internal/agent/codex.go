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

// CodexBackend communicates directly with Codex CLI using
// `codex exec --json` for each prompt.
type CodexBackend struct {
	mu    sync.Mutex
	cwd   string
	env   []string
	model string
	cmd   *exec.Cmd
	logger *slog.Logger
}

// CodexOpts configures the Codex CLI backend.
type CodexOpts struct {
	Cwd    string
	Env    map[string]string
	Model  string
	Logger *slog.Logger
}

// NewCodexBackend creates a Backend that calls `codex exec --json` directly.
func NewCodexBackend(opts CodexOpts) *CodexBackend {
	env := mergeEnv(os.Environ(), opts.Env)
	return &CodexBackend{
		cwd:    opts.Cwd,
		env:    env,
		model:  opts.Model,
		logger: opts.Logger,
	}
}

// codexStreamMessage represents a JSON line from `codex exec --json`.
type codexStreamMessage struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id,omitempty"`
	Item     *struct {
		ID   string `json:"id,omitempty"`
		Type string `json:"type,omitempty"`
		Text string `json:"text,omitempty"`
	} `json:"item,omitempty"`
	Usage *struct {
		InputTokens       int `json:"input_tokens,omitempty"`
		CachedInputTokens int `json:"cached_input_tokens,omitempty"`
		OutputTokens      int `json:"output_tokens,omitempty"`
	} `json:"usage,omitempty"`
}

func (b *CodexBackend) Prompt(ctx context.Context, text string, onText func(chunk string)) (*PromptResult, error) {
	args := []string{"exec", "--json"}

	b.mu.Lock()
	if b.model != "" {
		args = append(args, "--model", b.model)
	}
	b.mu.Unlock()

	args = append(args, text)

	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Dir = b.cwd
	cmd.Env = b.env

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex: %w", err)
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

		b.logger.Debug("codex_stream_line", "line", truncate(line, 200))

		var msg codexStreamMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			b.logger.Debug("codex_stream_parse_error", "error", err)
			continue
		}

		switch msg.Type {
		case "item.completed":
			if msg.Item != nil && msg.Item.Type == "agent_message" && msg.Item.Text != "" {
				resultText.WriteString(msg.Item.Text)
				if onText != nil {
					onText(msg.Item.Text)
				}
			}

		case "turn.completed":
			if msg.Usage != nil {
				b.logger.Debug("codex_usage",
					"input_tokens", msg.Usage.InputTokens,
					"output_tokens", msg.Usage.OutputTokens,
				)
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return &PromptResult{Text: resultText.String(), StopReason: "cancelled"}, nil
		}
		if resultText.Len() > 0 {
			return &PromptResult{Text: resultText.String(), StopReason: "error"}, nil
		}
		errDetail := stderrBuf.String()
		if errDetail != "" {
			return nil, fmt.Errorf("codex error: %s", strings.TrimSpace(errDetail))
		}
		return nil, fmt.Errorf("codex exited: %w", err)
	}

	return &PromptResult{
		Text:       strings.TrimSpace(resultText.String()),
		StopReason: stopReason,
	}, nil
}

func (b *CodexBackend) Kill() {
	b.mu.Lock()
	cmd := b.cmd
	b.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
