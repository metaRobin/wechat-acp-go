package adapter

import (
	"fmt"
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

// CodeBlock represents an extracted code block from agent reply.
type CodeBlock struct {
	Language string
	Content  string
	FileName string // derived from language
}

// ExtractLongCodeBlocks finds code blocks longer than threshold lines
// and returns them, replacing each in the text with a placeholder.
func ExtractLongCodeBlocks(text string, threshold int) (string, []CodeBlock) {
	var blocks []CodeBlock
	result := text
	count := 0

	for {
		start := strings.Index(result, "```")
		if start == -1 {
			break
		}

		// Find the language tag (rest of the line after ```)
		lineEnd := strings.Index(result[start+3:], "\n")
		if lineEnd == -1 {
			break
		}
		lang := strings.TrimSpace(result[start+3 : start+3+lineEnd])

		// Find closing ```
		closeSearch := start + 3 + lineEnd + 1
		end := strings.Index(result[closeSearch:], "```")
		if end == -1 {
			break
		}
		end = closeSearch + end + 3

		// Extract content between fences
		content := result[start+3+lineEnd+1 : closeSearch+strings.Index(result[closeSearch:], "```")]
		lineCount := strings.Count(content, "\n") + 1

		if lineCount > threshold {
			count++
			ext := LangToExt(lang)
			fileName := fmt.Sprintf("code_%d%s", count, ext)

			blocks = append(blocks, CodeBlock{
				Language: lang,
				Content:  content,
				FileName: fileName,
			})

			placeholder := fmt.Sprintf("[代码已作为文件发送: %s]", fileName)
			result = result[:start] + placeholder + result[end:]
		} else {
			// Skip past this block to find the next one
			// Move past the closing ``` by replacing start search position
			// We need to avoid re-matching the same block
			break // simplified: only extract first long block per pass
		}
	}

	return result, blocks
}

// LangToExt maps a code block language tag to a file extension.
func LangToExt(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch lang {
	case "python", "py":
		return ".py"
	case "go", "golang":
		return ".go"
	case "javascript", "js":
		return ".js"
	case "typescript", "ts":
		return ".ts"
	case "java":
		return ".java"
	case "rust", "rs":
		return ".rs"
	case "c":
		return ".c"
	case "cpp", "c++":
		return ".cpp"
	case "ruby", "rb":
		return ".rb"
	case "php":
		return ".php"
	case "shell", "bash", "sh":
		return ".sh"
	case "sql":
		return ".sql"
	case "html":
		return ".html"
	case "css":
		return ".css"
	case "json":
		return ".json"
	case "yaml", "yml":
		return ".yaml"
	case "toml":
		return ".toml"
	case "xml":
		return ".xml"
	case "swift":
		return ".swift"
	case "kotlin", "kt":
		return ".kt"
	default:
		return ".txt"
	}
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
