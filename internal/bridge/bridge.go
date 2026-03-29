package bridge

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	wechatbot "github.com/corespeed-io/wechatbot/golang"

	"github.com/metaRobin/wechat-router-go/internal/adapter"
	agentpkg "github.com/metaRobin/wechat-router-go/internal/agent"
	"github.com/metaRobin/wechat-router-go/internal/config"
	"github.com/metaRobin/wechat-router-go/internal/router"
	"github.com/metaRobin/wechat-router-go/internal/session"
)

// Bridge connects a WeChat bot to AI agent sessions.
type Bridge struct {
	cfg          *config.BotConfig
	defaultAgent string
	customAgents map[string]config.AgentPreset
	storageDir   string
	verbose      bool
	logger       *slog.Logger
	mgr          *session.Manager
	bot          *wechatbot.Bot
}

// New creates a Bridge for a single bot configuration.
func New(cfg *config.BotConfig, defaultAgent string, customAgents map[string]config.AgentPreset, storageDir string, verbose bool, logger *slog.Logger) *Bridge {
	return &Bridge{
		cfg:          cfg,
		defaultAgent: defaultAgent,
		customAgents: customAgents,
		storageDir:   storageDir,
		verbose:      verbose,
		logger:       logger,
	}
}

// Run starts the full bridge lifecycle.
func (b *Bridge) Run(ctx context.Context, forceLogin bool) error {
	b.logger.Info("bridge_starting", "bot", b.cfg.Name, "default_agent", b.defaultAgent)

	credPath := filepath.Join(b.storageDir, b.cfg.Name+".cred.json")

	b.bot = wechatbot.New(wechatbot.Options{
		BaseURL:  b.cfg.WeChat.BaseURL,
		CredPath: credPath,
		LogLevel: "info",
		OnQRURL: func(url string) {
			fmt.Println("Scan this QR code to login:")
			fmt.Println(url)
		},
		OnScanned: func() {
			b.logger.Info("qr_scanned", "bot", b.cfg.Name)
		},
		OnExpired: func() {
			b.logger.Warn("qr_expired", "bot", b.cfg.Name)
		},
		OnError: func(err error) {
			b.logger.Error("bot_error", "bot", b.cfg.Name, "error", err)
		},
	})

	creds, err := b.bot.Login(ctx, forceLogin)
	if err != nil {
		return fmt.Errorf("login %s: %w", b.cfg.Name, err)
	}
	b.logger.Info("logged_in", "bot", b.cfg.Name, "user_id", creds.UserID)

	if err := os.MkdirAll(b.storageDir, 0o755); err != nil {
		return fmt.Errorf("create storage dir: %w", err)
	}

	dbPath := filepath.Join(b.storageDir, b.cfg.Name+".db")
	store, err := session.OpenStore(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	// Build backend registry from all available presets
	registry := b.buildRegistry()

	b.mgr = session.NewManager(session.ManagerOpts{
		Registry:      registry,
		DefaultAgent:  b.defaultAgent,
		HistoryLimit:    b.cfg.Session.HistoryLimit,
		StreamThreshold: 500,
		IdleTimeout:     b.cfg.Session.IdleTimeout.Duration,
		MaxConcurrent: b.cfg.Session.MaxConcurrent,
		OnReply:       b.sendReply,
		SendTyping:    b.sendTyping,
		Store:         store,
		Logger:        b.logger,
	})
	b.mgr.Start()

	r := router.NewRouter(b.cfg.Name, b.cfg.Group, b.mgr, b.bot, b.customAgents, b.logger)
	b.bot.OnMessage(r.Route)

	b.logger.Info("bridge_running", "bot", b.cfg.Name, "agents", len(registry))
	err = b.bot.Run(ctx)

	b.mgr.Stop()
	b.logger.Info("bridge_stopped", "bot", b.cfg.Name)

	return err
}

// buildRegistry creates BackendFactories for all available agent presets.
func (b *Bridge) buildRegistry() session.BackendRegistry {
	registry := make(session.BackendRegistry)

	agentEnv := make(map[string]string)
	for k, v := range b.cfg.AgentCfg.Env {
		agentEnv[k] = v
	}

	// Register all built-in presets
	for id, preset := range config.BuiltInAgents {
		id, preset := id, preset // capture
		registry[id] = b.makeFactory(id, preset, agentEnv)
	}

	// Register custom presets
	for id, preset := range b.customAgents {
		if _, builtin := config.BuiltInAgents[id]; !builtin {
			id, preset := id, preset
			registry[id] = b.makeFactory(id, preset, agentEnv)
		}
	}

	return registry
}

func (b *Bridge) makeFactory(id string, preset config.AgentPreset, agentEnv map[string]string) session.BackendFactory {
	// Merge preset env with bot env
	env := make(map[string]string)
	for k, v := range agentEnv {
		env[k] = v
	}
	for k, v := range preset.Env {
		env[k] = v
	}

	if id == "claude" || preset.Command == "claude" {
		return func(ctx context.Context) (agentpkg.Backend, error) {
			return agentpkg.NewClaudeBackend(agentpkg.ClaudeOpts{
				Cwd:    b.cfg.AgentCfg.Cwd,
				Env:    env,
				Logger: b.logger,
			}), nil
		}
	}

	if id == "codex" || preset.Command == "codex" {
		return func(ctx context.Context) (agentpkg.Backend, error) {
			return agentpkg.NewCodexBackend(agentpkg.CodexOpts{
				Cwd:    b.cfg.AgentCfg.Cwd,
				Env:    env,
				Logger: b.logger,
			}), nil
		}
	}

	// Generic CLI backend for all other agents
	return func(ctx context.Context) (agentpkg.Backend, error) {
		return agentpkg.NewGenericCLIBackend(agentpkg.GenericCLIOpts{
			Command: preset.Command,
			Args:    preset.Args,
			Cwd:     b.cfg.AgentCfg.Cwd,
			Env:     env,
			Logger:  b.logger,
		}), nil
	}
}

// Stop gracefully stops the bridge.
func (b *Bridge) Stop() {
	if b.bot != nil {
		b.bot.Stop()
	}
	if b.mgr != nil {
		b.mgr.Stop()
	}
}

// sendReply formats and sends agent reply text back to WeChat.
func (b *Bridge) sendReply(sessionKey, contextToken, text string) {
	formatted := adapter.FormatForWeChat(text)
	if formatted == "" {
		return
	}
	segments := adapter.SplitText(formatted, 4000)
	for _, seg := range segments {
		if err := b.bot.Send(context.Background(), contextToken, seg); err != nil {
			b.logger.Error("send_failed", "target", contextToken, "error", err)
		}
	}
	_ = b.bot.StopTyping(context.Background(), contextToken)
}

// sendTyping sends a typing indicator to the reply target.
func (b *Bridge) sendTyping(sessionKey, contextToken string) {
	if err := b.bot.SendTyping(context.Background(), contextToken); err != nil {
		b.logger.Debug("typing_failed", "target", contextToken, "error", err)
	}
}
