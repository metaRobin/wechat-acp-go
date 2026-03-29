package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Global GlobalConfig          `toml:"global"`
	Bot    []BotConfig           `toml:"bot"`
	Agents map[string]AgentPreset `toml:"agents"`
}

type GlobalConfig struct {
	StorageDir string `toml:"storage_dir"`
	LogLevel   string `toml:"log_level"`
}

type BotConfig struct {
	Name     string        `toml:"name"`
	Agent    string        `toml:"agent"`
	WeChat   WeChatConfig  `toml:"wechat"`
	AgentCfg AgentCfg      `toml:"agent_config"`
	Session  SessionConfig `toml:"session"`
	Group    GroupConfig   `toml:"group"`
	Daemon   DaemonConfig  `toml:"daemon"`
}

type WeChatConfig struct {
	BaseURL    string `toml:"base_url"`
	CDNBaseURL string `toml:"cdn_base_url"`
}

type AgentCfg struct {
	Command      string            `toml:"command"`
	Args         []string          `toml:"args"`
	Cwd          string            `toml:"cwd"`
	Env          map[string]string `toml:"env"`
	ShowThoughts bool              `toml:"show_thoughts"`
}

type SessionConfig struct {
	IdleTimeout    Duration `toml:"idle_timeout"`
	MaxConcurrent  int      `toml:"max_concurrent"`
}

type GroupConfig struct {
	Enabled     bool   `toml:"enabled"`
	Trigger     string `toml:"trigger"`
	SessionMode string `toml:"session_mode"`
}

type DaemonConfig struct {
	Enabled bool   `toml:"enabled"`
	PidFile string `toml:"pid_file"`
	LogFile string `toml:"log_file"`
}

// Duration wraps time.Duration for TOML string parsing ("24h", "30m").
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalText(text []byte) error {
	var err error
	d.Duration, err = time.ParseDuration(string(text))
	return err
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.Duration.String()), nil
}

func DefaultConfig() *Config {
	return &Config{
		Global: GlobalConfig{
			StorageDir: defaultStorageDir(),
			LogLevel:   "info",
		},
	}
}

func DefaultBotConfig(name, agent string) BotConfig {
	return BotConfig{
		Name:  name,
		Agent: agent,
		WeChat: WeChatConfig{
			BaseURL:    "https://ilinkai.weixin.qq.com",
			CDNBaseURL: "https://novac2c.cdn.weixin.qq.com/c2c",
		},
		AgentCfg: AgentCfg{
			Cwd: ".",
		},
		Session: SessionConfig{
			IdleTimeout:   Duration{24 * time.Hour},
			MaxConcurrent: 10,
		},
		Group: GroupConfig{
			Enabled:     true,
			Trigger:     "@bot",
			SessionMode: "per_group",
		},
	}
}

func LoadFile(path string) (*Config, error) {
	cfg := DefaultConfig()
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	applyDefaults(cfg)
	return cfg, validate(cfg)
}

func applyDefaults(cfg *Config) {
	if cfg.Global.StorageDir == "" {
		cfg.Global.StorageDir = defaultStorageDir()
	}
	cfg.Global.StorageDir = expandHome(cfg.Global.StorageDir)
	if cfg.Global.LogLevel == "" {
		cfg.Global.LogLevel = "info"
	}
	for i := range cfg.Bot {
		b := &cfg.Bot[i]
		if b.WeChat.BaseURL == "" {
			b.WeChat.BaseURL = "https://ilinkai.weixin.qq.com"
		}
		if b.WeChat.CDNBaseURL == "" {
			b.WeChat.CDNBaseURL = "https://novac2c.cdn.weixin.qq.com/c2c"
		}
		if b.AgentCfg.Cwd == "" {
			b.AgentCfg.Cwd = "."
		}
		if b.Session.IdleTimeout.Duration == 0 {
			b.Session.IdleTimeout.Duration = 24 * time.Hour
		}
		if b.Session.MaxConcurrent == 0 {
			b.Session.MaxConcurrent = 10
		}
		if b.Group.Trigger == "" {
			b.Group.Trigger = "@bot"
		}
		if b.Group.SessionMode == "" {
			b.Group.SessionMode = "per_group"
		}
		if b.Daemon.PidFile == "" {
			b.Daemon.PidFile = filepath.Join(cfg.Global.StorageDir, b.Name+".pid")
		}
		if b.Daemon.LogFile == "" {
			b.Daemon.LogFile = filepath.Join(cfg.Global.StorageDir, b.Name+".log")
		}
		b.Daemon.PidFile = expandHome(b.Daemon.PidFile)
		b.Daemon.LogFile = expandHome(b.Daemon.LogFile)
	}
}

func validate(cfg *Config) error {
	names := make(map[string]bool, len(cfg.Bot))
	for _, b := range cfg.Bot {
		if b.Name == "" {
			return fmt.Errorf("bot name is required")
		}
		if names[b.Name] {
			return fmt.Errorf("duplicate bot name: %q", b.Name)
		}
		names[b.Name] = true
	}
	return nil
}

func defaultStorageDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".wechat-router-go")
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// ResolvedAgent holds the final agent command after resolving presets.
type ResolvedAgent struct {
	ID      string
	Label   string
	Command string
	Args    []string
	Env     map[string]string
	Source  string // "preset" or "raw"
}

// ResolveAgent resolves an agent selection string to a command.
// It first checks built-in presets, then custom presets, then treats it as a raw command.
func ResolveAgent(selection string, custom map[string]AgentPreset) ResolvedAgent {
	// Check built-in presets
	if p, ok := BuiltInAgents[selection]; ok {
		return ResolvedAgent{
			ID: selection, Label: p.Label, Command: p.Command,
			Args: p.Args, Env: p.Env, Source: "preset",
		}
	}
	// Check custom presets
	if custom != nil {
		if p, ok := custom[selection]; ok {
			return ResolvedAgent{
				ID: selection, Label: p.Label, Command: p.Command,
				Args: p.Args, Env: p.Env, Source: "preset",
			}
		}
	}
	// Treat as raw command
	parts := strings.Fields(selection)
	if len(parts) == 0 {
		return ResolvedAgent{}
	}
	return ResolvedAgent{
		ID: selection, Label: selection, Command: parts[0],
		Args: parts[1:], Source: "raw",
	}
}
