package session

import (
	"strings"
	"sync"
)

// StreamBuffer accumulates text chunks and flushes to WeChat
// when enough content has been buffered (at natural break points).
type StreamBuffer struct {
	mu           sync.Mutex
	buf          strings.Builder
	threshold    int
	flushFn      func(text string)
	totalFlushed strings.Builder // all text sent, for persistence
}

// NewStreamBuffer creates a buffer that flushes at threshold characters.
// flushFn is called with each segment to send.
func NewStreamBuffer(threshold int, flushFn func(text string)) *StreamBuffer {
	if threshold <= 0 {
		threshold = 500
	}
	return &StreamBuffer{
		threshold: threshold,
		flushFn:   flushFn,
	}
}

// Write adds a chunk of text. If the buffer exceeds threshold,
// it finds a good split point and flushes the first segment.
func (sb *StreamBuffer) Write(chunk string) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.buf.WriteString(chunk)

	// Only try to flush if above threshold
	for sb.buf.Len() >= sb.threshold {
		text := sb.buf.String()

		// Check if we're inside a code block at the split region
		if sb.hasOpenCodeBlock(text[:min(sb.threshold, len(text))]) {
			break // don't split inside code block
		}

		splitAt := sb.findSplitPoint(text)
		if splitAt <= 0 {
			break // no good split point, keep buffering
		}

		segment := strings.TrimSpace(text[:splitAt])
		if segment != "" {
			sb.totalFlushed.WriteString(segment + "\n")
			sb.flushFn(segment)
		}

		remainder := text[splitAt:]
		sb.buf.Reset()
		sb.buf.WriteString(remainder)
	}
}

// Flush sends any remaining buffered text.
func (sb *StreamBuffer) Flush() {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	text := strings.TrimSpace(sb.buf.String())
	if text != "" {
		sb.totalFlushed.WriteString(text)
		sb.flushFn(text)
	}
	sb.buf.Reset()
}

// AllText returns all text that was flushed (for persistence).
func (sb *StreamBuffer) AllText() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return strings.TrimSpace(sb.totalFlushed.String())
}

// hasOpenCodeBlock checks if text has an unclosed ``` fence.
func (sb *StreamBuffer) hasOpenCodeBlock(text string) bool {
	return strings.Count(text, "```")%2 != 0
}

// findSplitPoint finds the best position to split text for sending.
// Prefers paragraph breaks (\n\n), then line breaks (\n).
func (sb *StreamBuffer) findSplitPoint(text string) int {
	// Only search within the threshold region
	searchEnd := sb.threshold
	if searchEnd > len(text) {
		searchEnd = len(text)
	}

	// Prefer paragraph break (double newline)
	if idx := strings.LastIndex(text[:searchEnd], "\n\n"); idx > sb.threshold/4 {
		return idx + 2
	}

	// Fall back to line break
	if idx := strings.LastIndex(text[:searchEnd], "\n"); idx > sb.threshold/4 {
		return idx + 1
	}

	// Hard break at threshold
	return searchEnd
}
