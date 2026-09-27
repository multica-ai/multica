package telegram

// Inbound media: the engine.MediaResolver for Telegram. HasMedia is a pure
// decode of the raw envelope; ResolveMedia runs off the ACK path and carries
// the message's files — the sender's own and the one they quoted — from
// Telegram into object storage: getFile for the download path, the file host
// for the bytes, an intent-ledger row before the PUT so a crash anywhere
// leaves something the reconciler settles. Mirrors slack/media_ingest.go minus
// the redirect handling: Telegram's download host is fixed.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// objectStore is the slice of storage.Storage both media directions need:
// Upload + ObjectURL to ingest (ObjectURL is pure, so the intent row can carry
// the final URL before the PUT), KeyFromURL + GetReader to read an agent's
// attachment back out for delivery.
type objectStore interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	ObjectURL(key string) string
	KeyFromURL(rawURL string) string
	GetReader(ctx context.Context, key string) (io.ReadCloser, error)
}

type mediaResolver struct {
	decrypt Decrypter
	storage objectStore
	ledger  engine.MediaIntentLedger
	apiBase string
	client  *http.Client
	logger  *slog.Logger
}

var _ engine.MediaResolver = (*mediaResolver)(nil)

// NewMediaResolver builds the Telegram media resolver. apiBase and client
// override the Bot API host and HTTP client (tests); empty / nil use
// production. The Router's media deadline bounds each resolution; the client
// timeout only guards a single stalled transfer.
func NewMediaResolver(decrypt Decrypter, storage objectStore, ledger engine.MediaIntentLedger, apiBase string, client *http.Client, logger *slog.Logger) engine.MediaResolver {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &mediaResolver{decrypt: decrypt, storage: storage, ledger: ledger, apiBase: apiBase, client: client, logger: logger}
}

func (r *mediaResolver) HasMedia(msg channel.InboundMessage) bool {
	raw, err := decodeTelegramRaw(msg)
	return err == nil && len(raw.Media) > 0
}

func (r *mediaResolver) ResolveMedia(ctx context.Context, inst engine.ResolvedInstallation, _ engine.ResolvedIdentity, _ pgtype.UUID, chatMessageID pgtype.UUID, msg channel.InboundMessage) channel.InboundMessage {
	raw, err := decodeTelegramRaw(msg)
	if err != nil || len(raw.Media) == 0 {
		return msg
	}
	row, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		r.logWarn(msg, errors.New("installation platform row unavailable"))
		return msg
	}
	creds, err := decodeCredentials(row.Config, r.decrypt)
	if err != nil {
		r.logWarn(msg, fmt.Errorf("decode credentials: %w", err))
		return msg
	}
	api := newBotAPI(r.apiBase, creds.BotToken, r.client)
	// Every file is attempted; one that fails keeps its placeholder in the
	// body, and the sender hears about it once.
	failed := false
	for i, m := range raw.Media {
		ref, err := r.ingest(ctx, inst, chatMessageID, i, m, api)
		if err != nil {
			r.logWarn(msg, err)
			failed = true
			continue
		}
		msg.MediaRefs = append(msg.MediaRefs, ref)
	}
	if failed {
		r.notifyUnavailable(ctx, api, msg)
	}
	return msg
}

// notifyUnavailable tells the sender a file did not make it — their own or
// the one they quoted, over Telegram's 20 MB bot download limit or a failed
// fetch — so the placeholder the agent is left with is not mistaken for a
// file it saw. Best effort, on its own short budget: the failure may be the
// fetch context running out.
func (r *mediaResolver) notifyUnavailable(ctx context.Context, api *botAPI, msg channel.InboundMessage) {
	chatID, err := strconv.ParseInt(msg.Source.ChatID, 10, 64)
	if err != nil {
		return
	}
	var threadID int64
	if msg.Source.ThreadID != "" {
		threadID, _ = strconv.ParseInt(msg.Source.ThreadID, 10, 64)
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := api.SendMessage(ctx, sendMessageParams{
		ChatID:          chatID,
		Text:            msgMediaUnavailable,
		MessageThreadID: threadID,
		ReplyParameters: optionalReplyParameters(parseMessageRef(msg.MessageID)),
	}); err != nil {
		r.logger.Warn("telegram media: unavailable notice failed", "message_id", msg.MessageID, "error", err)
	}
}

// ingest carries one file from Telegram to object storage. The intent row goes
// first: from that point on every failure leaves a row the reconciler settles,
// and nothing here deletes anything.
func (r *mediaResolver) ingest(ctx context.Context, inst engine.ResolvedInstallation, chatMessageID pgtype.UUID, index int, m inboundMedia, api *botAPI) (channel.MediaRef, error) {
	if m.FileSize > maxBotDownloadBytes {
		return channel.MediaRef{}, fmt.Errorf("file size %d exceeds the %d MiB bot download limit", m.FileSize, maxBotDownloadBytes>>20)
	}
	file, err := api.GetFile(ctx, m.FileID)
	if err != nil {
		return channel.MediaRef{}, fmt.Errorf("getFile: %w", err)
	}
	if file.FilePath == "" {
		return channel.MediaRef{}, errors.New("getFile returned no file_path")
	}
	if file.FileSize > maxBotDownloadBytes {
		return channel.MediaRef{}, fmt.Errorf("file size %d exceeds the %d MiB bot download limit", file.FileSize, maxBotDownloadBytes>>20)
	}
	key := mediaObjectKey(inst, chatMessageID, index, m)
	link := r.storage.ObjectURL(key)
	owned, err := r.ledger.RecordPendingMediaObject(ctx, engine.RecordPendingMediaObjectParams{
		StorageKey:     key,
		WorkspaceID:    inst.WorkspaceID,
		ChatMessageID:  chatMessageID,
		StorageURL:     link,
		InstallationID: inst.ID,
	})
	if err != nil {
		return channel.MediaRef{}, fmt.Errorf("record media intent: %w", err)
	}
	if !owned {
		return channel.MediaRef{}, errors.New("media key owned by reconciler")
	}
	data, responseType, err := api.DownloadFile(ctx, file.FilePath, maxBotDownloadBytes)
	if err != nil {
		return channel.MediaRef{}, err
	}
	contentType := mediaContentType(m.MimeType, responseType, data)
	filename := mediaFilename(m, file.FilePath, contentType)
	if _, err := r.storage.Upload(ctx, key, data, contentType, filename); err != nil {
		return channel.MediaRef{}, fmt.Errorf("upload file: %w", err)
	}
	return channel.MediaRef{
		Type:              m.Kind,
		StorageKey:        key,
		StorageURL:        link,
		Filename:          filename,
		MimeType:          contentType,
		SizeBytes:         int64(len(data)),
		InlinePlaceholder: m.Placeholder,
		InlineIndex:       m.PlaceholderIndex,
	}, nil
}

// mediaObjectKey is keyed by the chat message the object binds to and the
// file's position in it, not the platform file alone: a reclaimed dedup claim
// can ingest one platform message twice, and a reply can quote the very file
// it sends, so a shared key would run the second ingest into the first one's
// (possibly tombstoned) ledger row.
func mediaObjectKey(inst engine.ResolvedInstallation, chatMessageID pgtype.UUID, index int, m inboundMedia) string {
	sum := sha256.Sum256([]byte(util.UUIDToString(chatMessageID) + "\x00" + strconv.Itoa(index) + "\x00" + firstNonEmpty(m.FileUniqueID, m.FileID)))
	return path.Join("workspaces", util.UUIDToString(inst.WorkspaceID), "telegram", util.UUIDToString(inst.ID), hex.EncodeToString(sum[:]))
}

// mediaContentType prefers what Telegram declared, then the download's own
// header, then a sniff of the bytes.
func mediaContentType(declared, responseType string, data []byte) string {
	if ct := strings.ToLower(strings.TrimSpace(declared)); ct != "" {
		return ct
	}
	if ct := strings.ToLower(strings.TrimSpace(responseType)); ct != "" && ct != "application/octet-stream" {
		return ct
	}
	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	ct := http.DetectContentType(sniff)
	if semi := strings.IndexByte(ct, ';'); semi >= 0 {
		ct = strings.TrimSpace(ct[:semi])
	}
	return ct
}

// mediaFilename is the stored display name: the sender's own file name when
// there is one, else Telegram's path segment (photos and voice notes have no
// name of their own), with an extension matching the content type.
func mediaFilename(m inboundMedia, filePath, contentType string) string {
	name := cleanFilename(m.FileName)
	if name == "" {
		name = cleanFilename(filePath)
	}
	if name == "" {
		name = "telegram-" + string(m.Kind)
	}
	if path.Ext(name) == "" {
		name += mediaExtension(contentType)
	}
	return name
}

func cleanFilename(name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if strings.Trim(name, ".") == "" || name == "/" {
		return ""
	}
	return name
}

// mediaExtension pins the familiar spellings (image/jpeg is ".jfif" first on
// some mime databases) and falls back to the system table.
func mediaExtension(contentType string) string {
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
	case "audio/ogg":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	}
	if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func (r *mediaResolver) logWarn(msg channel.InboundMessage, err error) {
	r.logger.Warn("telegram media resolve skipped", "message_id", msg.MessageID, "error", err)
}
