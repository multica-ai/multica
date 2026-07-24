package lark

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/storage"
)

const (
	maxInboundMessageBytes  int64 = 100 << 20
	maxInboundFilenameBytes       = 180
)

type FeishuMediaResolver struct {
	downloader  MessageResourceDownloader
	credentials CredentialsResolver
	storage     storage.Storage
	logger      *slog.Logger
}

func NewFeishuMediaResolver(downloader MessageResourceDownloader, credentials CredentialsResolver, objectStorage storage.Storage, logger *slog.Logger) *FeishuMediaResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &FeishuMediaResolver{downloader: downloader, credentials: credentials, storage: objectStorage, logger: logger}
}

func (r *FeishuMediaResolver) Resolve(ctx context.Context, inst engine.ResolvedInstallation, _ engine.ResolvedIdentity, msg channel.InboundMessage) (channel.InboundMessage, func(context.Context), error) {
	lm, err := larkMsgFromRaw(msg)
	if err != nil {
		return msg, nil, err
	}
	if len(lm.Resources) == 0 {
		return msg, nil, nil
	}
	if r == nil || r.downloader == nil || r.credentials == nil || r.storage == nil {
		return msg, nil, errors.New("lark media resolver is not configured")
	}
	larkInst, ok := inst.Platform.(Installation)
	if !ok {
		return msg, nil, errors.New("lark media resolver received invalid installation")
	}
	secret, err := r.credentials.DecryptAppSecret(larkInst)
	if err != nil {
		return msg, nil, &messageResourceError{retryable: true, category: "credentials unavailable", cause: err}
	}
	creds := InstallationCredentials{AppID: larkInst.AppID, AppSecret: secret, Region: RegionOrDefault(larkInst.Region)}
	if larkInst.TenantKey.Valid {
		creds.TenantKey = larkInst.TenantKey.String
	}

	keys := make([]string, 0, len(lm.Resources))
	cleanup := func(cleanupCtx context.Context) { r.storage.DeleteKeys(cleanupCtx, keys) }
	var total int64
	failed := 0
	for _, ref := range lm.Resources {
		media, key, size, resolveErr := r.resolveOne(ctx, creds, inst, lm.MessageID, ref, maxInboundMessageBytes-total)
		if resolveErr != nil {
			if IsRetryableResourceError(resolveErr) {
				return msg, cleanup, resolveErr
			}
			failed++
			continue
		}
		keys = append(keys, key)
		msg.MediaRefs = append(msg.MediaRefs, media)
		total += size
	}
	if failed > 0 {
		msg.Text = appendAttachmentWarning(msg.Text, failed)
	}
	return msg, cleanup, nil
}

func (r *FeishuMediaResolver) resolveOne(ctx context.Context, creds InstallationCredentials, inst engine.ResolvedInstallation, messageID string, ref MessageResourceRef, remaining int64) (channel.MediaRef, string, int64, error) {
	if remaining <= 0 {
		return channel.MediaRef{}, "", 0, &messageResourceError{category: "message attachments exceed 100 MB"}
	}
	resourceType := MessageResourceFile
	if ref.Type == "image" {
		resourceType = MessageResourceImage
	}
	downloaded, err := r.downloader.DownloadMessageResource(ctx, creds, messageID, ref.Key, resourceType)
	if err != nil {
		return channel.MediaRef{}, "", 0, err
	}
	defer downloaded.Body.Close()
	if downloaded.ContentLength > remaining || downloaded.ContentLength > maxMessageResourceBytes {
		return channel.MediaRef{}, "", 0, &messageResourceError{category: "message attachments exceed 100 MB"}
	}

	tmp, err := os.CreateTemp("", "multica-lark-resource-*")
	if err != nil {
		return channel.MediaRef{}, "", 0, &messageResourceError{retryable: true, category: "temporary storage unavailable", cause: err}
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	limit := remaining
	if limit > maxMessageResourceBytes {
		limit = maxMessageResourceBytes
	}
	size, err := io.Copy(tmp, io.LimitReader(downloaded.Body, limit+1))
	if err != nil {
		_ = tmp.Close()
		return channel.MediaRef{}, "", 0, &messageResourceError{retryable: true, category: "resource stream interrupted", cause: err}
	}
	if size > limit {
		_ = tmp.Close()
		return channel.MediaRef{}, "", 0, &messageResourceError{category: "message attachments exceed 100 MB"}
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		_ = tmp.Close()
		return channel.MediaRef{}, "", 0, &messageResourceError{retryable: true, category: "temporary resource unavailable", cause: err}
	}
	header := make([]byte, 512)
	n, readErr := io.ReadFull(tmp, header)
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
		_ = tmp.Close()
		return channel.MediaRef{}, "", 0, &messageResourceError{retryable: true, category: "resource inspection failed", cause: readErr}
	}
	detected := http.DetectContentType(header[:n])
	if !inboundResourceMIMEMatches(ref.Type, detected) {
		_ = tmp.Close()
		return channel.MediaRef{}, "", 0, &messageResourceError{category: "resource MIME does not match message type"}
	}
	contentType := chooseInboundContentType(detected, downloaded.ContentType, ref.Type)
	filename := sanitizeInboundFilename(firstNonEmpty(ref.Filename, downloaded.Filename), ref.Type, contentType)
	ext := safeObjectExtension(filename, contentType)
	objectID, err := uuid.NewV7()
	if err != nil {
		_ = tmp.Close()
		return channel.MediaRef{}, "", 0, &messageResourceError{retryable: true, category: "object identifier unavailable", cause: err}
	}
	key := fmt.Sprintf("workspaces/%s/channel-inbound/%s%s", uuidString(inst.WorkspaceID), objectID.String(), ext)
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		_ = tmp.Close()
		return channel.MediaRef{}, "", 0, &messageResourceError{retryable: true, category: "temporary resource unavailable", cause: err}
	}
	url, err := r.storage.UploadFromReader(ctx, key, tmp, size, contentType, filename)
	closeErr := tmp.Close()
	if err != nil {
		return channel.MediaRef{}, "", 0, &messageResourceError{retryable: true, category: "object storage unavailable", cause: err}
	}
	if closeErr != nil {
		r.storage.Delete(ctx, key)
		return channel.MediaRef{}, "", 0, &messageResourceError{retryable: true, category: "temporary resource close failed", cause: closeErr}
	}
	return channel.MediaRef{
		Type:       channelTypeForResource(ref.Type),
		StorageKey: key,
		URL:        url,
		Filename:   filename,
		MimeType:   contentType,
		SizeBytes:  size,
	}, key, size, nil
}

func inboundResourceMIMEMatches(resourceType, detected string) bool {
	if detected == "" || detected == "application/octet-stream" {
		return resourceType != "image"
	}
	switch resourceType {
	case "image":
		return strings.HasPrefix(detected, "image/")
	case "audio":
		return strings.HasPrefix(detected, "audio/")
	case "video":
		return strings.HasPrefix(detected, "video/")
	default:
		return true
	}
}

func appendAttachmentWarning(body string, failed int) string {
	warning := fmt.Sprintf("[Attachment notice: %d attachment could not be downloaded.]", failed)
	if failed != 1 {
		warning = fmt.Sprintf("[Attachment notice: %d attachments could not be downloaded.]", failed)
	}
	if strings.TrimSpace(body) == "" {
		return warning
	}
	return strings.TrimRight(body, "\n") + "\n" + warning
}

func chooseInboundContentType(detected, declared, resourceType string) string {
	declared, _, _ = mime.ParseMediaType(declared)
	if detected != "" && detected != "application/octet-stream" {
		return detected
	}
	if declared != "" {
		switch resourceType {
		case "audio":
			if strings.HasPrefix(declared, "audio/") {
				return declared
			}
		case "video":
			if strings.HasPrefix(declared, "video/") {
				return declared
			}
		case "file":
			return declared
		}
	}
	return "application/octet-stream"
}

func channelTypeForResource(resourceType string) channel.MsgType {
	switch resourceType {
	case "image":
		return channel.MsgTypeImage
	case "audio":
		return channel.MsgTypeAudio
	case "video":
		return channel.MsgTypeVideo
	default:
		return channel.MsgTypeFile
	}
}

func sanitizeInboundFilename(name, resourceType, contentType string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == 0 {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		name = resourceType + "-attachment" + safeObjectExtension("", contentType)
	}
	for len(name) > maxInboundFilenameBytes {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

func safeObjectExtension(filename, contentType string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if len(ext) > 1 && len(ext) <= 12 {
		valid := true
		for _, r := range ext[1:] {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				valid = false
				break
			}
		}
		if valid {
			return ext
		}
	}
	if extensions, err := mime.ExtensionsByType(contentType); err == nil && len(extensions) > 0 && len(extensions[0]) <= 12 {
		return extensions[0]
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var _ engine.MediaResolver = (*FeishuMediaResolver)(nil)
