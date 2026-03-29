package router

import (
	"log/slog"
	"strings"

	wechatbot "github.com/corespeed-io/wechatbot/golang"

	"github.com/anthropic/wechat-acp-go/internal/config"
	"github.com/anthropic/wechat-acp-go/internal/session"
)

// Router determines how incoming WeChat messages are routed to ACP sessions.
type Router struct {
	botName string
	group   config.GroupConfig
	mgr     *session.Manager
	bot     *wechatbot.Bot
	logger  *slog.Logger
}

// NewRouter creates a router for the given bot configuration.
func NewRouter(botName string, group config.GroupConfig, mgr *session.Manager, bot *wechatbot.Bot, logger *slog.Logger) *Router {
	return &Router{
		botName: botName,
		group:   group,
		mgr:     mgr,
		bot:     bot,
		logger:  logger,
	}
}

// Route processes an incoming WeChat message and enqueues it to the appropriate session.
// This is designed to be called as a wechatbot.MessageHandler.
func (r *Router) Route(msg *wechatbot.IncomingMessage) {
	// NOTE: Current SDK only supports private chat (no GroupID field).
	// Group chat routing is architecturally prepared but inactive.
	// When SDK adds group support, isGroup detection goes here.
	isGroup := false

	sessionKey := r.sessionKey(msg, isGroup)
	replyTarget := r.replyTarget(msg, isGroup)

	// Extract text for the agent prompt
	promptText := msg.Text
	if promptText == "" {
		promptText = "[empty message]"
	}

	err := r.mgr.Enqueue(sessionKey, session.PendingMessage{
		Text:         promptText,
		ContextToken: replyTarget,
	})
	if err != nil {
		r.logger.Error("enqueue_failed", "key", sessionKey, "error", err)
	}
}

// sessionKey computes the session map key.
// Private chat: "u:{userID}", Group chat: "g:{groupID}" (future).
func (r *Router) sessionKey(msg *wechatbot.IncomingMessage, isGroup bool) string {
	// Future: if isGroup { return "g:" + msg.GroupID }
	return "u:" + msg.UserID
}

// replyTarget returns the ID to send replies to.
// For private chats this is the user ID; for groups it would be the group ID.
func (r *Router) replyTarget(msg *wechatbot.IncomingMessage, isGroup bool) string {
	return msg.UserID
}

// --- Group chat helpers (prepared for future SDK support) ---

// CheckGroupTrigger checks if a group message text should trigger the bot.
// Returns (triggered, cleanedText).
func (r *Router) CheckGroupTrigger(text string) (bool, string) {
	switch r.group.Trigger {
	case "all":
		return true, text

	case "@bot":
		return r.stripAtBot(text)

	default:
		// Treat trigger as a keyword prefix
		keyword := r.group.Trigger
		if strings.HasPrefix(text, keyword) {
			cleaned := strings.TrimSpace(strings.TrimPrefix(text, keyword))
			if cleaned == "" {
				cleaned = text
			}
			return true, cleaned
		}
		return false, ""
	}
}

// stripAtBot checks for @bot mention and strips it from the text.
// WeChat @mentions use special whitespace (U+2005, U+00A0) after the mention.
func (r *Router) stripAtBot(text string) (bool, string) {
	botMention := "@" + r.botName

	if !strings.Contains(text, botMention) {
		return false, ""
	}

	cleaned := strings.Replace(text, botMention, "", 1)
	cleaned = strings.TrimLeft(cleaned, " \t\u2005\u00a0")
	cleaned = strings.TrimSpace(cleaned)

	if cleaned == "" {
		cleaned = "[mentioned bot]"
	}

	return true, cleaned
}
