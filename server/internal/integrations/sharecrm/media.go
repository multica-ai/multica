package sharecrm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	maxShareCRMImagesPerMessage = 8
	maxShareCRMImageBytes       = 10 << 20
	shareCRMImageFetchTimeout   = 15 * time.Second
	maxShareCRMImageRedirects   = 3
)

type mediaStorage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	ObjectURL(key string) string
}

type sharecrmMediaResolver struct {
	storage mediaStorage
	ledger  engine.MediaIntentLedger
	http    *http.Client
	logger  *slog.Logger
}

var _ engine.MediaResolver = (*sharecrmMediaResolver)(nil)

func NewMediaResolver(storage mediaStorage, ledger engine.MediaIntentLedger, logger *slog.Logger) engine.MediaResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &sharecrmMediaResolver{
		storage: storage,
		ledger:  ledger,
		http:    newShareCRMMediaHTTPClient(),
		logger:  logger,
	}
}

func (r *sharecrmMediaResolver) HasMedia(msg channel.InboundMessage) bool {
	raw, err := decodeShareCRMRaw(msg)
	return err == nil && len(raw.Images) > 0
}

func (r *sharecrmMediaResolver) ResolveMedia(ctx context.Context, inst engine.ResolvedInstallation, _ engine.ResolvedIdentity, _ pgtype.UUID, chatMessageID pgtype.UUID, msg channel.InboundMessage) channel.InboundMessage {
	raw, err := decodeShareCRMRaw(msg)
	if err != nil || len(raw.Images) == 0 {
		return msg
	}
	if r.storage == nil || r.ledger == nil {
		r.logger.Warn("sharecrm media ingest skipped: no storage configured", "message_id", msg.MessageID)
		return msg
	}
	if len(raw.Images) > maxShareCRMImagesPerMessage {
		r.logger.Warn("sharecrm media ingest skipped: too many images", "message_id", msg.MessageID, "count", len(raw.Images))
		return msg
	}
	for i, image := range raw.Images {
		ref, err := r.ingestOne(ctx, inst, chatMessageID, msg.MessageID, i, image)
		if err != nil {
			r.logger.Warn("sharecrm media ingest failed", "message_id", msg.MessageID, "index", i, "err", err)
			continue
		}
		msg.MediaRefs = append(msg.MediaRefs, ref)
	}
	return msg
}

func (r *sharecrmMediaResolver) ingestOne(ctx context.Context, inst engine.ResolvedInstallation, chatMessageID pgtype.UUID, messageID string, index int, image botImageRef) (channel.MediaRef, error) {
	filename := strings.TrimSpace(image.Filename)
	if filename == "" {
		filename = fmt.Sprintf("image-%d.png", index+1)
	}
	key := sharecrmMediaObjectKey(inst, chatMessageID, messageID, index)
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
	body, contentType, err := downloadShareCRMImage(ctx, r.http, image.URL)
	if err != nil {
		return channel.MediaRef{}, err
	}
	if _, err := r.storage.Upload(ctx, key, body, contentType, filename); err != nil {
		return channel.MediaRef{}, fmt.Errorf("upload image: %w", err)
	}
	return channel.MediaRef{
		Type:       channel.MsgTypeImage,
		StorageKey: key,
		StorageURL: link,
		Filename:   filename,
		MimeType:   contentType,
		SizeBytes:  int64(len(body)),
	}, nil
}

func sharecrmMediaObjectKey(inst engine.ResolvedInstallation, chatMessageID pgtype.UUID, messageID string, index int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", util.UUIDToString(chatMessageID), messageID, index)))
	return path.Join(
		"workspaces",
		util.UUIDToString(inst.WorkspaceID),
		"sharecrm",
		util.UUIDToString(inst.ID),
		hex.EncodeToString(sum[:]),
	)
}

func newShareCRMMediaHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			var last error
			for _, ip := range ips {
				addr, ok := netip.AddrFromSlice(ip.IP)
				if !ok || !isPublicShareCRMMediaAddr(addr) {
					last = errors.New("sharecrm: media host resolves to a non-public address")
					continue
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			if last == nil {
				last = errors.New("sharecrm: media host resolves to a non-public address")
			}
			return nil, last
		},
	}
	return &http.Client{
		Timeout: shareCRMImageFetchTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxShareCRMImageRedirects {
				return errors.New("sharecrm: too many media redirects")
			}
			if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
				return errors.New("sharecrm: media redirect scheme not allowed")
			}
			return nil
		},
	}
}

func isPublicShareCRMMediaAddr(addr netip.Addr) bool {
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsMulticast() {
		return false
	}
	if addr.Is4() {
		v4 := addr.As4()
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return false
		}
		if v4[0] == 169 && v4[1] == 254 {
			return false
		}
	}
	return true
}

func downloadShareCRMImage(ctx context.Context, hc *http.Client, rawURL string) ([]byte, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return nil, "", errors.New("sharecrm: invalid image url")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, "", errors.New("sharecrm: image url scheme not allowed")
	}
	if parsed.User != nil {
		return nil, "", errors.New("sharecrm: image url must not include userinfo")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "image/*,*/*;q=0.8")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("sharecrm: image download failed (%d)", resp.StatusCode)
	}
	if resp.ContentLength > maxShareCRMImageBytes {
		return nil, "", errors.New("sharecrm: image exceeds size limit")
	}
	limited := io.LimitReader(resp.Body, maxShareCRMImageBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > maxShareCRMImageBytes {
		return nil, "", errors.New("sharecrm: image exceeds size limit")
	}
	contentType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if !strings.HasPrefix(contentType, "image/") {
		contentType = "image/png"
	}
	return body, contentType, nil
}
