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
	cfg        *config.BotConfig
	resolved   config.ResolvedAgent
	storageDir string
	verbose    bool
	logger     *slog.Logger
	mgr        *session.Manager
	bot        *wechatbot.Bot
}

// New creates a Bridge for a single bot configuration.
func New(cfg *config.BotConfig, resolved config.ResolvedAgent, storageDir string, verbose bool, logger *slog.Logger) *Bridge {
	return &Bridge{
		cfg:        cfg,
		resolved:   resolved,
		storageDir: storageDir,
		verbose:    verbose,
		logger:     logger,
	}
}

// Run starts the full bridge lifecycle.
func (b *Bridge) Run(ctx context.Context, forceLogin bool) error {
	b.logger.Info("bridge_starting", "bot", b.cfg.Name, "agent", b.resolved.Label)

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

	// Build backend factory based on agent type
	backendFactory := b.makeBackendFactory()

	b.mgr = session.NewManager(session.ManagerOpts{
		NewBackend:    backendFactory,
		IdleTimeout:   b.cfg.Session.IdleTimeout.Duration,
		MaxConcurrent: b.cfg.Session.MaxConcurrent,
		OnReply:       b.sendReply,
		SendTyping:    b.sendTyping,
		Store:         store,
		Logger:        b.logger,
	})
	b.mgr.Start()

	r := router.NewRouter(b.cfg.Name, b.cfg.Group, b.mgr, b.bot, b.logger)
	b.bot.OnMessage(r.Route)

	b.logger.Info("bridge_running", "bot", b.cfg.Name)
	err = b.bot.Run(ctx)

	b.mgr.Stop()
	b.logger.Info("bridge_stopped", "bot", b.cfg.Name)

	return err
}

// makeBackendFactory returns a factory that creates the appropriate backend type.
func (b *Bridge) makeBackendFactory() session.BackendFactory {
	agentEnv := make(map[string]string)
	for k, v := range b.cfg.AgentCfg.Env {
		agentEnv[k] = v
	}

	// Use Claude CLI backend for the "claude" preset (direct OAuth, no API key needed)
	if b.resolved.ID == "claude" || b.resolved.Command == "claude" {
		return func(ctx context.Context) (agentpkg.Backend, error) {
			return agentpkg.NewClaudeBackend(agentpkg.ClaudeOpts{
				Cwd:    b.cfg.AgentCfg.Cwd,
				Env:    agentEnv,
				Logger: b.logger,
			}), nil
		}
	}

	// Default: ACP protocol backend
	return func(ctx context.Context) (agentpkg.Backend, error) {
		return agentpkg.NewACPBackend(ctx, agentpkg.ACPOpts{
			Command:      b.resolved.Command,
			Args:         b.resolved.Args,
			Cwd:          b.cfg.AgentCfg.Cwd,
			Env:          agentEnv,
			ShowThoughts: b.cfg.AgentCfg.ShowThoughts,
			Logger:       b.logger,
		})
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
