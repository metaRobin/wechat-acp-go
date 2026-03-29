package agent

import (
	"context"
	"log/slog"
	"strings"

	sdk "github.com/coder/acp-go-sdk"

	acputil "github.com/metaRobin/wechat-router-go/internal/acp"
)

// ACPBackend wraps an ACP agent process as a Backend.
type ACPBackend struct {
	client *acputil.WeChatACPClient
	agent  *acputil.AgentProcess
	logger *slog.Logger
}

// ACPOpts configures the ACP backend.
type ACPOpts struct {
	Command      string
	Args         []string
	Cwd          string
	Env          map[string]string
	ShowThoughts bool
	Logger       *slog.Logger
}

// NewACPBackend spawns an ACP agent subprocess and returns a Backend.
func NewACPBackend(ctx context.Context, opts ACPOpts) (*ACPBackend, error) {
	client := acputil.NewClient(opts.ShowThoughts, opts.Logger)

	agent, err := acputil.Spawn(ctx, acputil.SpawnOpts{
		Command: opts.Command,
		Args:    opts.Args,
		Cwd:     opts.Cwd,
		Env:     opts.Env,
		Client:  client,
		Logger:  opts.Logger,
	})
	if err != nil {
		return nil, err
	}

	return &ACPBackend{
		client: client,
		agent:  agent,
		logger: opts.Logger,
	}, nil
}

func (b *ACPBackend) Prompt(ctx context.Context, text string, onText func(chunk string)) (*PromptResult, error) {
	// Set up streaming callback
	b.client.UpdateCallbacks(acputil.ClientCallbacks{
		OnThoughtFlush: func(thought string) error {
			if onText != nil {
				onText(thought)
			}
			return nil
		},
	})

	// Clear previous
	b.client.Flush()

	// Send prompt
	result, err := b.agent.Connection.Prompt(ctx, sdk.PromptRequest{
		SessionId: b.agent.SessionID,
		Prompt:    []sdk.ContentBlock{sdk.TextBlock(text)},
	})
	if err != nil {
		return nil, err
	}

	replyText := b.client.Flush()

	stopReason := "end"
	if result.StopReason == "cancelled" {
		stopReason = "cancelled"
	} else if result.StopReason == "refusal" {
		stopReason = "refusal"
	}

	return &PromptResult{
		Text:       strings.TrimSpace(replyText),
		StopReason: stopReason,
	}, nil
}

func (b *ACPBackend) Kill() {
	if b.agent != nil {
		b.agent.Kill()
	}
}

// OnExit sets a callback for when the agent process exits.
func (b *ACPBackend) OnExit(fn func()) {
	if b.agent != nil {
		b.agent.OnExit = fn
	}
}
