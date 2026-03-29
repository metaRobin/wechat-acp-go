package adapter

import (
	"regexp"
	"strings"
)

var (
	reImage      = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	reLink       = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reBoldItalic = regexp.MustCompile(`\*\*\*(.+?)\*\*\*`)
	reBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reItalic     = regexp.MustCompile(`\*(.+?)\*`)
	reUnderBold  = regexp.MustCompile(`__(.+?)__`)
	reUnderItal  = regexp.MustCompile(`_(.+?)_`)
	reHeading    = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	reBlankLines = regexp.MustCompile(`\n{3,}`)
)

// FormatForWeChat strips Markdown formatting for cleaner WeChat display.
// Preserves code blocks.
func FormatForWeChat(text string) string {
	out := reImage.ReplaceAllString(text, "[$1]")
	out = reLink.ReplaceAllString(out, "$1 ($2)")
	out = reBoldItalic.ReplaceAllString(out, "$1")
	out = reBold.ReplaceAllString(out, "$1")
	out = reItalic.ReplaceAllString(out, "$1")
	out = reUnderBold.ReplaceAllString(out, "$1")
	out = reUnderItal.ReplaceAllString(out, "$1")
	out = reHeading.ReplaceAllString(out, "")
	out = reBlankLines.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out)
}

// SplitText splits text into segments of at most maxRunes runes,
// preferring to break at newline boundaries.
func SplitText(text string, maxRunes int) []string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return []string{text}
	}

	var segments []string
	for len(runes) > 0 {
		if len(runes) <= maxRunes {
			segments = append(segments, string(runes))
			break
		}

		chunk := runes[:maxRunes]
		breakAt := -1
		for i := len(chunk) - 1; i > 0; i-- {
			if chunk[i] == '\n' {
				breakAt = i
				break
			}
		}

		if breakAt <= 0 {
			breakAt = maxRunes
		}

		segments = append(segments, string(runes[:breakAt]))
		runes = runes[breakAt:]
		// Skip leading newline on next segment
		if len(runes) > 0 && runes[0] == '\n' {
			runes = runes[1:]
		}
	}
	return segments
}
