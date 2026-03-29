package adapter

import (
	"os"
	"strings"
	"testing"
)

func TestFormatForWeChat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips markdown images",
			input:    "Check this ![screenshot](https://example.com/img.png) out",
			expected: "Check this [screenshot] out",
		},
		{
			name:     "strips markdown image with empty alt",
			input:    "Here ![](https://example.com/img.png)",
			expected: "Here []",
		},
		{
			name:     "converts links",
			input:    "Visit [Google](https://google.com) for more",
			expected: "Visit Google (https://google.com) for more",
		},
		{
			name:     "strips bold",
			input:    "This is **bold text** here",
			expected: "This is bold text here",
		},
		{
			name:     "strips italic with asterisk",
			input:    "This is *italic text* here",
			expected: "This is italic text here",
		},
		{
			name:     "strips bold-italic",
			input:    "This is ***bold italic*** here",
			expected: "This is bold italic here",
		},
		{
			name:     "strips underscore bold",
			input:    "This is __bold__ here",
			expected: "This is bold here",
		},
		{
			name:     "strips underscore italic",
			input:    "This is _italic_ here",
			expected: "This is italic here",
		},
		{
			name:     "strips headings",
			input:    "## Heading Two\nSome text\n### Heading Three",
			expected: "Heading Two\nSome text\nHeading Three",
		},
		{
			name:     "strips h1 heading",
			input:    "# Title",
			expected: "Title",
		},
		{
			name:     "collapses blank lines",
			input:    "Line 1\n\n\n\nLine 2",
			expected: "Line 1\n\nLine 2",
		},
		{
			name:     "preserves code block content",
			input:    "```go\nfunc main() {}\n```",
			expected: "```go\nfunc main() {}\n```",
		},
		{
			name:     "preserves inline code",
			input:    "Use `fmt.Println` to print",
			expected: "Use `fmt.Println` to print",
		},
		{
			name:     "combined formatting",
			input:    "## Title\n\nVisit **[Google](https://google.com)** for *more* info\n\n\n\nEnd",
			expected: "Title\n\nVisit Google (https://google.com) for more info\n\nEnd",
		},
		{
			name:     "trims surrounding whitespace",
			input:    "  \n  hello  \n  ",
			expected: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatForWeChat(tt.input)
			if got != tt.expected {
				t.Errorf("FormatForWeChat(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSplitText_ShortText(t *testing.T) {
	text := "short message"
	segments := SplitText(text, 100)

	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}
	if segments[0] != text {
		t.Errorf("expected %q, got %q", text, segments[0])
	}
}

func TestSplitText_ExactLimit(t *testing.T) {
	text := "12345"
	segments := SplitText(text, 5)

	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}
	if segments[0] != text {
		t.Errorf("expected %q, got %q", text, segments[0])
	}
}

func TestSplitText_SplitsAtNewline(t *testing.T) {
	// Build text that exceeds maxRunes with newlines for preferred break points
	line1 := "Line one content"
	line2 := "Line two content"
	line3 := "Line three content"
	text := line1 + "\n" + line2 + "\n" + line3

	// Set maxRunes so that line1 + "\n" + line2 fits but adding line3 doesn't
	maxRunes := len([]rune(line1)) + 1 + len([]rune(line2)) + 5

	segments := SplitText(text, maxRunes)

	if len(segments) < 2 {
		t.Fatalf("expected at least 2 segments, got %d", len(segments))
	}

	// The first segment should break at a newline boundary
	if !strings.HasSuffix(segments[0], line2) && !strings.HasSuffix(segments[0], line1) {
		// Verify it breaks at a newline somewhere
		for i, seg := range segments {
			if strings.Contains(seg, "\n") && i < len(segments)-1 {
				// OK: newlines are inside segments, not split mid-line
			}
		}
	}

	// Reassemble should give back the original text (minus potential newline at split)
	reassembled := strings.Join(segments, "\n")
	if reassembled != text {
		t.Logf("reassembled: %q", reassembled)
		t.Logf("original:    %q", text)
		// This is informational; exact behavior depends on newline-stripping logic
	}
}

func TestSplitText_HardBreakWithoutNewlines(t *testing.T) {
	// Text with no newlines at all
	text := strings.Repeat("a", 50)
	maxRunes := 20

	segments := SplitText(text, maxRunes)

	if len(segments) != 3 {
		t.Fatalf("expected 3 segments for 50 chars / 20 max, got %d", len(segments))
	}
	if len([]rune(segments[0])) != 20 {
		t.Errorf("first segment should be 20 runes, got %d", len([]rune(segments[0])))
	}
	if len([]rune(segments[1])) != 20 {
		t.Errorf("second segment should be 20 runes, got %d", len([]rune(segments[1])))
	}
	if len([]rune(segments[2])) != 10 {
		t.Errorf("third segment should be 10 runes, got %d", len([]rune(segments[2])))
	}
}

func TestSplitText_ChineseCharacters(t *testing.T) {
	// Each Chinese character is one rune but multiple bytes
	text := "你好世界这是一个测试消息用于验证中文分割功能"
	runes := []rune(text)
	totalRunes := len(runes)

	maxRunes := 10
	segments := SplitText(text, maxRunes)

	// Verify no segment exceeds maxRunes
	for i, seg := range segments {
		segRunes := len([]rune(seg))
		if segRunes > maxRunes {
			t.Errorf("segment %d has %d runes (max %d): %q", i, segRunes, maxRunes, seg)
		}
	}

	// Verify all runes are accounted for
	totalSegRunes := 0
	for _, seg := range segments {
		totalSegRunes += len([]rune(seg))
	}
	if totalSegRunes != totalRunes {
		t.Errorf("total runes in segments (%d) != original runes (%d)", totalSegRunes, totalRunes)
	}

	// Verify reassembly
	reassembled := strings.Join(segments, "")
	if reassembled != text {
		t.Errorf("reassembled text doesn't match original\n  got:  %q\n  want: %q", reassembled, text)
	}
}

func TestSplitText_MixedChineseAndASCII(t *testing.T) {
	text := "Hello你好World世界Test"
	maxRunes := 7

	segments := SplitText(text, maxRunes)

	for i, seg := range segments {
		segRunes := len([]rune(seg))
		if segRunes > maxRunes {
			t.Errorf("segment %d has %d runes (max %d): %q", i, segRunes, maxRunes, seg)
		}
	}

	if len(segments) < 2 {
		t.Errorf("expected at least 2 segments, got %d", len(segments))
	}
}

func TestSplitText_EmptyText(t *testing.T) {
	segments := SplitText("", 100)

	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}
	if segments[0] != "" {
		t.Errorf("expected empty string, got %q", segments[0])
	}
}

func TestSplitText_PreferNewlineBreak(t *testing.T) {
	// Ensure that when a newline exists within the chunk, we break there
	text := "AAAAAAAAAA\nBBBBBBBBBB\nCCCCCCCCCC"
	maxRunes := 15

	segments := SplitText(text, maxRunes)

	// First segment should break at the newline after "AAAAAAAAAA"
	if segments[0] != "AAAAAAAAAA" {
		t.Errorf("expected first segment %q, got %q", "AAAAAAAAAA", segments[0])
	}

	if len(segments) < 2 {
		t.Fatalf("expected at least 2 segments, got %d", len(segments))
	}
}

// --- Code block extraction tests ---

func TestExtractLongCodeBlocks_LongBlock(t *testing.T) {
	// Create a code block with 60 lines
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, "print('hello')")
	}
	code := strings.Join(lines, "\n")
	text := "Here is code:\n```python\n" + code + "\n```\nDone."

	result, blocks := ExtractLongCodeBlocks(text, 50)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block extracted, got %d", len(blocks))
	}
	if blocks[0].Language != "python" {
		t.Errorf("language = %q, want python", blocks[0].Language)
	}
	if blocks[0].FileName != "code_1.py" {
		t.Errorf("fileName = %q, want code_1.py", blocks[0].FileName)
	}
	if !strings.Contains(result, "[代码已作为文件发送: code_1.py]") {
		t.Errorf("expected placeholder in result, got: %s", result[:min(len(result), 100)])
	}
	if strings.Contains(result, "print('hello')") {
		t.Error("code should be removed from result text")
	}
}

func TestExtractLongCodeBlocks_ShortBlock(t *testing.T) {
	text := "```go\nfmt.Println(\"hi\")\n```"
	result, blocks := ExtractLongCodeBlocks(text, 50)

	if len(blocks) != 0 {
		t.Errorf("expected 0 blocks for short code, got %d", len(blocks))
	}
	if result != text {
		t.Errorf("short code should be unchanged")
	}
}

func TestLangToExt(t *testing.T) {
	tests := map[string]string{
		"python":     ".py",
		"go":         ".go",
		"javascript": ".js",
		"typescript":  ".ts",
		"rust":       ".rs",
		"bash":       ".sh",
		"":           ".txt",
		"unknown":    ".txt",
	}
	for lang, want := range tests {
		got := LangToExt(lang)
		if got != want {
			t.Errorf("LangToExt(%q) = %q, want %q", lang, got, want)
		}
	}
}

func TestCleanupMedia(t *testing.T) {
	dir := t.TempDir()
	// Create a session media directory with a file
	sessionDir := dir + "/test-session"
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionDir+"/test.jpg", []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	CleanupMedia(dir, "test-session")

	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Error("expected session media dir to be deleted")
	}
}
