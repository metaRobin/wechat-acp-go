package session

import (
	"strings"
	"sync"
	"testing"
)

func TestStreamBuffer_ShortText(t *testing.T) {
	var mu sync.Mutex
	var flushed []string

	buf := NewStreamBuffer(500, func(text string) {
		mu.Lock()
		flushed = append(flushed, text)
		mu.Unlock()
	})

	buf.Write("short reply")
	// Should not flush yet (below threshold)
	mu.Lock()
	if len(flushed) != 0 {
		t.Errorf("expected 0 intermediate flushes, got %d", len(flushed))
	}
	mu.Unlock()

	buf.Flush()
	mu.Lock()
	defer mu.Unlock()
	if len(flushed) != 1 {
		t.Fatalf("expected 1 flush after Flush(), got %d", len(flushed))
	}
	if flushed[0] != "short reply" {
		t.Errorf("flushed = %q, want 'short reply'", flushed[0])
	}
}

func TestStreamBuffer_LongText_ParagraphSplit(t *testing.T) {
	var mu sync.Mutex
	var flushed []string

	buf := NewStreamBuffer(100, func(text string) {
		mu.Lock()
		flushed = append(flushed, text)
		mu.Unlock()
	})

	// Write text with paragraphs, exceeding threshold
	para1 := strings.Repeat("a", 60) + "\n\n"
	para2 := strings.Repeat("b", 60) + "\n\n"
	para3 := strings.Repeat("c", 30)

	buf.Write(para1)
	buf.Write(para2)
	buf.Write(para3)
	buf.Flush()

	mu.Lock()
	defer mu.Unlock()

	// Should have split at paragraph boundaries
	if len(flushed) < 2 {
		t.Fatalf("expected >= 2 flushes for long text, got %d: %v", len(flushed), flushed)
	}

	// All text should be present across all flushes
	all := strings.Join(flushed, "\n")
	if !strings.Contains(all, strings.Repeat("a", 60)) {
		t.Error("missing paragraph 1 content")
	}
	if !strings.Contains(all, strings.Repeat("c", 30)) {
		t.Error("missing paragraph 3 content")
	}
}

func TestStreamBuffer_CodeBlock_NoSplit(t *testing.T) {
	var mu sync.Mutex
	var flushed []string

	buf := NewStreamBuffer(50, func(text string) {
		mu.Lock()
		flushed = append(flushed, text)
		mu.Unlock()
	})

	// Write a code block that exceeds threshold
	code := "```\n" + strings.Repeat("x", 100) + "\n```"
	buf.Write("Here is code:\n" + code + "\nDone.")

	// Check: while inside code block, no intermediate flush should have happened
	// that splits the code block
	buf.Flush()

	mu.Lock()
	defer mu.Unlock()

	// Verify code block content is not split across segments
	for _, seg := range flushed {
		// If a segment contains opening ```, it must also contain closing ```
		opens := strings.Count(seg, "```")
		if opens%2 != 0 {
			t.Errorf("segment has unbalanced code fences (%d): %s", opens, seg[:min(len(seg), 80)])
		}
	}
}

func TestStreamBuffer_EmptyFlush(t *testing.T) {
	var flushed int
	buf := NewStreamBuffer(500, func(text string) {
		flushed++
	})

	buf.Flush()
	if flushed != 0 {
		t.Errorf("expected 0 flushes for empty buffer, got %d", flushed)
	}
}

func TestStreamBuffer_AllText(t *testing.T) {
	buf := NewStreamBuffer(50, func(text string) {})

	buf.Write("hello ")
	buf.Write("world")
	buf.Flush()

	all := buf.AllText()
	if !strings.Contains(all, "hello") || !strings.Contains(all, "world") {
		t.Errorf("AllText() = %q, want to contain 'hello' and 'world'", all)
	}
}
