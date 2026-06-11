package runtime

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	pdftext "github.com/multica-ai/multica/packages/cerebro-pdf-text"
	"github.com/multica-ai/multica/server/internal/storage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const firtalGatewayAttachmentMaxBytes = 10 << 20

// FirtalGatewayAttachmentStorage builds the storage backend used to fetch
// attachment bytes server-side: S3 when configured, else the local store.
// Lives in the cerebro zone so the cmd/server wiring stays a one-line call.
func FirtalGatewayAttachmentStorage() storage.Storage {
	if s3 := storage.NewS3StorageFromEnv(); s3 != nil {
		return s3
	}
	return storage.NewLocalStorageFromEnv()
}

func (e *FirtalGatewayExecutor) loadChatAttachments(ctx context.Context, workspaceID pgtype.UUID, history []db.ChatMessage) map[pgtype.UUID][]AnthropicContentBlock {
	if e.queries == nil || e.attachmentStorage == nil {
		return nil
	}
	ids := make([]pgtype.UUID, 0, len(history))
	for _, m := range history {
		if m.ID.Valid {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	attachments, err := e.queries.ListAttachmentsByChatMessageIDs(ctx, db.ListAttachmentsByChatMessageIDsParams{
		Column1:     ids,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		e.attachmentLogger().Warn("firtal gateway chat attachment lookup failed", "error", err)
		return nil
	}
	return e.attachmentBlocksByOwner(ctx, attachments, func(a db.Attachment) pgtype.UUID { return a.ChatMessageID })
}

func (e *FirtalGatewayExecutor) loadCommentAttachments(ctx context.Context, workspaceID pgtype.UUID, comments []db.Comment) map[pgtype.UUID][]AnthropicContentBlock {
	if e.queries == nil || e.attachmentStorage == nil {
		return nil
	}
	ids := make([]pgtype.UUID, 0, len(comments))
	for _, c := range comments {
		if c.ID.Valid {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	attachments, err := e.queries.ListAttachmentsByCommentIDs(ctx, db.ListAttachmentsByCommentIDsParams{
		Column1:     ids,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		e.attachmentLogger().Warn("firtal gateway comment attachment lookup failed", "error", err)
		return nil
	}
	return e.attachmentBlocksByOwner(ctx, attachments, func(a db.Attachment) pgtype.UUID { return a.CommentID })
}

func (e *FirtalGatewayExecutor) attachmentBlocksByOwner(ctx context.Context, attachments []db.Attachment, owner func(db.Attachment) pgtype.UUID) map[pgtype.UUID][]AnthropicContentBlock {
	out := make(map[pgtype.UUID][]AnthropicContentBlock)
	for _, att := range attachments {
		ownerID := owner(att)
		if !ownerID.Valid {
			continue
		}
		blocks, err := gatewayAttachmentBlocks(ctx, e.attachmentStorage, att)
		if err != nil {
			e.attachmentLogger().Warn("firtal gateway attachment skipped", "filename", att.Filename, "content_type", att.ContentType, "error", err)
			continue
		}
		out[ownerID] = append(out[ownerID], blocks...)
	}
	return out
}

func gatewayAttachmentBlocks(ctx context.Context, store storage.Storage, att db.Attachment) ([]AnthropicContentBlock, error) {
	if store == nil {
		return nil, fmt.Errorf("storage not configured")
	}
	if att.SizeBytes > firtalGatewayAttachmentMaxBytes {
		return []AnthropicContentBlock{{Type: "text", Text: fmt.Sprintf("Attachment %q was skipped because it is larger than the gateway limit.", att.Filename)}}, nil
	}
	key := store.KeyFromURL(att.Url)
	if key == "" {
		return nil, fmt.Errorf("attachment storage key is empty")
	}
	reader, err := store.GetReader(ctx, key)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, firtalGatewayAttachmentMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > firtalGatewayAttachmentMaxBytes {
		return []AnthropicContentBlock{{Type: "text", Text: fmt.Sprintf("Attachment %q was skipped because it is larger than the gateway limit.", att.Filename)}}, nil
	}

	mediaType := normalizedAttachmentMediaType(att.ContentType, att.Filename)
	switch {
	case strings.HasPrefix(mediaType, "image/"):
		return []AnthropicContentBlock{{
			Type: "image",
			Source: &AnthropicContentSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      base64.StdEncoding.EncodeToString(body),
			},
		}}, nil
	case mediaType == "application/pdf":
		return []AnthropicContentBlock{{
			Type: "document",
			Source: &AnthropicContentSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      base64.StdEncoding.EncodeToString(body),
			},
		}}, nil
	case isDocxAttachment(mediaType, att.Filename):
		text, err := extractDocxText(body)
		if err != nil {
			return nil, err
		}
		return attachmentTextBlocks(att.Filename, text), nil
	case isXlsxAttachment(mediaType, att.Filename):
		text, err := extractXlsxText(body)
		if err != nil {
			return nil, err
		}
		return attachmentTextBlocks(att.Filename, text), nil
	default:
		if text := strings.TrimSpace(string(body)); text != "" && strings.HasPrefix(mediaType, "text/") {
			return attachmentTextBlocks(att.Filename, text), nil
		}
		if mediaType == "application/pdf" {
			text, err := pdftext.Extract(body, firtalGatewayAttachmentMaxBytes)
			if err == nil {
				return attachmentTextBlocks(att.Filename, string(text)), nil
			}
		}
		return nil, fmt.Errorf("unsupported attachment type %q", mediaType)
	}
}

func attachmentTextBlocks(filename, text string) []AnthropicContentBlock {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if filename == "" {
		filename = "attachment"
	}
	return []AnthropicContentBlock{{Type: "text", Text: fmt.Sprintf("Attachment %q contents:\n%s", filename, text)}}
}

func normalizedAttachmentMediaType(contentType, filename string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	if ct != "" && ct != "application/octet-stream" {
		return ct
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".txt", ".md", ".csv":
		return "text/plain"
	default:
		return ct
	}
}

func isDocxAttachment(mediaType, filename string) bool {
	return mediaType == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" || strings.EqualFold(filepath.Ext(filename), ".docx")
}

func isXlsxAttachment(mediaType, filename string) bool {
	return mediaType == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" || strings.EqualFold(filepath.Ext(filename), ".xlsx")
}

func extractDocxText(body []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("open docx: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		data, err := readZipFile(f)
		if err != nil {
			return "", err
		}
		return xmlText(data, "t"), nil
	}
	return "", fmt.Errorf("docx document.xml not found")
}

func extractXlsxText(body []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("open xlsx: %w", err)
	}
	var rows []string
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "xl/worksheets/") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		data, err := readZipFile(f)
		if err != nil {
			return "", err
		}
		text := strings.TrimSpace(xmlText(data, "v"))
		if text != "" {
			rows = append(rows, filepath.Base(f.Name)+": "+text)
		}
	}
	sort.Strings(rows)
	if len(rows) == 0 {
		return "", fmt.Errorf("xlsx worksheet text not found")
	}
	return strings.Join(rows, "\n"), nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, firtalGatewayAttachmentMaxBytes+1))
}

func xmlText(data []byte, element string) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var parts []string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != element {
			continue
		}
		var value string
		if err := dec.DecodeElement(&value, &start); err == nil {
			value = strings.TrimSpace(value)
			if value != "" {
				parts = append(parts, value)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func (e *FirtalGatewayExecutor) attachmentLogger() *slog.Logger {
	if e != nil && e.logger != nil {
		return e.logger
	}
	return slog.Default()
}
