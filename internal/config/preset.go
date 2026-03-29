package config

import "strings"

type AgentPreset struct {
	Label       string            `toml:"label"`
	Description string            `toml:"description"`
	Command     string            `toml:"command"`
	Args        []string          `toml:"args"`
	Env         map[string]string `toml:"env"`
}

var BuiltInAgents = map[string]AgentPreset{
	"claude": {
		Label:       "Claude Code",
		Description: "Anthropic Claude — uses OAuth login, no API key needed",
		Command:     "claude",
	},
	"codex": {
		Label:       "Codex CLI",
		Description: "OpenAI Codex — uses codex login, no API key needed",
		Command:     "codex",
	},
	"gemini": {
		Label:       "Gemini CLI",
		Description: "Google Gemini coding agent",
		Command:     "gemini",
		Args:        []string{"-p"},
	},
	"copilot": {
		Label:       "GitHub Copilot",
		Description: "GitHub Copilot coding agent",
		Command:     "github-copilot",
	},
	"qwen": {
		Label:       "Qwen Code",
		Description: "Alibaba Qwen coding agent",
		Command:     "qwen",
	},
	"opencode": {
		Label:       "OpenCode",
		Description: "Open-source coding agent",
		Command:     "opencode",
		Args:        []string{"run"},
	},
}

// FormatAgentList returns a WeChat-friendly text listing all available agents.
func FormatAgentList(custom map[string]AgentPreset) string {
	var sb strings.Builder
	sb.WriteString("可用的 AI Agent:\n\n")
	for _, p := range ListPresets() {
		sb.WriteString("  /use " + p.ID + " — " + p.Preset.Label + "\n")
	}
	// Custom presets
	for id, p := range custom {
		if _, builtin := BuiltInAgents[id]; !builtin {
			label := p.Label
			if label == "" {
				label = p.Command
			}
			sb.WriteString("  /use " + id + " — " + label + "\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// LookupAgent checks if an agent ID exists in built-in or custom presets.
func LookupAgent(id string, custom map[string]AgentPreset) (AgentPreset, bool) {
	if p, ok := BuiltInAgents[id]; ok {
		return p, true
	}
	if custom != nil {
		if p, ok := custom[id]; ok {
			return p, true
		}
	}
	return AgentPreset{}, false
}

// ListPresets returns all built-in agent presets sorted by ID.
func ListPresets() []struct {
	ID     string
	Preset AgentPreset
} {
	ids := []string{"claude", "codex", "copilot", "gemini", "opencode", "qwen"}
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
