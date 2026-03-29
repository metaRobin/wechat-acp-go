package acp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sdk "github.com/coder/acp-go-sdk"
)

const typingInterval = 5 * time.Second

// ClientCallbacks holds dynamic per-message callbacks.
type ClientCallbacks struct {
	SendTyping     func() error
	OnThoughtFlush func(text string) error
}

// WeChatACPClient implements sdk.Client for the WeChat bridge.
type WeChatACPClient struct {
	mu            sync.Mutex
	chunks        []string
	thoughtChunks []string
	lastTypingAt  time.Time
	showThoughts  bool
	callbacks     ClientCallbacks
	logger        *slog.Logger
}

func NewClient(showThoughts bool, logger *slog.Logger) *WeChatACPClient {
	return &WeChatACPClient{
		showThoughts: showThoughts,
		logger:       logger,
	}
}

func (c *WeChatACPClient) UpdateCallbacks(cb ClientCallbacks) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.callbacks = cb
}

// Flush returns accumulated text and resets buffers.
func (c *WeChatACPClient) Flush() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushThoughtsLocked()
	text := strings.Join(c.chunks, "")
	c.chunks = nil
	c.lastTypingAt = time.Time{}
	return text
}

// --- sdk.Client interface ---

func (c *WeChatACPClient) SessionUpdate(_ context.Context, params sdk.SessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	u := params.Update

	switch {
	case u.AgentMessageChunk != nil:
		c.flushThoughtsLocked()
		if u.AgentMessageChunk.Content.Text != nil {
			c.chunks = append(c.chunks, u.AgentMessageChunk.Content.Text.Text)
		}
		c.maybeSendTypingLocked()

	case u.AgentThoughtChunk != nil:
		if u.AgentThoughtChunk.Content.Text != nil {
			text := u.AgentThoughtChunk.Content.Text.Text
			c.logger.Debug("thought", "text", truncate(text, 80))
			if c.showThoughts {
				c.thoughtChunks = append(c.thoughtChunks, text)
			}
		}
		c.maybeSendTypingLocked()

	case u.ToolCall != nil:
		c.flushThoughtsLocked()
		c.logger.Info("tool_call", "title", u.ToolCall.Title, "status", u.ToolCall.Status)
		c.maybeSendTypingLocked()

	case u.ToolCallUpdate != nil:
		if u.ToolCallUpdate.Status != nil && *u.ToolCallUpdate.Status == sdk.ToolCallStatus("completed") && u.ToolCallUpdate.Content != nil {
			for _, tc := range u.ToolCallUpdate.Content {
				if tc.Diff != nil {
					var sb strings.Builder
					sb.WriteString("\n```diff\n")
					sb.WriteString("--- " + tc.Diff.Path + "\n")
					if tc.Diff.OldText != nil {
						for _, l := range strings.Split(*tc.Diff.OldText, "\n") {
							sb.WriteString("- " + l + "\n")
						}
					}
					if tc.Diff.NewText != "" {
						for _, l := range strings.Split(tc.Diff.NewText, "\n") {
							sb.WriteString("+ " + l + "\n")
						}
					}
					sb.WriteString("```\n")
					c.chunks = append(c.chunks, sb.String())
				}
			}
		}

	case u.Plan != nil:
		if u.Plan.Entries != nil {
			var sb strings.Builder
			for i, e := range u.Plan.Entries {
				sb.WriteString(fmt.Sprintf("  %d. [%s] %s\n", i+1, e.Status, e.Content))
			}
			c.logger.Info("plan", "entries", sb.String())
		}
	}

	return nil
}

func (c *WeChatACPClient) RequestPermission(_ context.Context, params sdk.RequestPermissionRequest) (sdk.RequestPermissionResponse, error) {
	// Auto-approve: find first allow option
	var optionID sdk.PermissionOptionId
	for _, opt := range params.Options {
		if opt.Kind == "allow_once" || opt.Kind == "allow_always" {
			optionID = opt.OptionId
			break
		}
	}
	if optionID == "" && len(params.Options) > 0 {
		optionID = params.Options[0].OptionId
	}
	if optionID == "" {
		optionID = "allow"
	}

	c.logger.Info("permission_auto_approved", "option", optionID)

	return sdk.RequestPermissionResponse{
		Outcome: sdk.NewRequestPermissionOutcomeSelected(optionID),
	}, nil
}

func (c *WeChatACPClient) ReadTextFile(_ context.Context, params sdk.ReadTextFileRequest) (sdk.ReadTextFileResponse, error) {
	content, err := os.ReadFile(params.Path)
	if err != nil {
		return sdk.ReadTextFileResponse{}, fmt.Errorf("read file %s: %w", params.Path, err)
	}
	return sdk.ReadTextFileResponse{Content: string(content)}, nil
}

func (c *WeChatACPClient) WriteTextFile(_ context.Context, params sdk.WriteTextFileRequest) (sdk.WriteTextFileResponse, error) {
	if err := os.MkdirAll(filepath.Dir(params.Path), 0o755); err != nil {
		return sdk.WriteTextFileResponse{}, err
	}
	if err := os.WriteFile(params.Path, []byte(params.Content), 0o644); err != nil {
		return sdk.WriteTextFileResponse{}, fmt.Errorf("write file %s: %w", params.Path, err)
	}
	return sdk.WriteTextFileResponse{}, nil
}

// Terminal methods: not supported in v1
func (c *WeChatACPClient) CreateTerminal(_ context.Context, _ sdk.CreateTerminalRequest) (sdk.CreateTerminalResponse, error) {
	return sdk.CreateTerminalResponse{}, sdk.NewMethodNotFound("terminal/create")
}

func (c *WeChatACPClient) TerminalOutput(_ context.Context, _ sdk.TerminalOutputRequest) (sdk.TerminalOutputResponse, error) {
	return sdk.TerminalOutputResponse{}, sdk.NewMethodNotFound("terminal/output")
}

func (c *WeChatACPClient) ReleaseTerminal(_ context.Context, _ sdk.ReleaseTerminalRequest) (sdk.ReleaseTerminalResponse, error) {
	return sdk.ReleaseTerminalResponse{}, sdk.NewMethodNotFound("terminal/release")
}

func (c *WeChatACPClient) WaitForTerminalExit(_ context.Context, _ sdk.WaitForTerminalExitRequest) (sdk.WaitForTerminalExitResponse, error) {
	return sdk.WaitForTerminalExitResponse{}, sdk.NewMethodNotFound("terminal/wait_for_exit")
}

func (c *WeChatACPClient) KillTerminalCommand(_ context.Context, _ sdk.KillTerminalCommandRequest) (sdk.KillTerminalCommandResponse, error) {
	return sdk.KillTerminalCommandResponse{}, sdk.NewMethodNotFound("terminal/kill_command")
}

// --- internal helpers ---

func (c *WeChatACPClient) flushThoughtsLocked() {
	if len(c.thoughtChunks) == 0 {
		return
	}
	text := strings.Join(c.thoughtChunks, "")
	c.thoughtChunks = nil
	if strings.TrimSpace(text) == "" {
		return
	}
	if c.callbacks.OnThoughtFlush != nil {
		go func() {
			_ = c.callbacks.OnThoughtFlush("💭 [Thinking]\n" + text)
		}()
	}
}

func (c *WeChatACPClient) maybeSendTypingLocked() {
	if time.Since(c.lastTypingAt) < typingInterval {
		return
	}
	c.lastTypingAt = time.Now()
	if c.callbacks.SendTyping != nil {
		go func() {
			_ = c.callbacks.SendTyping()
		}()
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
