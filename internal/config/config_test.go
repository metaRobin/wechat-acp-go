package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------- 1. TOML parsing ----------

func TestLoadFile_FullParsing(t *testing.T) {
	content := `
[global]
storage_dir = "/tmp/test-storage"
log_level   = "debug"

[[bot]]
name  = "mybot"

[bot.wechat]
base_url     = "https://example.com"
cdn_base_url = "https://cdn.example.com"

[bot.session]
idle_timeout   = "1h"
max_concurrent = 5

[bot.group]
enabled      = true
trigger      = "@helper"
session_mode = "per_user"

[bot.daemon]
enabled  = true
pid_file = "/tmp/mybot.pid"
log_file = "/tmp/mybot.log"
`
	path := writeTempTOML(t, content)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}

	// Global
	if cfg.Global.StorageDir != "/tmp/test-storage" {
		t.Errorf("StorageDir = %q, want /tmp/test-storage", cfg.Global.StorageDir)
	}
	if cfg.Global.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.Global.LogLevel)
	}

	if len(cfg.Bot) != 1 {
		t.Fatalf("len(Bot) = %d, want 1", len(cfg.Bot))
	}
	b := cfg.Bot[0]

	if b.Name != "mybot" {
		t.Errorf("Name = %q, want mybot", b.Name)
	}
	if b.WeChat.BaseURL != "https://example.com" {
		t.Errorf("BaseURL = %q", b.WeChat.BaseURL)
	}
	if b.WeChat.CDNBaseURL != "https://cdn.example.com" {
		t.Errorf("CDNBaseURL = %q", b.WeChat.CDNBaseURL)
	}
	// Note: AgentCfg fields share the TOML key "agent" with the Agent string field,
	// so they cannot be tested via TOML parsing. We verify defaults are applied instead.
	if b.AgentCfg.Cwd != "." {
		t.Errorf("AgentCfg.Cwd = %q, want default '.'", b.AgentCfg.Cwd)
	}
	if b.Session.IdleTimeout.Duration != time.Hour {
		t.Errorf("IdleTimeout = %v, want 1h", b.Session.IdleTimeout.Duration)
	}
	if b.Session.MaxConcurrent != 5 {
		t.Errorf("MaxConcurrent = %d, want 5", b.Session.MaxConcurrent)
	}
	if b.Group.Trigger != "@helper" {
		t.Errorf("Trigger = %q", b.Group.Trigger)
	}
	if b.Group.SessionMode != "per_user" {
		t.Errorf("SessionMode = %q", b.Group.SessionMode)
	}
	if !b.Daemon.Enabled {
		t.Error("Daemon.Enabled should be true")
	}
	if b.Daemon.PidFile != "/tmp/mybot.pid" {
		t.Errorf("PidFile = %q", b.Daemon.PidFile)
	}
	if b.Daemon.LogFile != "/tmp/mybot.log" {
		t.Errorf("LogFile = %q", b.Daemon.LogFile)
	}
}

// ---------- 2. Default values ----------

func TestLoadFile_Defaults(t *testing.T) {
	content := `
[[bot]]
name  = "minimal"
agent = "claude"
`
	path := writeTempTOML(t, content)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}

	b := cfg.Bot[0]

	if b.Session.IdleTimeout.Duration != 24*time.Hour {
		t.Errorf("IdleTimeout = %v, want 24h", b.Session.IdleTimeout.Duration)
	}
	if b.Session.MaxConcurrent != 10 {
		t.Errorf("MaxConcurrent = %d, want 10", b.Session.MaxConcurrent)
	}
	if b.Group.Trigger != "@bot" {
		t.Errorf("Trigger = %q, want @bot", b.Group.Trigger)
	}
	if b.Group.SessionMode != "per_group" {
		t.Errorf("SessionMode = %q, want per_group", b.Group.SessionMode)
	}
	if b.WeChat.BaseURL != "https://ilinkai.weixin.qq.com" {
		t.Errorf("BaseURL = %q", b.WeChat.BaseURL)
	}
	if b.WeChat.CDNBaseURL != "https://novac2c.cdn.weixin.qq.com/c2c" {
		t.Errorf("CDNBaseURL = %q", b.WeChat.CDNBaseURL)
	}
	if b.AgentCfg.Cwd != "." {
		t.Errorf("Cwd = %q, want .", b.AgentCfg.Cwd)
	}
	if cfg.Global.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.Global.LogLevel)
	}
}

// ---------- 3. Validation - duplicate bot names ----------

func TestLoadFile_DuplicateBotNames(t *testing.T) {
	content := `
[[bot]]
name = "dup"
agent = "claude"

[[bot]]
name = "dup"
agent = "copilot"
`
	path := writeTempTOML(t, content)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error for duplicate bot names, got nil")
	}
	if got := err.Error(); got != `duplicate bot name: "dup"` {
		t.Errorf("error = %q, want duplicate bot name: \"dup\"", got)
	}
}

// ---------- 4. Validation - empty bot name ----------

func TestLoadFile_EmptyBotName(t *testing.T) {
	content := `
[[bot]]
name = ""
agent = "claude"
`
	path := writeTempTOML(t, content)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error for empty bot name, got nil")
	}
	if got := err.Error(); got != "bot name is required" {
		t.Errorf("error = %q, want 'bot name is required'", got)
	}
}

// ---------- 5. Preset resolution - built-in ----------

func TestResolveAgent_BuiltIn(t *testing.T) {
	r := ResolveAgent("claude", nil)
	if r.Source != "preset" {
		t.Errorf("Source = %q, want preset", r.Source)
	}
	if r.ID != "claude" {
		t.Errorf("ID = %q, want claude", r.ID)
	}
	if r.Label != "Claude Code" {
		t.Errorf("Label = %q, want Claude Code", r.Label)
	}
	if r.Command != "claude" {
		t.Errorf("Command = %q, want claude", r.Command)
	}
	if len(r.Args) != 0 {
		t.Errorf("Args = %v, want empty", r.Args)
	}
}

// ---------- 6. Preset resolution - custom ----------

func TestResolveAgent_Custom(t *testing.T) {
	custom := map[string]AgentPreset{
		"myagent": {
			Label:   "My Agent",
			Command: "my-binary",
			Args:    []string{"--serve"},
			Env:     map[string]string{"KEY": "val"},
		},
	}
	r := ResolveAgent("myagent", custom)
	if r.Source != "preset" {
		t.Errorf("Source = %q, want preset", r.Source)
	}
	if r.Command != "my-binary" {
		t.Errorf("Command = %q", r.Command)
	}
	if len(r.Args) != 1 || r.Args[0] != "--serve" {
		t.Errorf("Args = %v", r.Args)
	}
	if r.Env["KEY"] != "val" {
		t.Errorf("Env = %v", r.Env)
	}
}

// ---------- 7. Preset resolution - raw command ----------

func TestResolveAgent_RawCommand(t *testing.T) {
	r := ResolveAgent("my-agent --flag", nil)
	if r.Source != "raw" {
		t.Errorf("Source = %q, want raw", r.Source)
	}
	if r.Command != "my-agent" {
		t.Errorf("Command = %q, want my-agent", r.Command)
	}
	if len(r.Args) != 1 || r.Args[0] != "--flag" {
		t.Errorf("Args = %v, want [--flag]", r.Args)
	}
}

// ---------- 8. Preset resolution - unknown / empty ----------

func TestResolveAgent_Empty(t *testing.T) {
	r := ResolveAgent("", nil)
	if r.Command != "" {
		t.Errorf("Command = %q, want empty", r.Command)
	}
	if r.Source != "" {
		t.Errorf("Source = %q, want empty", r.Source)
	}
	if r.ID != "" {
		t.Errorf("ID = %q, want empty", r.ID)
	}
}

// ---------- 9. Duration unmarshal ----------

func TestDuration_UnmarshalText(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"24h", 24 * time.Hour},
		{"30m", 30 * time.Minute},
		{"1h30m", 90 * time.Minute},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			var d Duration
			if err := d.UnmarshalText([]byte(tc.input)); err != nil {
				t.Fatalf("UnmarshalText(%q) error: %v", tc.input, err)
			}
			if d.Duration != tc.want {
				t.Errorf("got %v, want %v", d.Duration, tc.want)
			}
		})
	}
}

func TestDuration_UnmarshalText_Invalid(t *testing.T) {
	var d Duration
	if err := d.UnmarshalText([]byte("bad")); err == nil {
		t.Fatal("expected error for invalid duration, got nil")
	}
}

// ---------- 10. ListPresets ----------

func TestListPresets(t *testing.T) {
	presets := ListPresets()
	expectedIDs := []string{"claude", "codex", "copilot", "gemini", "opencode", "qwen"}

	if len(presets) != len(expectedIDs) {
		t.Fatalf("len(presets) = %d, want %d", len(presets), len(expectedIDs))
	}

	for i, id := range expectedIDs {
		if presets[i].ID != id {
			t.Errorf("presets[%d].ID = %q, want %q", i, presets[i].ID, id)
		}
		if presets[i].Preset.Command == "" {
			t.Errorf("presets[%d] (%s) has empty Command", i, id)
		}
		if presets[i].Preset.Label == "" {
			t.Errorf("presets[%d] (%s) has empty Label", i, id)
		}
	}
}

// ---------- helpers ----------

func writeTempTOML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp TOML: %v", err)
	}
	return path
}
