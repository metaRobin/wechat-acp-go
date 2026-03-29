package config

type AgentPreset struct {
	Label       string            `toml:"label"`
	Description string            `toml:"description"`
	Command     string            `toml:"command"`
	Args        []string          `toml:"args"`
	Env         map[string]string `toml:"env"`
}

var BuiltInAgents = map[string]AgentPreset{
	"copilot": {
		Label:       "GitHub Copilot",
		Description: "GitHub Copilot coding agent",
		Command:     "npx",
		Args:        []string{"@github/copilot", "--acp", "--yolo"},
	},
	"claude": {
		Label:       "Claude Code (direct)",
		Description: "Claude Code CLI — uses OAuth login, no API key needed",
		Command:     "claude",
	},
	"claude-acp": {
		Label:       "Claude Agent (ACP)",
		Description: "Claude coding agent via ACP bridge (requires API key)",
		Command:     "npx",
		Args:        []string{"@zed-industries/claude-agent-acp"},
	},
	"gemini": {
		Label:       "Gemini CLI",
		Description: "Google Gemini coding agent",
		Command:     "npx",
		Args:        []string{"@google/gemini-cli", "--experimental-acp"},
	},
	"qwen": {
		Label:       "Qwen Code",
		Description: "Alibaba Qwen coding agent",
		Command:     "npx",
		Args:        []string{"@qwen-code/qwen-code", "--acp", "--experimental-skills"},
	},
	"codex": {
		Label:       "Codex CLI",
		Description: "OpenAI Codex coding agent",
		Command:     "npx",
		Args:        []string{"@zed-industries/codex-acp"},
	},
	"opencode": {
		Label:       "OpenCode",
		Description: "Open-source coding agent",
		Command:     "npx",
		Args:        []string{"opencode-ai", "acp"},
	},
}

// ListPresets returns all built-in agent presets sorted by ID.
func ListPresets() []struct {
	ID     string
	Preset AgentPreset
} {
	ids := []string{"claude", "claude-acp", "codex", "copilot", "gemini", "opencode", "qwen"}
	result := make([]struct {
		ID     string
		Preset AgentPreset
	}, 0, len(ids))
	for _, id := range ids {
		if p, ok := BuiltInAgents[id]; ok {
			result = append(result, struct {
				ID     string
				Preset AgentPreset
			}{ID: id, Preset: p})
		}
	}
	return result
}
