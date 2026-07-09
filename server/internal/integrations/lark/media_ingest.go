package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

type mediaStorage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
}

type mediaStreamStorage interface {
	UploadStream(ctx context.Context, key string, data io.Reader, contentType string, filename string) (string, error)
}

type messageResourceStreamer interface {
	DownloadMessageResourceStream(ctx context.Context, creds InstallationCredentials, p DownloadResourceParams) (DownloadedResourceStream, error)
}

type feishuMediaResolver struct {
	api     APIClient
	creds   CredentialsResolver
	storage mediaStorage
	logger  *slog.Logger
}

func NewFeishuMediaResolver(api APIClient, creds CredentialsResolver, storage mediaStorage, logger *slog.Logger) engine.MediaResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &feishuMediaResolver{api: api, creds: creds, storage: storage, logger: logger}
}

func (r *feishuMediaResolver) ResolveMedia(ctx context.Context, inst engine.ResolvedInstallation, _ engine.ResolvedIdentity, _ pgtype.UUID, msg channel.InboundMessage) channel.InboundMessage {
	if len(msg.MediaRefs) > 0 {
		return msg
	}
	lm, err := larkMsgFromRaw(msg)
	if err != nil {
		r.logMediaWarn("lark media ingest skipped: raw decode failed", InboundMessage{MessageID: msg.MessageID}, err)
		return msg
	}
	resources := mediaResourcesFromMessage(lm)
	if len(resources) == 0 {
		return msg
	}
	if r.api == nil || r.creds == nil || r.storage == nil {
		r.logMediaWarn("lark media ingest skipped: missing dependency", lm, nil)
		return msg
	}
	larkInst, ok := inst.Platform.(Installation)
	if !ok {
		r.logMediaWarn("lark media ingest skipped: installation payload unavailable", lm, nil)
		return msg
	}
	creds, err := installationCredentialsFor(larkInst, r.creds)
	if err != nil {
		r.logMediaWarn("lark media ingest skipped: credentials unavailable", lm, err)
		return msg
	}
	for _, res := range resources {
		got, err := r.downloadResource(ctx, creds, DownloadResourceParams{
			MessageID: res.messageID,
			FileKey:   res.key,
			Type:      res.fetchType,
		})
		if err != nil {
			r.logMediaWarn("lark media download failed", lm, err)
			continue
		}
		contentType := got.ContentType
		if contentType == "" {
			contentType = res.mimeType
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		filename := mediaFilename(lm, res, got, contentType)
		id, err := uuid.NewV7()
		if err != nil {
			r.logMediaWarn("lark media attachment id failed", lm, err)
			continue
		}
		key := path.Join("workspaces", uuidString(inst.WorkspaceID), "lark", id.String()+path.Ext(filename))
		link, uploadedBytes, err := r.uploadResource(ctx, key, got.Body, contentType, filename)
		if err != nil {
			r.logMediaWarn("lark media upload failed", lm, err)
			continue
		}
		sizeBytes := got.SizeBytes
		if sizeBytes == 0 {
			sizeBytes = uploadedBytes
		}
		msg.MediaRefs = append(msg.MediaRefs, channel.MediaRef{
			Type:       res.kind,
			StorageKey: key,
			StorageURL: link,
			Filename:   filename,
			MimeType:   contentType,
			SizeBytes:  sizeBytes,
		})
	}
	return msg
}

func (r *feishuMediaResolver) downloadResource(ctx context.Context, creds InstallationCredentials, p DownloadResourceParams) (DownloadedResourceStream, error) {
	if streamer, ok := r.api.(messageResourceStreamer); ok {
		return streamer.DownloadMessageResourceStream(ctx, creds, p)
	}
	got, err := r.api.DownloadMessageResource(ctx, creds, p)
	if err != nil {
		return DownloadedResourceStream{}, err
	}
	return DownloadedResourceStream{
		Body:        io.NopCloser(bytes.NewReader(got.Data)),
		ContentType: got.ContentType,
		Filename:    got.Filename,
		SizeBytes:   got.SizeBytes,
	}, nil
}

func (r *feishuMediaResolver) uploadResource(ctx context.Context, key string, body io.ReadCloser, contentType string, filename string) (string, int64, error) {
	defer body.Close()
	counter := &countingReader{r: body}
	if streamStorage, ok := r.storage.(mediaStreamStorage); ok {
		link, err := streamStorage.UploadStream(ctx, key, counter, contentType, filename)
		return link, counter.n, err
	}
	data, err := io.ReadAll(counter)
	if err != nil {
		return "", counter.n, err
	}
	link, err := r.storage.Upload(ctx, key, data, contentType, filename)
	return link, int64(len(data)), err
}

type countingReader struct {
	r io.Reader
	n int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

func (r *feishuMediaResolver) logMediaWarn(msg string, lm InboundMessage, err error) {
	if r.logger == nil {
		return
	}
	args := []any{"message_id", lm.MessageID, "message_type", lm.MessageType}
	if err != nil {
		args = append(args, "err", err)
	}
	r.logger.Warn(msg, args...)
}

type larkMediaResource struct {
	key       string
	kind      channel.MsgType
	fetchType string
	filename  string
	mimeType  string
	sizeBytes int64
	messageID string
}

func mediaResourcesFromMessage(lm InboundMessage) []larkMediaResource {
	var payload struct {
		ImageKey    string `json:"image_key"`
		FileKey     string `json:"file_key"`
		FileName    string `json:"file_name"`
		Name        string `json:"name"`
		MimeType    string `json:"mime_type"`
		ContentType string `json:"content_type"`
		Size        int64  `json:"size"`
		SizeBytes   int64  `json:"size_bytes"`
	}
	if lm.Content == "" || json.Unmarshal([]byte(lm.Content), &payload) != nil {
		return nil
	}
	filename := firstNonEmpty(payload.FileName, payload.Name)
	mimeType := firstNonEmpty(payload.MimeType, payload.ContentType)
	sizeBytes := payload.SizeBytes
	if sizeBytes == 0 {
		sizeBytes = payload.Size
	}
	switch lm.MessageType {
	case "image":
		if payload.ImageKey == "" {
			return nil
		}
		return []larkMediaResource{{
			key:       payload.ImageKey,
			kind:      channel.MsgTypeImage,
			fetchType: "image",
			filename:  filename,
			mimeType:  mimeType,
			sizeBytes: sizeBytes,
			messageID: lm.MessageID,
		}}
	case "post":
		return mediaResourcesFromPost(lm)
	case "media", "video":
		if payload.FileKey == "" {
			return nil
		}
		return []larkMediaResource{{
			key:       payload.FileKey,
			kind:      channel.MsgTypeVideo,
			fetchType: "file",
			filename:  filename,
			mimeType:  mimeType,
			sizeBytes: sizeBytes,
			messageID: lm.MessageID,
		}}
	default:
		return nil
	}
}

func mediaResourcesFromPost(lm InboundMessage) []larkMediaResource {
	if lm.Content == "" {
		return nil
	}
	var doc larkPostContent
	if err := json.Unmarshal([]byte(lm.Content), &doc); err != nil {
		return nil
	}
	var out []larkMediaResource
	for _, para := range doc.Content {
		for _, span := range para {
			switch span.Tag {
			case "img":
				if span.ImageKey == "" {
					continue
				}
				out = append(out, larkMediaResource{
					key:       span.ImageKey,
					kind:      channel.MsgTypeImage,
					fetchType: "image",
					filename:  firstNonEmpty(span.FileName, span.Name),
					mimeType:  span.MimeType,
					messageID: lm.MessageID,
				})
			case "media":
				if span.FileKey == "" {
					continue
				}
				out = append(out, larkMediaResource{
					key:       span.FileKey,
					kind:      channel.MsgTypeVideo,
					fetchType: "file",
					filename:  firstNonEmpty(span.FileName, span.Name),
					mimeType:  span.MimeType,
					messageID: lm.MessageID,
				})
			}
		}
	}
	return out
}

func mediaFilename(lm InboundMessage, res larkMediaResource, got DownloadedResourceStream, contentType string) string {
	for _, candidate := range []string{got.Filename, res.filename} {
		if name := cleanFilename(candidate); name != "" {
			return name
		}
	}
	prefix := "feishu-file"
	switch res.kind {
	case channel.MsgTypeImage:
		prefix = "feishu-image"
	case channel.MsgTypeVideo:
		prefix = "feishu-video"
	}
	return prefix + "-" + safePathSegment(lm.MessageID) + mediaExtension(contentType)
}

func cleanFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" {
		return ""
	}
	return name
}

func mediaExtension(contentType string) string {
	if semi := strings.IndexByte(contentType, ';'); semi >= 0 {
		contentType = strings.TrimSpace(contentType[:semi])
	}
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	}
	if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func safePathSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
