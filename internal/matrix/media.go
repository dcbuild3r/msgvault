package matrix

import (
	"context"
	"errors"
	"mime"
	"strings"

	"go.kenn.io/msgvault/internal/export"
	msgmime "go.kenn.io/msgvault/internal/mime"
	"go.kenn.io/msgvault/internal/store"
)

const defaultMaxMediaBytes = int64(100 << 20)

func (imp *Importer) persistMedia(ctx context.Context, messageID int64, content eventContent, opts ImportOptions, sum *ImportSummary) {
	if opts.NoMedia || content.URL == "" || !strings.HasPrefix(content.URL, "mxc://") {
		return
	}
	maxBytes := opts.MaxMediaBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxMediaBytes
	}
	ref := store.AttachmentRef{
		Filename:           content.Body,
		MimeType:           content.Info.MimeType,
		StoragePath:        content.URL,
		Size:               int(content.Info.Size),
		SourceAttachmentID: "matrix:" + content.URL,
		MediaType:          matrixMediaType(content.MsgType),
		Width:              content.Info.Width,
		Height:             content.Info.Height,
		DurationMS:         content.Info.Duration,
	}
	if content.Info.Size <= 0 || content.Info.Size <= maxBytes {
		data, responseType, err := imp.client.DownloadMedia(ctx, content.URL, maxBytes)
		if err == nil && len(data) > 0 {
			if parsed, _, parseErr := mime.ParseMediaType(responseType); parseErr == nil && parsed != "" {
				ref.MimeType = parsed
			}
			attachment := &msgmime.Attachment{Filename: ref.Filename, ContentType: ref.MimeType, Content: data}
			storagePath, storeErr := export.StoreAttachmentFile(opts.AttachmentsDir, attachment)
			if storeErr == nil && storagePath != "" {
				ref.StoragePath = storagePath
				ref.ContentHash = attachment.ContentHash
				ref.Size = len(data)
				sum.AttachmentsDownloaded++
			} else {
				sum.Errors++
			}
		} else if err != nil && !errors.Is(err, ErrMediaTooLarge) {
			sum.Errors++
		}
	}
	if err := imp.store.ReplaceMessageMatrixAttachments(messageID, []store.AttachmentRef{ref}); err != nil {
		sum.Errors++
		return
	}
	if err := imp.store.RecomputeMessageAttachmentStats(messageID); err != nil {
		sum.Errors++
	}
}

func matrixMediaType(msgType string) string {
	switch msgType {
	case "m.image":
		return "image"
	case "m.video":
		return "video"
	case "m.audio":
		return "audio"
	case "m.file":
		return "document"
	default:
		return ""
	}
}
