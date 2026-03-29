package adapter

import (
	"context"
	"testing"

	wechatbot "github.com/corespeed-io/wechatbot/golang"
)

func TestIncomingToPrompt_TextMessage(t *testing.T) {
	msg := &wechatbot.IncomingMessage{
		UserID: "user1",
		Type:   wechatbot.ContentText,
		Text:   "hello world",
	}

	blocks := IncomingToPrompt(context.Background(), msg, nil, false, "")

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Text == nil {
		t.Fatal("expected Text block, got nil")
	}
	if blocks[0].Text.Text != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", blocks[0].Text.Text)
	}
}

func TestIncomingToPrompt_TextWithQuotedMessage(t *testing.T) {
	tests := []struct {
		name     string
		quoted   *wechatbot.QuotedMessage
		text     string
		expected string
	}{
		{
			name:     "title and text",
			quoted:   &wechatbot.QuotedMessage{Title: "Alice", Text: "original msg"},
			text:     "my reply",
			expected: "[Quote: Alice | original msg]\nmy reply",
		},
		{
			name:     "title only",
			quoted:   &wechatbot.QuotedMessage{Title: "Alice"},
			text:     "my reply",
			expected: "[Quote: Alice]\nmy reply",
		},
		{
			name:     "text only",
			quoted:   &wechatbot.QuotedMessage{Text: "original msg"},
			text:     "my reply",
			expected: "[Quote: original msg]\nmy reply",
		},
		{
			name:     "empty quoted message - no quote prefix",
			quoted:   &wechatbot.QuotedMessage{},
			text:     "my reply",
			expected: "my reply",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &wechatbot.IncomingMessage{
				UserID:        "user1",
				Type:          wechatbot.ContentText,
				Text:          tt.text,
				QuotedMessage: tt.quoted,
			}

			blocks := IncomingToPrompt(context.Background(), msg, nil, false, "")

			if len(blocks) != 1 {
				t.Fatalf("expected 1 block, got %d", len(blocks))
			}
			if blocks[0].Text == nil {
				t.Fatal("expected Text block, got nil")
			}
			if blocks[0].Text.Text != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, blocks[0].Text.Text)
			}
		})
	}
}

func TestIncomingToPrompt_EmptyMessageFallback(t *testing.T) {
	msg := &wechatbot.IncomingMessage{
		UserID: "user1",
		Type:   wechatbot.ContentText,
		Text:   "",
	}

	blocks := IncomingToPrompt(context.Background(), msg, nil, false, "")

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Text == nil {
		t.Fatal("expected Text block")
	}
	if blocks[0].Text.Text != "[empty message]" {
		t.Errorf("expected %q, got %q", "[empty message]", blocks[0].Text.Text)
	}
}

func TestIncomingToPrompt_GroupMessageWithSenderName(t *testing.T) {
	msg := &wechatbot.IncomingMessage{
		UserID: "user1",
		Type:   wechatbot.ContentText,
		Text:   "hello from group",
	}

	blocks := IncomingToPrompt(context.Background(), msg, nil, true, "Alice")

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Text == nil {
		t.Fatal("expected first block to be Text")
	}
	if blocks[0].Text.Text != "[Alice says]:" {
		t.Errorf("expected %q, got %q", "[Alice says]:", blocks[0].Text.Text)
	}
	if blocks[1].Text == nil {
		t.Fatal("expected second block to be Text")
	}
	if blocks[1].Text.Text != "hello from group" {
		t.Errorf("expected %q, got %q", "hello from group", blocks[1].Text.Text)
	}
}

func TestIncomingToPrompt_GroupEmptyMessageFallback(t *testing.T) {
	msg := &wechatbot.IncomingMessage{
		UserID: "user1",
		Type:   wechatbot.ContentText,
		Text:   "",
	}

	blocks := IncomingToPrompt(context.Background(), msg, nil, true, "Alice")

	// Should have sender identity block + empty message fallback
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Text.Text != "[Alice says]:" {
		t.Errorf("expected sender identity, got %q", blocks[0].Text.Text)
	}
	if blocks[1].Text.Text != "[empty message]" {
		t.Errorf("expected empty message fallback, got %q", blocks[1].Text.Text)
	}
}

func TestIncomingToPrompt_VoiceMessageWithText(t *testing.T) {
	msg := &wechatbot.IncomingMessage{
		UserID: "user1",
		Type:   wechatbot.ContentVoice,
		Text:   "transcribed voice text",
		Voices: []wechatbot.VoiceContent{{Text: "transcribed voice text"}},
	}

	blocks := IncomingToPrompt(context.Background(), msg, nil, false, "")

	// Should have the transcription text only (no media block since text is present)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Text == nil {
		t.Fatal("expected Text block")
	}
	if blocks[0].Text.Text != "transcribed voice text" {
		t.Errorf("expected %q, got %q", "transcribed voice text", blocks[0].Text.Text)
	}
}

func TestIncomingToPrompt_VoiceMessageWithoutText(t *testing.T) {
	msg := &wechatbot.IncomingMessage{
		UserID: "user1",
		Type:   wechatbot.ContentVoice,
		Text:   "",
		Voices: []wechatbot.VoiceContent{{}},
	}

	blocks := IncomingToPrompt(context.Background(), msg, nil, false, "")

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Text == nil {
		t.Fatal("expected Text block")
	}
	expected := "[Received voice message, no transcription available]"
	if blocks[0].Text.Text != expected {
		t.Errorf("expected %q, got %q", expected, blocks[0].Text.Text)
	}
}

func TestIncomingToPrompt_VideoMessage(t *testing.T) {
	msg := &wechatbot.IncomingMessage{
		UserID: "user1",
		Type:   wechatbot.ContentVideo,
		Videos: []wechatbot.VideoContent{{}},
	}

	blocks := IncomingToPrompt(context.Background(), msg, nil, false, "")

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Text == nil {
		t.Fatal("expected Text block")
	}
	expected := "[Received video message]"
	if blocks[0].Text.Text != expected {
		t.Errorf("expected %q, got %q", expected, blocks[0].Text.Text)
	}
}

func TestIncomingToPrompt_FileMessageNonTextExt(t *testing.T) {
	msg := &wechatbot.IncomingMessage{
		UserID: "user1",
		Type:   wechatbot.ContentFile,
		Files: []wechatbot.FileContent{
			{FileName: "photo.png"},
		},
	}

	blocks := IncomingToPrompt(context.Background(), msg, nil, false, "")

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Text == nil {
		t.Fatal("expected Text block")
	}
	expected := "[Received file: photo.png]"
	if blocks[0].Text.Text != expected {
		t.Errorf("expected %q, got %q", expected, blocks[0].Text.Text)
	}
}

func TestExtractText(t *testing.T) {
	tests := []struct {
		name     string
		msg      *wechatbot.IncomingMessage
		expected string
	}{
		{
			name: "text message plain",
			msg: &wechatbot.IncomingMessage{
				Type: wechatbot.ContentText,
				Text: "hello",
			},
			expected: "hello",
		},
		{
			name: "voice with transcription",
			msg: &wechatbot.IncomingMessage{
				Type:   wechatbot.ContentVoice,
				Text:   "voice text",
				Voices: []wechatbot.VoiceContent{{}},
			},
			expected: "voice text",
		},
		{
			name: "voice without voices slice",
			msg: &wechatbot.IncomingMessage{
				Type: wechatbot.ContentVoice,
				Text: "some text",
			},
			expected: "",
		},
		{
			name: "voice with empty text",
			msg: &wechatbot.IncomingMessage{
				Type:   wechatbot.ContentVoice,
				Text:   "",
				Voices: []wechatbot.VoiceContent{{}},
			},
			expected: "",
		},
		{
			name: "default type returns text",
			msg: &wechatbot.IncomingMessage{
				Type: wechatbot.ContentFile,
				Text: "file description",
			},
			expected: "file description",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractText(tt.msg)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestMimeTypeFromExt(t *testing.T) {
	tests := []struct {
		ext      string
		expected string
	}{
		{".json", "application/json"},
		{".html", "text/html"},
		{".xml", "text/xml"},
		{".css", "text/css"},
		{".js", "application/javascript"},
		{".ts", "application/typescript"},
		{".md", "text/markdown"},
		{".csv", "text/csv"},
		{".yaml", "text/yaml"},
		{".yml", "text/yaml"},
		{".txt", "text/plain"},
		{".go", "text/plain"},
		{".unknown", "text/plain"},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			got := mimeTypeFromExt(tt.ext)
			if got != tt.expected {
				t.Errorf("mimeTypeFromExt(%q) = %q, want %q", tt.ext, got, tt.expected)
			}
		})
	}
}

func TestTextFileExts(t *testing.T) {
	// Verify some known text extensions are in the map
	exts := []string{".txt", ".md", ".json", ".go", ".py", ".js", ".ts", ".yaml", ".yml"}
	for _, ext := range exts {
		if !textFileExts[ext] {
			t.Errorf("expected %q to be in textFileExts", ext)
		}
	}

	// Verify non-text extensions are not in the map
	nonText := []string{".png", ".jpg", ".exe", ".zip", ".pdf"}
	for _, ext := range nonText {
		if textFileExts[ext] {
			t.Errorf("expected %q to NOT be in textFileExts", ext)
		}
	}
}
