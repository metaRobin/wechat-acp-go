// Package agent defines the AgentBackend interface and implementations
// for communicating with AI coding agents.
package agent

import "context"

// PromptResult holds the response from an agent prompt call.
type PromptResult struct {
	Text       string
	StopReason string // "end", "cancelled", "refusal", "error"
}

// Backend is the interface for interacting with an AI agent.
// Implementations include ACP protocol agents and Claude Code CLI.
type Backend interface {
	// Prompt sends a text prompt and returns the agent's response.
	// The onText callback is called with streamed text chunks as they arrive.
	Prompt(ctx context.Context, text string, onText func(chunk string)) (*PromptResult, error)

	// Kill terminates the agent process/session.
	Kill()
}
