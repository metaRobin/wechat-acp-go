package adapter

import (
	"strings"
	"testing"

	wechatbot "github.com/corespeed-io/wechatbot/golang"
)

func TestIncomingToText_TextMessage(t *testing.T) {
	msg := &wechatbot.IncomingMessage{
		Type: wechatbot.ContentText,
		Text: "hello world",
	}
	result := IncomingToText(msg, false, "")
	if result != "hello world" {
		t.Errorf("got %q, want %q", result, "hello world")
	}
}

func TestIncomingToText_QuotedMessage(t *testing.T) {
	tests := []struct {
		name  string
		quote *wechatbot.QuotedMessage
		want  string
	}{
		{"title and text", &wechatbot.QuotedMessage{Title: "Alice", Text: "hi"}, "[Quote: Alice | hi]\nhello"},
		{"title only", &wechatbot.QuotedMessage{Title: "Alice"}, "[Quote: Alice]\nhello"},
		{"text only", &wechatbot.QuotedMessage{Text: "hi"}, "[Quote: hi]\nhello"},
		{"empty quote", &wechatbot.QuotedMessage{}, "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &wechatbot.IncomingMessage{
				Type:          wechatbot.ContentText,
				Text:          "hello",
				QuotedMessage: tt.quote,
			}
			result := IncomingToText(msg, false, "")
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

func TestIncomingToText_EmptyFallback(t *testing.T) {
	msg := &wechatbot.IncomingMessage{Type: wechatbot.ContentText}
	result := IncomingToText(msg, false, "")
	if !strings.Contains(result, "[empty message]") {
		t.Errorf("got %q, want [empty message]", result)
	}
}

func TestIncomingToText_GroupSender(t *testing.T) {
	msg := &wechatbot.IncomingMessage{
		Type: wechatbot.ContentText,
		Text: "hello",
	}
	result := IncomingToText(msg, true, "Alice")
	if !strings.Contains(result, "[Alice says]:") {
		t.Errorf("got %q, want to contain [Alice says]:", result)
	}
}

func TestIncomingToText_VoiceWithText(t *testing.T) {
	msg := &wechatbot.IncomingMessage{
		Type:   wechatbot.ContentVoice,
		Text:   "transcribed text",
		Voices: []wechatbot.VoiceContent{{}},
	}
	result := IncomingToText(msg, false, "")
	if !strings.Contains(result, "transcribed text") {
		t.Errorf("got %q, want transcribed text", result)
	}
}

func TestIncomingToText_VoiceNoText(t *testing.T) {
	msg := &wechatbot.IncomingMessage{
		Type:   wechatbot.ContentVoice,
		Voices: []wechatbot.VoiceContent{{}},
	}
	result := IncomingToText(msg, false, "")
	if !strings.Contains(result, "no transcription") {
		t.Errorf("got %q, want voice no transcription message", result)
	}
}

func TestIncomingToText_Video(t *testing.T) {
	msg := &wechatbot.IncomingMessage{Type: wechatbot.ContentVideo}
	result := IncomingToText(msg, false, "")
	if !strings.Contains(result, "video") {
		t.Errorf("got %q, want video message", result)
	}
}

func TestIncomingToText_File(t *testing.T) {
	msg := &wechatbot.IncomingMessage{
		Type:  wechatbot.ContentFile,
		Files: []wechatbot.FileContent{{FileName: "doc.pdf"}},
	}
	result := IncomingToText(msg, false, "")
	if !strings.Contains(result, "doc.pdf") {
		t.Errorf("got %q, want file name", result)
	}
}

func TestIncomingToText_Image(t *testing.T) {
	msg := &wechatbot.IncomingMessage{
		Type:   wechatbot.ContentImage,
		Images: []wechatbot.ImageContent{{}},
	}
	result := IncomingToText(msg, false, "")
	if !strings.Contains(result, "image") {
		t.Errorf("got %q, want image message", result)
	}
}

func TestExtractText(t *testing.T) {
	tests := []struct {
		name string
		msg  *wechatbot.IncomingMessage
		want string
	}{
		{"text", &wechatbot.IncomingMessage{Type: wechatbot.ContentText, Text: "hi"}, "hi"},
		{"voice with text", &wechatbot.IncomingMessage{Type: wechatbot.ContentVoice, Text: "spoken"}, "spoken"},
		{"voice empty", &wechatbot.IncomingMessage{Type: wechatbot.ContentVoice}, ""},
		{"default", &wechatbot.IncomingMessage{Type: "unknown", Text: "fallback"}, "fallback"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractText(tt.msg)
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}
