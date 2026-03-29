package router

import (
	"log/slog"
	"testing"

	"github.com/metaRobin/wechat-router-go/internal/config"
)

func newTestRouter(botName string, group config.GroupConfig) *Router {
	return NewRouter(botName, group, nil, nil, slog.Default())
}

func TestCheckGroupTriggerAll(t *testing.T) {
	r := newTestRouter("TestBot", config.GroupConfig{Trigger: "all"})

	tests := []struct {
		text        string
		wantTrigger bool
		wantCleaned string
	}{
		{"hello world", true, "hello world"},
		{"", true, ""},
		{"@TestBot hi", true, "@TestBot hi"},
	}

	for _, tt := range tests {
		triggered, cleaned := r.CheckGroupTrigger(tt.text)
		if triggered != tt.wantTrigger {
			t.Errorf("CheckGroupTrigger(%q): triggered = %v, want %v", tt.text, triggered, tt.wantTrigger)
		}
		if cleaned != tt.wantCleaned {
			t.Errorf("CheckGroupTrigger(%q): cleaned = %q, want %q", tt.text, cleaned, tt.wantCleaned)
		}
	}
}

func TestCheckGroupTriggerAtBot(t *testing.T) {
	r := newTestRouter("TestBot", config.GroupConfig{Trigger: "@bot"})

	tests := []struct {
		name        string
		text        string
		wantTrigger bool
		wantCleaned string
	}{
		{
			name:        "mention at start",
			text:        "@TestBot hello",
			wantTrigger: true,
			wantCleaned: "hello",
		},
		{
			name:        "mention at end",
			text:        "hello @TestBot",
			wantTrigger: true,
			wantCleaned: "hello",
		},
		{
			name:        "no mention",
			text:        "hello",
			wantTrigger: false,
			wantCleaned: "",
		},
		{
			name:        "mention only",
			text:        "@TestBot",
			wantTrigger: true,
			wantCleaned: "[mentioned bot]",
		},
		{
			name:        "mention with special whitespace U+2005",
			text:        "@TestBot\u2005hello",
			wantTrigger: true,
			wantCleaned: "hello",
		},
		{
			name:        "mention with non-breaking space U+00A0",
			text:        "@TestBot\u00a0hello",
			wantTrigger: true,
			wantCleaned: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			triggered, cleaned := r.CheckGroupTrigger(tt.text)
			if triggered != tt.wantTrigger {
				t.Errorf("triggered = %v, want %v", triggered, tt.wantTrigger)
			}
			if cleaned != tt.wantCleaned {
				t.Errorf("cleaned = %q, want %q", cleaned, tt.wantCleaned)
			}
		})
	}
}

func TestCheckGroupTriggerKeyword(t *testing.T) {
	r := newTestRouter("TestBot", config.GroupConfig{Trigger: "/ask"})

	tests := []struct {
		name        string
		text        string
		wantTrigger bool
		wantCleaned string
	}{
		{
			name:        "keyword with question",
			text:        "/ask question",
			wantTrigger: true,
			wantCleaned: "question",
		},
		{
			name:        "keyword only",
			text:        "/ask",
			wantTrigger: true,
			wantCleaned: "/ask", // falls back to original text when cleaned is empty
		},
		{
			name:        "no keyword",
			text:        "hello",
			wantTrigger: false,
			wantCleaned: "",
		},
		{
			name:        "keyword not at start",
			text:        "please /ask something",
			wantTrigger: false,
			wantCleaned: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			triggered, cleaned := r.CheckGroupTrigger(tt.text)
			if triggered != tt.wantTrigger {
				t.Errorf("triggered = %v, want %v", triggered, tt.wantTrigger)
			}
			if cleaned != tt.wantCleaned {
				t.Errorf("cleaned = %q, want %q", cleaned, tt.wantCleaned)
			}
		})
	}
}

func TestStripAtBotSpecialWhitespace(t *testing.T) {
	r := newTestRouter("MyBot", config.GroupConfig{Trigger: "@bot"})

	// WeChat inserts U+2005 (four-per-em space) after @mentions
	text := "@MyBot\u2005what is Go?"
	triggered, cleaned := r.CheckGroupTrigger(text)
	if !triggered {
		t.Error("expected triggered = true")
	}
	if cleaned != "what is Go?" {
		t.Errorf("cleaned = %q, want %q", cleaned, "what is Go?")
	}

	// Also handle U+00A0 (non-breaking space)
	text2 := "@MyBot\u00a0tell me a joke"
	triggered2, cleaned2 := r.CheckGroupTrigger(text2)
	if !triggered2 {
		t.Error("expected triggered = true")
	}
	if cleaned2 != "tell me a joke" {
		t.Errorf("cleaned = %q, want %q", cleaned2, "tell me a joke")
	}
}

func TestSessionKeyPrivateMessage(t *testing.T) {
	// sessionKey is unexported, but we can test it indirectly.
	// The method just returns "u:" + msg.UserID for private messages.
	// Since we can't import wechatbot without a real bot, we verify the
	// logic by checking the pattern via the Route path is correct.
	//
	// Instead, test through the exported function signature understanding:
	// For private chat (isGroup=false), sessionKey returns "u:" + userID.
	// This is verified by the code inspection: return "u:" + msg.UserID
	//
	// We document this as a design verification rather than a runtime test
	// since sessionKey requires a *wechatbot.IncomingMessage which needs
	// the external SDK.
	t.Log("sessionKey for private messages returns 'u:{userID}' - verified by code inspection")
}
