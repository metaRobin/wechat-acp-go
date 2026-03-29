package router

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	wechatbot "github.com/corespeed-io/wechatbot/golang"

	"github.com/metaRobin/wechat-router-go/internal/adapter"
	"github.com/metaRobin/wechat-router-go/internal/config"
	"github.com/metaRobin/wechat-router-go/internal/session"
)

// Router determines how incoming WeChat messages are routed to ACP sessions.
type Router struct {
	botName      string
	group        config.GroupConfig
	mgr          *session.Manager
	bot          *wechatbot.Bot
	customAgents map[string]config.AgentPreset
	mediaDir     string // directory for saving downloaded media
	logger       *slog.Logger
}

// NewRouter creates a router for the given bot configuration.
func NewRouter(botName string, group config.GroupConfig, mgr *session.Manager, bot *wechatbot.Bot, customAgents map[string]config.AgentPreset, mediaDir string, logger *slog.Logger) *Router {
	return &Router{
		botName:      botName,
		group:        group,
		mgr:          mgr,
		bot:          bot,
		customAgents: customAgents,
		mediaDir:     mediaDir,
		logger:       logger,
	}
}

// Route processes an incoming WeChat message and enqueues it to the appropriate session.
func (r *Router) Route(msg *wechatbot.IncomingMessage) {
	isGroup := false
	sessionKey := r.sessionKey(msg, isGroup)
	replyTarget := r.replyTarget(msg, isGroup)

	text := msg.Text

	// Check for commands first (only on text messages)
	if cmd := ParseCommand(text); cmd != nil {
		r.handleCommand(cmd, sessionKey, replyTarget)
		return
	}

	// Determine agent ID for this message
	agentID := r.mgr.GetSessionAgentID(sessionKey)
	if agentID == "" {
		agentID = r.mgr.DefaultAgent()
	}

	// If still no agent, check store for previously persisted agent
	if agentID == "" {
		agentID = r.restoreAgentFromStore(sessionKey)
	}

	// If still no agent, prompt user to select
	if agentID == "" {
		r.sendReply(replyTarget, config.FormatAgentList(r.customAgents)+"\n\n请发送 /use <名称> 来选择一个 AI Agent")
		return
	}

	// Build prompt text with media info if present
	promptText := r.buildPromptText(msg, sessionKey)

	err := r.mgr.Enqueue(sessionKey, session.PendingMessage{
		Text:         promptText,
		ContextToken: replyTarget,
	}, agentID)
	if err != nil {
		r.logger.Error("enqueue_failed", "key", sessionKey, "error", err)
	}
}

func (r *Router) buildPromptText(msg *wechatbot.IncomingMessage, sessionKey string) string {
	var parts []string

	// Text content
	if msg.Text != "" {
		parts = append(parts, msg.Text)
	}

	// Try to download and save media
	if r.mediaDir != "" && r.bot != nil && hasMedia(msg) {
		result, err := adapter.DownloadAndSave(context.Background(), msg, r.bot, r.mediaDir, sessionKey)
		if err != nil {
			r.logger.Debug("media_download_failed", "error", err)
			switch msg.Type {
			case wechatbot.ContentImage:
				parts = append(parts, "[用户发送了图片，下载失败]")
			case wechatbot.ContentFile:
				name := "file"
				if len(msg.Files) > 0 {
					name = msg.Files[0].FileName
				}
				parts = append(parts, fmt.Sprintf("[用户发送了文件: %s，下载失败]", name))
			}
		} else if result != nil {
			switch result.Type {
			case "image":
				parts = append(parts, fmt.Sprintf("[用户发送了图片, 已保存到: %s]", result.Path))
			default:
				parts = append(parts, fmt.Sprintf("[用户发送了文件: %s, 已保存到: %s]", result.FileName, result.Path))
			}
		}
	} else if hasMedia(msg) {
		// No mediaDir configured, fall back to description
		parts = append(parts, adapter.IncomingToText(msg, false, ""))
		return strings.Join(parts, "\n")
	}

	if len(parts) == 0 {
		return "[empty message]"
	}
	return strings.Join(parts, "\n")
}

func hasMedia(msg *wechatbot.IncomingMessage) bool {
	return msg.Type == wechatbot.ContentImage ||
		msg.Type == wechatbot.ContentFile ||
		msg.Type == wechatbot.ContentVideo ||
		msg.Type == wechatbot.ContentVoice
}

func (r *Router) handleCommand(cmd *Command, sessionKey, replyTarget string) {
	switch cmd.Name {
	case "use":
		r.handleUse(cmd.Arg, sessionKey, replyTarget)
	case "agents":
		r.sendReply(replyTarget, config.FormatAgentList(r.customAgents))
	case "status":
		r.handleStatus(sessionKey, replyTarget)
	case "clear":
		r.handleClear(sessionKey, replyTarget)
	}
}

func (r *Router) handleUse(agentName, sessionKey, replyTarget string) {
	if agentName == "" {
		r.sendReply(replyTarget, "用法: /use <agent名称>\n\n"+config.FormatAgentList(r.customAgents))
		return
	}

	preset, exists := config.LookupAgent(agentName, r.customAgents)
	if !exists {
		r.sendReply(replyTarget, fmt.Sprintf("未找到 agent: %s\n\n%s", agentName, config.FormatAgentList(r.customAgents)))
		return
	}

	// Check if already using this agent
	currentAgent := r.mgr.GetSessionAgentID(sessionKey)
	if currentAgent == agentName {
		r.sendReply(replyTarget, fmt.Sprintf("当前已在使用 %s", preset.Label))
		return
	}

	// Switch or create session
	if err := r.mgr.SwitchAgent(sessionKey, agentName); err != nil {
		r.sendReply(replyTarget, "切换 agent 失败: "+err.Error())
		return
	}

	if currentAgent != "" {
		r.sendReply(replyTarget, fmt.Sprintf("已切换到 %s，对话历史已清除", preset.Label))
	} else {
		r.sendReply(replyTarget, fmt.Sprintf("已选择 %s，可以开始对话了", preset.Label))
	}
}

func (r *Router) handleStatus(sessionKey, replyTarget string) {
	agentID := r.mgr.GetSessionAgentID(sessionKey)
	if agentID == "" {
		r.sendReply(replyTarget, "当前没有活跃的会话\n\n发送 /use <名称> 选择一个 AI Agent")
		return
	}
	label := agentID
	if p, ok := config.LookupAgent(agentID, r.customAgents); ok {
		label = p.Label
	}
	r.sendReply(replyTarget, fmt.Sprintf("当前 Agent: %s (%s)", label, agentID))
}

func (r *Router) handleClear(sessionKey, replyTarget string) {
	if !r.mgr.HasSession(sessionKey) {
		r.sendReply(replyTarget, "当前没有活跃的会话")
		return
	}
	r.mgr.RemoveSession(sessionKey)
	r.sendReply(replyTarget, "会话已清除，对话历史已删除")
}

func (r *Router) restoreAgentFromStore(sessionKey string) string {
	if r.mgr == nil {
		return ""
	}
	// Check if store has a persisted agent for this session
	// This is accessed via the Manager's store reference
	return "" // Store integration handled at Manager level via GetSession
}

func (r *Router) sendReply(target, text string) {
	if err := r.bot.Send(context.Background(), target, text); err != nil {
		r.logger.Error("command_reply_failed", "target", target, "error", err)
	}
}

// sessionKey computes the session map key.
func (r *Router) sessionKey(msg *wechatbot.IncomingMessage, isGroup bool) string {
	return "u:" + msg.UserID
}

// replyTarget returns the ID to send replies to.
func (r *Router) replyTarget(msg *wechatbot.IncomingMessage, isGroup bool) string {
	return msg.UserID
}

// --- Group chat helpers ---

// CheckGroupTrigger checks if a group message text should trigger the bot.
func (r *Router) CheckGroupTrigger(text string) (bool, string) {
	switch r.group.Trigger {
	case "all":
		return true, text
	case "@bot":
		return r.stripAtBot(text)
	default:
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
