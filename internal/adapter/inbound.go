package adapter

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	sdk "github.com/coder/acp-go-sdk"
	wechatbot "github.com/corespeed-io/wechatbot/golang"
)

// textFileExts lists extensions considered as text files for resource conversion.
var textFileExts = map[string]bool{
	".txt": true, ".md": true, ".json": true, ".js": true, ".ts": true,
	".py": true, ".java": true, ".c": true, ".cpp": true, ".h": true,
	".css": true, ".html": true, ".xml": true, ".yaml": true, ".yml": true,
	".toml": true, ".ini": true, ".cfg": true, ".sh": true, ".bash": true,
	".rs": true, ".go": true, ".rb": true, ".php": true, ".sql": true,
	".csv": true, ".log": true, ".env": true,
}

// IncomingToPrompt converts a wechatbot IncomingMessage to ACP ContentBlocks.
// If isGroup is true, sender identity is prepended.
func IncomingToPrompt(
	ctx context.Context,
	msg *wechatbot.IncomingMessage,
	bot *wechatbot.Bot,
	isGroup bool,
	senderName string,
) []sdk.ContentBlock {
	var blocks []sdk.ContentBlock

	// Inject sender identity for group messages
	if isGroup && senderName != "" {
		blocks = append(blocks, sdk.TextBlock(fmt.Sprintf("[%s says]:", senderName)))
	}

	// Extract text content
	text := extractText(msg)
	if text != "" {
		blocks = append(blocks, sdk.TextBlock(text))
	}

	// Extract media content
	mediaBlock := convertMedia(ctx, msg, bot)
	if mediaBlock != nil {
		blocks = append(blocks, *mediaBlock)
	}

	// Fallback for empty messages
	if len(blocks) == 0 || (len(blocks) == 1 && isGroup) {
		blocks = append(blocks, sdk.TextBlock("[empty message]"))
	}

	return blocks
}

func extractText(msg *wechatbot.IncomingMessage) string {
	switch msg.Type {
	case wechatbot.ContentText:
		text := msg.Text
		if msg.QuotedMessage != nil {
			var parts []string
			if msg.QuotedMessage.Title != "" {
				parts = append(parts, msg.QuotedMessage.Title)
			}
			if msg.QuotedMessage.Text != "" {
				parts = append(parts, msg.QuotedMessage.Text)
			}
			if len(parts) > 0 {
				return fmt.Sprintf("[Quote: %s]\n%s", strings.Join(parts, " | "), text)
			}
		}
		return text

	case wechatbot.ContentVoice:
		if len(msg.Voices) > 0 {
			// Voice transcription text is in the first voice item
			// The SDK stores transcription in a text field if available
			// For now, return the main text field which may contain transcription
			if msg.Text != "" {
				return msg.Text
			}
		}
		return ""

	default:
		return msg.Text
	}
}

func convertMedia(ctx context.Context, msg *wechatbot.IncomingMessage, bot *wechatbot.Bot) *sdk.ContentBlock {
	switch msg.Type {
	case wechatbot.ContentImage:
		if len(msg.Images) == 0 {
			return nil
		}
		downloaded, err := bot.Download(ctx, msg)
		if err != nil {
			block := sdk.TextBlock("[Received image - download failed]")
			return &block
		}
		b64 := base64.StdEncoding.EncodeToString(downloaded.Data)
		mimeType := "image/jpeg" // default; SDK doesn't provide MIME type
		block := sdk.ImageBlock(b64, mimeType)
		return &block

	case wechatbot.ContentFile:
		if len(msg.Files) == 0 {
			return nil
		}
		f := msg.Files[0]
		ext := strings.ToLower(filepath.Ext(f.FileName))
		if textFileExts[ext] {
			downloaded, err := bot.Download(ctx, msg)
			if err != nil {
				block := sdk.TextBlock(fmt.Sprintf("[Received file: %s - download failed]", f.FileName))
				return &block
			}
			block := sdk.ResourceBlock(sdk.EmbeddedResourceResource{
				TextResourceContents: &sdk.TextResourceContents{
					Uri:      "file://" + f.FileName,
					MimeType: Ptr(mimeTypeFromExt(ext)),
					Text:     string(downloaded.Data),
				},
			})
			return &block
		}
		block := sdk.TextBlock(fmt.Sprintf("[Received file: %s]", f.FileName))
		return &block

	case wechatbot.ContentVideo:
		block := sdk.TextBlock("[Received video message]")
		return &block

	case wechatbot.ContentVoice:
		if msg.Text == "" {
			block := sdk.TextBlock("[Received voice message, no transcription available]")
			return &block
		}
		return nil // text already extracted

	default:
		return nil
	}
}

func mimeTypeFromExt(ext string) string {
	switch ext {
	case ".json":
		return "application/json"
	case ".html":
		return "text/html"
	case ".xml":
		return "text/xml"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".ts":
		return "application/typescript"
	case ".md":
		return "text/markdown"
	case ".csv":
		return "text/csv"
	case ".yaml", ".yml":
		return "text/yaml"
	default:
		return "text/plain"
	}
}

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T { return &v }
