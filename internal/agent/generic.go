package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// GenericCLIBackend runs any CLI command with the prompt as an argument
// and captures stdout as the response. Used for agents without JSON streaming.
type GenericCLIBackend struct {
	mu      sync.Mutex
	command string
	args    []string // base args, prompt is appended
	cwd     string
	env     []string
	cmd     *exec.Cmd
	logger  *slog.Logger
}

// GenericCLIOpts configures the generic CLI backend.
type GenericCLIOpts struct {
	Command string
	Args    []string
	Cwd     string
	Env     map[string]string
	Logger  *slog.Logger
}

// NewGenericCLIBackend creates a Backend that runs a CLI command per prompt.
func NewGenericCLIBackend(opts GenericCLIOpts) *GenericCLIBackend {
	env := mergeEnv(os.Environ(), opts.Env)
	return &GenericCLIBackend{
		command: opts.Command,
		args:    opts.Args,
		cwd:     opts.Cwd,
		env:     env,
		logger:  opts.Logger,
	}
}

func (b *GenericCLIBackend) Prompt(ctx context.Context, text string, onText func(chunk string)) (*PromptResult, error) {
	args := make([]string, len(b.args))
	copy(args, b.args)
	args = append(args, text)

	cmd := exec.CommandContext(ctx, b.command, args...)
	cmd.Dir = b.cwd
	cmd.Env = b.env

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", b.command, err)
	}

	b.mu.Lock()
	b.cmd = cmd
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.cmd = nil
		b.mu.Unlock()
	}()

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return &PromptResult{Text: stdoutBuf.String(), StopReason: "cancelled"}, nil
		}
		if stdoutBuf.Len() > 0 {
			return &PromptResult{Text: strings.TrimSpace(stdoutBuf.String()), StopReason: "error"}, nil
		}
		errDetail := stderrBuf.String()
		if errDetail != "" {
			return nil, fmt.Errorf("%s error: %s", b.command, strings.TrimSpace(errDetail))
		}
		return nil, fmt.Errorf("%s exited: %w", b.command, err)
	}

	result := strings.TrimSpace(stdoutBuf.String())
	if onText != nil && result != "" {
		onText(result)
	}

	return &PromptResult{
		Text:       result,
		StopReason: "end",
	}, nil
}

func (b *GenericCLIBackend) Kill() {
	b.mu.Lock()
	cmd := b.cmd
	b.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
