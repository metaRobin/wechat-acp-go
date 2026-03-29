package adapter

import (
	"fmt"
	"strings"

	wechatbot "github.com/corespeed-io/wechatbot/golang"
)

// IncomingToText extracts plain text from a WeChat IncomingMessage.
// If isGroup is true, sender identity is prepended.
func IncomingToText(msg *wechatbot.IncomingMessage, isGroup bool, senderName string) string {
	var parts []string

	if isGroup && senderName != "" {
		parts = append(parts, fmt.Sprintf("[%s says]:", senderName))
	}

	text := extractText(msg)
	if text != "" {
		parts = append(parts, text)
	}

	mediaText := describeMedia(msg)
	if mediaText != "" {
		parts = append(parts, mediaText)
	}

	if len(parts) == 0 || (len(parts) == 1 && isGroup) {
		parts = append(parts, "[empty message]")
	}

	return strings.Join(parts, "\n")
}

func extractText(msg *wechatbot.IncomingMessage) string {
	switch msg.Type {
	case wechatbot.ContentText:
		text := msg.Text
		if msg.QuotedMessage != nil {
			var qparts []string
			if msg.QuotedMessage.Title != "" {
				qparts = append(qparts, msg.QuotedMessage.Title)
			}
			if msg.QuotedMessage.Text != "" {
				qparts = append(qparts, msg.QuotedMessage.Text)
			}
			if len(qparts) > 0 {
				return fmt.Sprintf("[Quote: %s]\n%s", strings.Join(qparts, " | "), text)
			}
		}
		return text

	case wechatbot.ContentVoice:
		if msg.Text != "" {
			return msg.Text
		}
		return ""

	default:
		return msg.Text
	}
}

func describeMedia(msg *wechatbot.IncomingMessage) string {
	switch msg.Type {
	case wechatbot.ContentImage:
		return "[Received image]"
	case wechatbot.ContentFile:
		if len(msg.Files) > 0 {
			return fmt.Sprintf("[Received file: %s]", msg.Files[0].FileName)
		}
		return "[Received file]"
	case wechatbot.ContentVideo:
		return "[Received video message]"
	case wechatbot.ContentVoice:
		if msg.Text == "" {
			return "[Received voice message, no transcription available]"
		}
		return ""
	default:
		return ""
	}
}
