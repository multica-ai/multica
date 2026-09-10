package lark

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"path"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

type mediaStorage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	// ObjectURL is the URL a successful upload of key returns — a pure
	// function of configuration, so the intent ledger can persist it BEFORE
	// the PUT.
	ObjectURL(key string) string
}

type mediaStreamStorage interface {
	UploadStream(ctx context.Context, key string, data io.Reader, sizeBytes int64, contentType string, filename string) (string, error)
}

type messageResourceStreamer interface {
	DownloadMessageResourceStream(ctx context.Context, creds InstallationCredentials, p DownloadResourceParams) (DownloadedResourceStream, error)
}

type feishuMediaResolver struct {
	api     APIClient
	creds   CredentialsResolver
	storage mediaStorage
	ledger  engine.MediaIntentLedger
	logger  *slog.Logger
}

func NewFeishuMediaResolver(api APIClient, creds CredentialsResolver, storage mediaStorage, ledger engine.MediaIntentLedger, logger *slog.Logger) engine.MediaResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &feishuMediaResolver{api: api, creds: creds, storage: storage, ledger: ledger, logger: logger}
}

// HasMedia reports whether the message carries downloadable Feishu resources
// (standalone image/video/file/audio or post-embedded img/media spans). Pure
// in-memory decode of the already-received payload — it runs on the connector
// ACK path.
func (r *feishuMediaResolver) HasMedia(msg channel.InboundMessage) bool {
	lm, err := larkMsgFromRaw(msg)
	if err != nil {
		return false
	}
	return len(mediaResourcesFromMessage(lm)) > 0
}

func (r *feishuMediaResolver) ResolveMedia(ctx context.Context, inst engine.ResolvedInstallation, _ engine.ResolvedIdentity, _ pgtype.UUID, chatMessageID pgtype.UUID, msg channel.InboundMessage) channel.InboundMessage {
	lm, err := larkMsgFromRaw(msg)
	if err != nil {
		r.logMediaWarn("lark media ingest skipped: raw decode failed", InboundMessage{MessageID: msg.MessageID}, err)
		return msg
	}
	resources := mediaResourcesFromMessage(lm)
	if len(resources) == 0 {
		return msg
	}
	if r.api == nil || r.creds == nil || r.storage == nil || r.ledger == nil {
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
	for resIndex, res := range resources {
		key := mediaObjectKey(inst, chatMessageID, res)
		link := r.storage.ObjectURL(key)
		// Persist the upload intent BEFORE any write can happen. Every
		// failure from here on — download error, upload error (even one the
		// store may still be processing), resolve deadline, crash — simply
		// leaves this row for the reconciler; nothing is ever deleted inline.
		ok, err := r.ledger.RecordPendingMediaObject(ctx, engine.RecordPendingMediaObjectParams{
			StorageKey:     key,
			WorkspaceID:    inst.WorkspaceID,
			ChatMessageID:  chatMessageID,
			StorageURL:     link,
			InstallationID: inst.ID,
		})
		if err != nil {
			// No durable intent, no upload — fail-safe direction.
			r.logMediaWarn("lark media ingest skipped: intent record failed", lm, err)
			continue
		}
		if !ok {
			// The reconciler owns this key ('deleting'); never resurrect it.
			r.logMediaWarn("lark media ingest skipped: key owned by reconciler", lm, nil)
			continue
		}
		got, err := r.downloadResource(ctx, creds, DownloadResourceParams{
			MessageID: res.messageID,
			FileKey:   res.key,
			Type:      res.fetchType,
		})
		if err != nil {
			r.logMediaWarn("lark media download failed", lm, err)
			continue
		}
		contentType := mediaContentType(res, got)
		filename := mediaFilename(lm, res, got, contentType, resIndex)
		uploadedBytes, err := r.uploadResource(ctx, key, got.Body, got.SizeBytes, contentType, filename)
		if err != nil {
			// The store may still be processing the PUT — deleting here
			// could reorder with it. The intent row (written above) covers
			// the object either way; the reconciler settles it.
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

// mediaObjectKey derives the object key from the CHAT message the object will
// be attached to, not from the platform message alone: a platform message can
// be ingested twice (the inbound dedup row is reclaimable once its claim is 60s
// stale, and vacuumed after 24h), and a shared key would run the second ingest
// into the first one's ledger row. That row may be a tombstone — the intent
// upsert refuses anything that has left 'pending', so the second ingest would
// silently drop its media for as long as the re-delete schedule runs. A key
// per chat message keeps the ingests independent; nothing leaks, because each
// one's objects are covered by its own ledger row.
func mediaObjectKey(inst engine.ResolvedInstallation, chatMessageID pgtype.UUID, res larkMediaResource) string {
	sum := sha256.Sum256([]byte(uuidString(chatMessageID) + "\x00" + res.messageID + "\x00" + res.fetchType + "\x00" + res.key))
	return path.Join(
		"workspaces",
		uuidString(inst.WorkspaceID),
		"lark",
		uuidString(inst.ID),
		hex.EncodeToString(sum[:]),
	)
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

func (r *feishuMediaResolver) uploadResource(ctx context.Context, key string, body io.ReadCloser, sizeBytes int64, contentType string, filename string) (int64, error) {
	defer body.Close()
	counter := &countingReader{r: body}
	if streamStorage, ok := r.storage.(mediaStreamStorage); ok && sizeBytes > 0 {
		_, err := streamStorage.UploadStream(ctx, key, counter, sizeBytes, contentType, filename)
		return counter.n, err
	}
	// Unknown-length HTTP bodies cannot be sent through S3 PutObject as a
	// non-seekable stream. The transport already enforces the 100 MiB resource
	// cap, so buffer this uncommon fallback and use the seekable Upload path.
	data, err := io.ReadAll(counter)
	if err != nil {
		return counter.n, err
	}
	_, err = r.storage.Upload(ctx, key, data, contentType, filename)
	return int64(len(data)), err
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
	case "file", "audio":
		if payload.FileKey == "" {
			return nil
		}
		kind := channel.MsgTypeFile
		if lm.MessageType == "audio" {
			kind = channel.MsgTypeAudio
		}
		return []larkMediaResource{{
			key:       payload.FileKey,
			kind:      kind,
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
	// A post may reference the same image_key/file_key in more than one span.
	// The object key is derived from (message, type, key), so duplicates would
	// upload to the SAME key twice: a later failed attempt could destroy the
	// object an earlier success already produced (dangling attachment), and a
	// later success would yield two attachment rows for one object. Collapse
	// them here, before any upload.
	seen := make(map[string]bool)
	for _, para := range doc.Content {
		for _, span := range para {
			switch span.Tag {
			case "img":
				if span.ImageKey == "" || seen["image\x00"+span.ImageKey] {
					continue
				}
				seen["image\x00"+span.ImageKey] = true
				out = append(out, larkMediaResource{
					key:       span.ImageKey,
					kind:      channel.MsgTypeImage,
					fetchType: "image",
					filename:  firstNonEmpty(span.FileName, span.Name),
					mimeType:  span.MimeType,
					messageID: lm.MessageID,
				})
			case "media":
				if span.FileKey == "" || seen["file\x00"+span.FileKey] {
					continue
				}
				seen["file\x00"+span.FileKey] = true
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

// mediaFilename picks a name for the stored object. index disambiguates the
// generated form: every resource on one message shares a MessageID, so three
// photos in one send used to produce three objects all named
// "feishu-image-<msg>.jpg".
func mediaFilename(lm InboundMessage, res larkMediaResource, got DownloadedResourceStream, contentType string, index int) string {
	for _, candidate := range []string{got.Filename, res.filename} {
		if name := cleanFilename(candidate); name != "" {
			return ensureAudioFilenameExtension(name, res.kind, contentType)
		}
	}
	prefix := "feishu-file"
	switch res.kind {
	case channel.MsgTypeImage:
		prefix = "feishu-image"
	case channel.MsgTypeVideo:
		prefix = "feishu-video"
	case channel.MsgTypeAudio:
		prefix = "feishu-audio"
	}
	name := prefix + "-" + safePathSegment(lm.MessageID)
	if index > 0 {
		name += "-" + strconv.Itoa(index+1)
	}
	return name + mediaExtension(contentType)
}

func mediaContentType(res larkMediaResource, got DownloadedResourceStream) string {
	contentType := strings.TrimSpace(got.ContentType)
	if res.kind == channel.MsgTypeAudio && isGenericBinaryContentType(contentType) {
		if hinted := strings.TrimSpace(res.mimeType); !isGenericBinaryContentType(hinted) {
			return hinted
		}
		// Feishu audio messages are Opus. Its resource endpoint can return an
		// extensionless Content-Disposition filename together with the generic
		// audio/octet-stream type, so preserve the protocol-level format here.
		return "audio/opus"
	}
	if contentType == "" {
		contentType = strings.TrimSpace(res.mimeType)
	}
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}

func isGenericBinaryContentType(contentType string) bool {
	if semi := strings.IndexByte(contentType, ';'); semi >= 0 {
		contentType = contentType[:semi]
	}
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "", "application/octet-stream", "audio/octet-stream":
		return true
	default:
		return false
	}
}

func ensureAudioFilenameExtension(name string, kind channel.MsgType, contentType string) string {
	if kind != channel.MsgTypeAudio || path.Ext(name) != "" {
		return name
	}
	return name + mediaExtension(contentType)
}

func cleanFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	// path.Base resolves a path, but it hands back ".." and "..." unchanged.
	// A name made only of dots is not a filename; letting one through means
	// the object's display name is ".." wherever it is later rendered or
	// re-saved. Fall through to the generated name instead.
	if strings.Trim(name, ".") == "" || name == "/" {
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
	case "audio/opus":
		return ".opus"
	case "audio/ogg":
		return ".ogg"
	case "audio/amr":
		return ".amr"
	case "audio/mpeg":
		return ".mp3"
	case "application/pdf":
		// mime.ExtensionsByType returns ExtensionsByType(".pdf") on most
		// systems, but it reads /etc/mime.types — which a slim container
		// image does not ship — so the common document type is pinned here
		// rather than left to the host.
		return ".pdf"
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
