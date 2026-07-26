package lark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/storage"
)

const defaultInboundMediaTimeout = 2 * time.Second

// InboundMediaService downloads a user-sent Feishu resource and persists it
// through Multica's configured object storage. Database attachment rows are
// created later, inside the chat-message transaction, from the returned
// channel.MediaRef.
type InboundMediaService struct {
	downloader  MessageResourceDownloader
	credentials CredentialsResolver
	storage     storage.Storage
	timeout     time.Duration
	logger      *slog.Logger
}

type InboundMediaServiceConfig struct {
	Timeout time.Duration
	Logger  *slog.Logger
}

func NewInboundMediaService(downloader MessageResourceDownloader, credentials CredentialsResolver, objectStorage storage.Storage, cfg InboundMediaServiceConfig) (*InboundMediaService, error) {
	if downloader == nil {
		return nil, errors.New("lark inbound media: downloader is required")
	}
	if credentials == nil {
		return nil, errors.New("lark inbound media: credentials resolver is required")
	}
	if objectStorage == nil {
		return nil, errors.New("lark inbound media: storage is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultInboundMediaTimeout
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &InboundMediaService{
		downloader:  downloader,
		credentials: credentials,
		storage:     objectStorage,
		timeout:     cfg.Timeout,
		logger:      cfg.Logger,
	}, nil
}

// IngestImage ingests the single image carried by a top-level image message.
// It remains as the narrow helper used by callers and tests that already know
// the message kind.
func (s *InboundMediaService) IngestImage(ctx context.Context, inst Installation, workspaceID pgtype.UUID, messageID, rawContent string) (channel.MediaRef, error) {
	refs, err := s.IngestMessageImages(ctx, inst, workspaceID, messageID, "image", rawContent)
	if err != nil {
		return channel.MediaRef{}, err
	}
	if len(refs) != 1 {
		return channel.MediaRef{}, fmt.Errorf("lark inbound media: expected one image, got %d", len(refs))
	}
	return refs[0], nil
}

// IngestMessageImages resolves image keys from either a top-level image or
// image spans embedded in a rich-text post, downloads each resource, and
// uploads it under a deterministic object key. Redelivery overwrites the same
// objects rather than leaking a new orphan for every retry.
func (s *InboundMediaService) IngestMessageImages(ctx context.Context, inst Installation, workspaceID pgtype.UUID, messageID, messageType, rawContent string) ([]channel.MediaRef, error) {
	if !workspaceID.Valid {
		return nil, errors.New("lark inbound media: workspace_id is required")
	}
	if messageID == "" {
		return nil, errors.New("lark inbound media: message_id is required")
	}

	imageKeys, err := inboundImageKeys(messageType, rawContent)
	if err != nil {
		return nil, err
	}
	if len(imageKeys) == 0 {
		return nil, nil
	}

	secret, err := s.credentials.DecryptAppSecret(inst)
	if err != nil {
		return nil, fmt.Errorf("lark inbound media: decrypt app_secret: %w", err)
	}
	creds := InstallationCredentials{
		AppID:     inst.AppID,
		AppSecret: secret,
		Region:    RegionOrDefault(inst.Region),
	}
	if inst.TenantKey.Valid {
		creds.TenantKey = inst.TenantKey.String
	}

	refs := make([]channel.MediaRef, 0, len(imageKeys))
	for i, imageKey := range imageKeys {
		ref, err := s.ingestImageKey(ctx, inst, workspaceID, creds, messageID, imageKey, i, len(imageKeys))
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func inboundImageKeys(messageType, rawContent string) ([]string, error) {
	switch messageType {
	case "image":
		var content struct {
			ImageKey string `json:"image_key"`
		}
		if err := json.Unmarshal([]byte(rawContent), &content); err != nil {
			return nil, fmt.Errorf("lark inbound media: decode image content: %w", err)
		}
		if content.ImageKey == "" {
			return nil, errors.New("lark inbound media: image_key is required")
		}
		return []string{content.ImageKey}, nil
	case "post":
		var doc larkPostContent
		if err := json.Unmarshal([]byte(rawContent), &doc); err != nil {
			return nil, fmt.Errorf("lark inbound media: decode post content: %w", err)
		}
		seen := make(map[string]struct{})
		keys := make([]string, 0)
		for _, paragraph := range doc.Content {
			for _, span := range paragraph {
				if span.Tag != "img" || span.ImageKey == "" {
					continue
				}
				if _, ok := seen[span.ImageKey]; ok {
					continue
				}
				seen[span.ImageKey] = struct{}{}
				keys = append(keys, span.ImageKey)
			}
		}
		return keys, nil
	default:
		return nil, nil
	}
}

func (s *InboundMediaService) ingestImageKey(ctx context.Context, inst Installation, workspaceID pgtype.UUID, creds InstallationCredentials, messageID, imageKey string, index, total int) (channel.MediaRef, error) {
	mediaCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	resource, err := s.downloader.DownloadMessageResource(mediaCtx, creds, messageID, imageKey, "image")
	if err != nil {
		return channel.MediaRef{}, fmt.Errorf("lark inbound media: download image: %w", err)
	}
	if len(resource.Data) == 0 {
		return channel.MediaRef{}, errors.New("lark inbound media: downloaded image is empty")
	}

	contentType, err := normalizeInboundImageContentType(resource.ContentType, resource.Data)
	if err != nil {
		return channel.MediaRef{}, err
	}
	ext := inboundImageExtension(contentType)
	filename := sanitizeInboundFilename(resource.Filename)
	if filename == "" {
		filename = "feishu-image"
		if total > 1 {
			filename += fmt.Sprintf("-%d", index+1)
		}
		filename += ext
	} else if path.Ext(filename) == "" && ext != "" {
		filename += ext
	}

	sum := sha256.Sum256([]byte(uuidString(inst.ID) + "\x00" + messageID + "\x00" + imageKey))
	key := "workspaces/" + uuidString(workspaceID) + "/channels/feishu/" +
		hex.EncodeToString(sum[:16]) + ext
	objectURL, err := s.storage.Upload(mediaCtx, key, resource.Data, contentType, filename)
	if err != nil {
		return channel.MediaRef{}, fmt.Errorf("lark inbound media: store image: %w", err)
	}
	s.logger.Debug("lark inbound media: image persisted",
		"message_id", messageID,
		"workspace_id", uuidString(workspaceID),
		"size_bytes", len(resource.Data),
		"content_type", contentType,
	)
	return channel.MediaRef{
		Type:       channel.MsgTypeImage,
		StorageKey: key,
		URL:        objectURL,
		Filename:   filename,
		MimeType:   contentType,
		SizeBytes:  int64(len(resource.Data)),
	}, nil
}

func normalizeInboundImageContentType(headerValue string, data []byte) (string, error) {
	contentType := strings.TrimSpace(headerValue)
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = mediaType
	}
	contentType = strings.ToLower(contentType)

	detected := strings.ToLower(http.DetectContentType(data))
	if mediaType, _, err := mime.ParseMediaType(detected); err == nil {
		detected = mediaType
	}
	if isAllowedInboundImageContentType(detected) {
		return detected, nil
	}
	if detected != "application/octet-stream" {
		return "", fmt.Errorf("lark inbound media: downloaded resource is not an image: %s", detected)
	}
	if isAllowedInboundImageContentType(contentType) {
		return contentType, nil
	}
	return "", fmt.Errorf("lark inbound media: unsupported image content type %q", contentType)
}

func isAllowedInboundImageContentType(contentType string) bool {
	switch contentType {
	case "image/jpeg",
		"image/png",
		"image/gif",
		"image/webp",
		"image/bmp",
		"image/tiff",
		"image/x-icon",
		"image/vnd.microsoft.icon":
		return true
	default:
		return false
	}
}

func inboundImageExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/tiff":
		return ".tiff"
	case "image/bmp":
		return ".bmp"
	case "image/x-icon", "image/vnd.microsoft.icon":
		return ".ico"
	}
	exts, err := mime.ExtensionsByType(contentType)
	if err != nil || len(exts) == 0 {
		return ""
	}
	ext := strings.ToLower(exts[0])
	if !strings.HasPrefix(ext, ".") || len(ext) > 10 {
		return ""
	}
	return ext
}

func sanitizeInboundFilename(filename string) string {
	filename = strings.TrimSpace(strings.ReplaceAll(filename, "\\", "/"))
	filename = path.Base(filename)
	if filename == "." || filename == "/" || filename == "" {
		return ""
	}
	return filename
}
