package router

import "strings"

// Command represents a parsed user command.
type Command struct {
	Name string // "use", "agents", "status"
	Arg  string // argument (e.g., agent name for /use)
}

// known commands
var knownCommands = map[string]bool{
	"use":    true,
	"agents": true,
	"status": true,
	"clear":  true,
}

// ParseCommand checks if text is a known command.
// Returns nil if the text is not a command.
func ParseCommand(text string) *Command {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return nil
	}

	parts := strings.SplitN(text[1:], " ", 2)
	name := strings.ToLower(parts[0])

	if !knownCommands[name] {
		return nil // unknown /command — pass through to agent
	}

	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}

	return &Command{Name: name, Arg: arg}
}
