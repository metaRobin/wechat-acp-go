package adapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	wechatbot "github.com/corespeed-io/wechatbot/golang"
)

// MediaResult holds the result of downloading and saving a media file.
type MediaResult struct {
	Path     string // local file path where media was saved
	FileName string // original file name
	Type     string // "image", "file", "video", "voice"
}

// DownloadAndSave downloads media from a WeChat message and saves it to disk.
// Returns nil if the message has no downloadable media.
func DownloadAndSave(ctx context.Context, msg *wechatbot.IncomingMessage, bot *wechatbot.Bot, mediaDir, sessionKey string) (*MediaResult, error) {
	downloaded, err := bot.Download(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	if downloaded == nil {
		return nil, nil
	}

	// Determine file name
	fileName := downloaded.FileName
	if fileName == "" {
		fileName = fmt.Sprintf("%d.%s", time.Now().UnixMilli(), defaultExt(downloaded.Type))
	}

	// Create session media directory
	dir := filepath.Join(mediaDir, sessionKey)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create media dir: %w", err)
	}

	// Save with timestamp prefix to avoid collisions
	saveName := fmt.Sprintf("%d-%s", time.Now().UnixMilli(), fileName)
	savePath := filepath.Join(dir, saveName)

	if err := os.WriteFile(savePath, downloaded.Data, 0o644); err != nil {
		return nil, fmt.Errorf("save media: %w", err)
	}

	return &MediaResult{
		Path:     savePath,
		FileName: fileName,
		Type:     downloaded.Type,
	}, nil
}

// CleanupMedia removes the media directory for a session.
func CleanupMedia(mediaDir, sessionKey string) {
	dir := filepath.Join(mediaDir, sessionKey)
	os.RemoveAll(dir)
}

func defaultExt(mediaType string) string {
	switch mediaType {
	case "image":
		return "jpg"
	case "voice":
		return "silk"
	case "video":
		return "mp4"
	default:
		return "bin"
	}
}
