package weixin

import (
	"context"
	"crypto/aes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

// Bound memory use independently of provider-declared lengths. This is a
// Multica transfer limit, not a claim about WeChat's account-specific quota.
const maxMediaBytes = 20 << 20
const defaultCDN = "https://novac2c.cdn.weixin.qq.com/c2c"

type objectStore interface {
	Upload(context.Context, string, []byte, string, string) (string, error)
	ObjectURL(string) string
	KeyFromURL(string) string
	GetReader(context.Context, string) (io.ReadCloser, error)
}

type inboundMedia struct {
	ref                    mediaRef
	key, name, placeholder string
	kind                   channel.MsgType
	index                  int
}

func messageMedia(msg message) []inboundMedia {
	var out []inboundMedia
	counts := map[string]int{}
	for _, item := range msg.Items {
		var m inboundMedia
		switch {
		case item.Type == 2 && item.Image != nil:
			m = inboundMedia{ref: item.Image.Media, key: item.Image.AESKey, kind: channel.MsgTypeImage, placeholder: "[Image]"}
		case item.Type == 3 && item.Voice != nil && item.Voice.Text == "":
			m = inboundMedia{ref: item.Voice.Media, kind: channel.MsgTypeAudio, placeholder: "[Audio]", name: "voice.silk"}
		case item.Type == 4 && item.File != nil:
			m = inboundMedia{ref: item.File.Media, name: item.File.Name, kind: channel.MsgTypeFile, placeholder: "[File]"}
		case item.Type == 5 && item.Video != nil:
			m = inboundMedia{ref: item.Video.Media, kind: channel.MsgTypeVideo, placeholder: "[Video]", name: "video.mp4"}
		default:
			continue
		}
		m.index = counts[m.placeholder]
		counts[m.placeholder]++
		out = append(out, m)
	}
	return out
}

func mediaKey(ref mediaRef, imageKey string) ([]byte, error) {
	if imageKey != "" {
		key, err := hex.DecodeString(imageKey)
		if err == nil && len(key) == aes.BlockSize {
			return key, nil
		}
		return nil, ErrProtocol
	}
	key, err := base64.StdEncoding.DecodeString(ref.AESKey)
	if err != nil {
		return nil, ErrProtocol
	}
	if len(key) == 32 {
		key, err = hex.DecodeString(string(key))
	}
	if err != nil || len(key) != aes.BlockSize {
		return nil, ErrProtocol
	}
	return key, nil
}

// iLink media uses AES-128-ECB with PKCS#7 padding. Authenticate neither
// arbitrary hosts nor redirected requests with the bot credential.
func decryptMedia(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil || len(key) != aes.BlockSize || len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, ErrProtocol
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += aes.BlockSize {
		block.Decrypt(out[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
	}
	n := int(out[len(out)-1])
	if n < 1 || n > aes.BlockSize {
		return nil, ErrProtocol
	}
	for _, b := range out[len(out)-n:] {
		if int(b) != n {
			return nil, ErrProtocol
		}
	}
	return out[:len(out)-n], nil
}

func trustedMediaURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && u.Fragment == "" && strings.HasSuffix(strings.ToLower(u.Hostname()), ".weixin.qq.com")
}

func downloadMedia(ctx context.Context, client *http.Client, m inboundMedia) ([]byte, error) {
	key, err := mediaKey(m.ref, m.key)
	if err != nil {
		return nil, err
	}
	target := m.ref.FullURL
	if target == "" && m.ref.Query != "" {
		target = defaultCDN + "/download?encrypted_query_param=" + url.QueryEscape(m.ref.Query)
	}
	if !trustedMediaURL(target) {
		return nil, ErrProtocol
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, ErrProtocol
	}
	if client == nil {
		client = http.DefaultClient
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := copy.Do(req)
	if err != nil {
		return nil, errors.New("weixin: media download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ProviderError{Status: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMediaBytes+aes.BlockSize+1))
	if err != nil || len(data) > maxMediaBytes+aes.BlockSize {
		return nil, errors.New("weixin: media transfer exceeds limit or failed")
	}
	plain, err := decryptMedia(data, key)
	if err != nil || len(plain) > maxMediaBytes {
		return nil, ErrProtocol
	}
	return plain, nil
}

type mediaResolver struct {
	store  objectStore
	ledger engine.MediaIntentLedger
	client *http.Client
}

func NewMediaResolver(store objectStore, ledger engine.MediaIntentLedger) engine.MediaResolver {
	return &mediaResolver{store: store, ledger: ledger}
}

func (r *mediaResolver) HasMedia(msg channel.InboundMessage) bool {
	raw, err := decodeWeixinRaw(msg)
	return err == nil && len(messageMedia(raw.Message)) > 0
}

func (r *mediaResolver) ResolveMedia(ctx context.Context, inst engine.ResolvedInstallation, _ engine.ResolvedIdentity, _ pgtype.UUID, chatMessageID pgtype.UUID, msg channel.InboundMessage) channel.InboundMessage {
	raw, err := decodeWeixinRaw(msg)
	if err != nil {
		return msg
	}
	for i, m := range messageMedia(raw.Message) {
		ref, err := r.ingest(ctx, inst, chatMessageID, i, m)
		if err != nil {
			// Be explicit in the persisted prompt: a placeholder is not a file
			// the model received. The original message stays in WeChat.
			msg.Text += "\n[WeChat attachment unavailable: file " + strconv.Itoa(i+1) + "; send it again or upload it in Multica.]"
			continue
		}
		msg.MediaRefs = append(msg.MediaRefs, ref)
	}
	return msg
}

func (r *mediaResolver) ingest(ctx context.Context, inst engine.ResolvedInstallation, messageID pgtype.UUID, index int, m inboundMedia) (channel.MediaRef, error) {
	if r.store == nil || r.ledger == nil {
		return channel.MediaRef{}, errors.New("weixin: media storage is not configured")
	}
	data, err := downloadMedia(ctx, r.client, m)
	if err != nil {
		return channel.MediaRef{}, err
	}
	contentType := http.DetectContentType(data)
	name := path.Base(strings.ReplaceAll(m.name, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		name = "wechat-" + string(m.kind)
		if extensions, _ := mime.ExtensionsByType(contentType); len(extensions) > 0 {
			name += extensions[0]
		}
	}
	sum := sha256.Sum256([]byte(util.UUIDToString(messageID) + ":" + strconv.Itoa(index)))
	key := path.Join("workspaces", util.UUIDToString(inst.WorkspaceID), "weixin", util.UUIDToString(inst.ID), hex.EncodeToString(sum[:]))
	link := r.store.ObjectURL(key)
	owned, err := r.ledger.RecordPendingMediaObject(ctx, engine.RecordPendingMediaObjectParams{StorageKey: key, WorkspaceID: inst.WorkspaceID, ChatMessageID: messageID, StorageURL: link, InstallationID: inst.ID})
	if err != nil || !owned {
		return channel.MediaRef{}, fmt.Errorf("weixin: media intent unavailable: %v", err)
	}
	if _, err := r.store.Upload(ctx, key, data, contentType, name); err != nil {
		return channel.MediaRef{}, errors.New("weixin: media storage failed")
	}
	return channel.MediaRef{Type: m.kind, StorageKey: key, StorageURL: link, Filename: name, MimeType: contentType, SizeBytes: int64(len(data)), InlinePlaceholder: m.placeholder, InlineIndex: m.index}, nil
}
