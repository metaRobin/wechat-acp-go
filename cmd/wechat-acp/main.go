package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/anthropic/wechat-acp-go/internal/bridge"
	"github.com/anthropic/wechat-acp-go/internal/config"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

func main() {
	var (
		flagAgent       string
		flagCwd         string
		flagConfig      string
		flagLogin       bool
		flagDaemon      bool
		flagIdleTimeout time.Duration
		flagMaxSessions int
		flagShowThoughts bool
		flagVerbose     bool
	)

	rootCmd := &cobra.Command{
		Use:   "wechat-acp-go",
		Short: "Bridge WeChat to any ACP-compatible AI agent",
		Long:  "wechat-acp-go — Bridge WeChat private/group chats to any ACP-compatible AI agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStart(cmd.Context(), startOpts{
				agent:        flagAgent,
				cwd:          flagCwd,
				configFile:   flagConfig,
				forceLogin:   flagLogin,
				daemon:       flagDaemon,
				idleTimeout:  flagIdleTimeout,
				maxSessions:  flagMaxSessions,
				showThoughts: flagShowThoughts,
				verbose:      flagVerbose,
			})
		},
	}

	rootCmd.Flags().StringVar(&flagAgent, "agent", "", "Built-in preset name or raw agent command")
	rootCmd.Flags().StringVar(&flagCwd, "cwd", "", "Working directory for agent")
	rootCmd.Flags().StringVar(&flagConfig, "config", "", "Config file path (TOML)")
	rootCmd.Flags().BoolVar(&flagLogin, "login", false, "Force re-login (new QR code)")
	rootCmd.Flags().BoolVar(&flagDaemon, "daemon", false, "Run in background after login")
	rootCmd.Flags().DurationVar(&flagIdleTimeout, "idle-timeout", 0, "Session idle timeout (e.g. 30m, 1h)")
	rootCmd.Flags().IntVar(&flagMaxSessions, "max-sessions", 0, "Max concurrent user sessions")
	rootCmd.Flags().BoolVar(&flagShowThoughts, "show-thoughts", false, "Forward agent thinking to WeChat")
	rootCmd.Flags().BoolVarP(&flagVerbose, "verbose", "v", false, "Verbose logging")

	rootCmd.AddCommand(agentsCmd())
	rootCmd.AddCommand(stopCmd())
	rootCmd.AddCommand(statusCmd())

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

type startOpts struct {
	agent        string
	cwd          string
	configFile   string
	forceLogin   bool
	daemon       bool
	idleTimeout  time.Duration
	maxSessions  int
	showThoughts bool
	verbose      bool
}

func runStart(ctx context.Context, opts startOpts) error {
	var cfg *config.Config

	if opts.configFile != "" {
		var err error
		cfg, err = config.LoadFile(opts.configFile)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
	} else if opts.agent != "" {
		cfg = config.DefaultConfig()
		botCfg := config.DefaultBotConfig("default", opts.agent)
		cfg.Bot = append(cfg.Bot, botCfg)
	} else {
		return fmt.Errorf("--agent or --config is required")
	}

	// Apply CLI overrides to all bots
	for i := range cfg.Bot {
		b := &cfg.Bot[i]
		if opts.cwd != "" {
			b.AgentCfg.Cwd = opts.cwd
		}
		if opts.idleTimeout > 0 {
			b.Session.IdleTimeout.Duration = opts.idleTimeout
		}
		if opts.maxSessions > 0 {
			b.Session.MaxConcurrent = opts.maxSessions
		}
		if opts.showThoughts {
			b.AgentCfg.ShowThoughts = true
		}
		if opts.daemon {
			b.Daemon.Enabled = true
		}
	}

	// Handle daemon mode
	if opts.daemon && os.Getenv("WECHAT_ACP_DAEMON") == "" {
		return daemonize(cfg)
	}

	// Resolve agents for all bots
	for i := range cfg.Bot {
		b := &cfg.Bot[i]
		if b.AgentCfg.Command == "" && b.Agent != "" {
			resolved := config.ResolveAgent(b.Agent, cfg.Agents)
			if resolved.Command == "" {
				return fmt.Errorf("bot %q: cannot resolve agent %q", b.Name, b.Agent)
			}
			b.AgentCfg.Command = resolved.Command
			b.AgentCfg.Args = resolved.Args
			if resolved.Env != nil {
				if b.AgentCfg.Env == nil {
					b.AgentCfg.Env = make(map[string]string)
				}
				for k, v := range resolved.Env {
					b.AgentCfg.Env[k] = v
				}
			}
		}
	}

	// Setup logger
	logLevel := slog.LevelInfo
	if opts.verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	// Start bridges
	bridges := make([]*bridge.Bridge, 0, len(cfg.Bot))
	for i := range cfg.Bot {
		b := &cfg.Bot[i]
		resolved := config.ResolvedAgent{
			Command: b.AgentCfg.Command,
			Args:    b.AgentCfg.Args,
			Label:   b.Agent,
		}
		br := bridge.New(b, resolved, cfg.Global.StorageDir, opts.verbose, logger.With("bot", b.Name))
		bridges = append(bridges, br)
	}

	errCh := make(chan error, len(bridges))
	for _, b := range bridges {
		b := b
		go func() {
			errCh <- b.Run(ctx, opts.forceLogin)
		}()
	}

	// Wait for context cancellation or first error
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	// Graceful shutdown
	for _, b := range bridges {
		b.Stop()
	}
	return nil
}

func daemonize(cfg *config.Config) error {
	if len(cfg.Bot) == 0 {
		return fmt.Errorf("no bot configured")
	}
	b := &cfg.Bot[0]

	os.MkdirAll(filepath.Dir(b.Daemon.LogFile), 0o755)
	os.MkdirAll(filepath.Dir(b.Daemon.PidFile), 0o755)

	logFile, err := os.OpenFile(b.Daemon.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	args := filterArgs(os.Args[1:], "--daemon")
	cmd := exec.Command(os.Args[0], args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), "WECHAT_ACP_DAEMON=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	os.WriteFile(b.Daemon.PidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
	cmd.Process.Release()

	fmt.Printf("Daemon started (PID %d)\n", cmd.Process.Pid)
	fmt.Printf("Logs: %s\n", b.Daemon.LogFile)
	fmt.Printf("PID file: %s\n", b.Daemon.PidFile)
	os.Exit(0)
	return nil
}

func filterArgs(args []string, remove string) []string {
	result := make([]string, 0, len(args))
	for _, a := range args {
		if a != remove {
			result = append(result, a)
		}
	}
	return result
}

func agentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agents",
		Short: "List built-in agent presets",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Built-in ACP agent presets:")
			fmt.Println()
			for _, entry := range config.ListPresets() {
				cmdLine := entry.Preset.Command + " " + strings.Join(entry.Preset.Args, " ")
				fmt.Printf("  %-10s %s\n", entry.ID, cmdLine)
				if entry.Preset.Description != "" {
					fmt.Printf("  %-10s %s\n", "", entry.Preset.Description)
				}
			}
		},
	}
}

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop a running daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.DefaultConfig()
			pidFile := filepath.Join(cfg.Global.StorageDir, "default.pid")
			return stopDaemon(pidFile)
		},
	}
}

func stopDaemon(pidFile string) error {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		fmt.Println("No daemon running (no PID file found)")
		return nil
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		os.Remove(pidFile)
		return fmt.Errorf("invalid PID file")
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidFile)
		fmt.Printf("Daemon not running (stale PID %d), cleaned up\n", pid)
		return nil
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		os.Remove(pidFile)
		fmt.Printf("Daemon not running (stale PID %d), cleaned up\n", pid)
		return nil
	}

	os.Remove(pidFile)
	fmt.Printf("Stopped daemon (PID %d)\n", pid)
	return nil
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check daemon status",
		Run: func(cmd *cobra.Command, args []string) {
			cfg := config.DefaultConfig()
			pidFile := filepath.Join(cfg.Global.StorageDir, "default.pid")

			data, err := os.ReadFile(pidFile)
			if err != nil {
				fmt.Println("Not running")
				return
			}

			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				fmt.Println("Not running (invalid PID file)")
				os.Remove(pidFile)
				return
			}

			proc, err := os.FindProcess(pid)
			if err != nil {
				fmt.Printf("Not running (stale PID %d)\n", pid)
				os.Remove(pidFile)
				return
			}

			if err := proc.Signal(syscall.Signal(0)); err != nil {
				fmt.Printf("Not running (stale PID %d)\n", pid)
				os.Remove(pidFile)
				return
			}

			fmt.Printf("Running (PID %d)\n", pid)
		},
	}
}
