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

// ShareCRM inbound images arrive as short-lived signed HTTP(S) URLs. Keep the
// adapter's memory budget below the shared Router media deadline: at most eight
// 10 MiB downloads, fetched one at a time.
const (
	maxShareCRMImagesPerMessage = 8
	maxShareCRMImageBytes       = 10 << 20
	shareCRMImageFetchTimeout   = 15 * time.Second
	maxShareCRMImageRedirects   = 3
)

var allowedShareCRMImageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
	"image/bmp":  ".bmp",
}

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
		r.logWarn(msg, errors.New("media dependency missing"))
		return msg
	}
	if len(raw.Images) > maxShareCRMImagesPerMessage {
		r.logWarn(msg, fmt.Errorf("%d images exceed the limit of %d", len(raw.Images), maxShareCRMImagesPerMessage))
		return msg
	}
	for i, image := range raw.Images {
		ref, err := r.ingestOne(ctx, inst, chatMessageID, msg.MessageID, i, image)
		if err != nil {
			r.logWarn(msg, err)
			continue
		}
		msg.MediaRefs = append(msg.MediaRefs, ref)
	}
	return msg
}

func (r *sharecrmMediaResolver) ingestOne(ctx context.Context, inst engine.ResolvedInstallation, chatMessageID pgtype.UUID, messageID string, index int, image botImageRef) (channel.MediaRef, error) {
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
	body, contentType, err := r.fetchBytes(ctx, image.URL)
	if err != nil {
		return channel.MediaRef{}, err
	}
	ext := allowedShareCRMImageTypes[contentType]
	filename := strings.TrimSpace(image.Filename)
	if filename == "" {
		filename = fmt.Sprintf("sharecrm-image-%d%s", index+1, ext)
	}
	if _, err := r.storage.Upload(ctx, key, body, contentType, filename); err != nil {
		return channel.MediaRef{}, fmt.Errorf("upload image: %w", err)
	}
	return channel.MediaRef{
		Type:              channel.MsgTypeImage,
		StorageKey:        key,
		StorageURL:        link,
		Filename:          filename,
		MimeType:          contentType,
		SizeBytes:         int64(len(body)),
		InlinePlaceholder: sharecrmImagePlaceholder,
		InlineIndex:       index,
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

// Signed image URLs are untrusted egress input: resolve the host ourselves,
// reject every non-public answer, and dial the validated IP directly so DNS
// rebinding cannot redirect the connection into the local network. Proxy use
// is disabled because a proxy would resolve the target again and bypass this
// guarantee. The query string is a short-lived bearer credential, so Referer
// must never leak across redirects.
func newShareCRMMediaHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = (&publicDownloadDialer{
		resolver: net.DefaultResolver,
		dialer:   &net.Dialer{Timeout: shareCRMImageFetchTimeout},
	}).DialContext
	return &http.Client{
		Timeout:   shareCRMImageFetchTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxShareCRMImageRedirects {
				return errors.New("too many redirects")
			}
			req.Header.Del("Referer")
			if err := validateDownloadURL(req.URL); err != nil {
				return err
			}
			previous := via[len(via)-1].URL
			if strings.EqualFold(previous.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https") {
				return errors.New("disallowed HTTPS download redirect downgrade")
			}
			if strings.EqualFold(req.URL.Scheme, "http") && !sameDownloadOrigin(previous, req.URL) {
				return errors.New("disallowed cross-origin HTTP download redirect")
			}
			return nil
		},
	}
}

type publicDownloadDialer struct {
	resolver *net.Resolver
	dialer   *net.Dialer
}

func (d *publicDownloadDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("invalid download target address")
	}
	addrs, err := d.lookup(ctx, host)
	if err != nil || len(addrs) == 0 {
		return nil, errors.New("resolve download target failed")
	}
	for _, addr := range addrs {
		if !isPublicDownloadAddress(addr) {
			return nil, errors.New("blocked non-public download target")
		}
	}
	for _, addr := range addrs {
		conn, err := d.dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("connect to download target failed")
}

func (d *publicDownloadDialer) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr.Unmap()}, nil
	}
	addrs, err := d.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for i := range addrs {
		addrs[i] = addrs[i].Unmap()
	}
	return addrs, nil
}

var nonPublicDownloadPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var wellKnownNAT64Prefix = netip.MustParsePrefix("64:ff9b::/96")

func isPublicDownloadAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	if wellKnownNAT64Prefix.Contains(addr) {
		raw := addr.As16()
		return isPublicDownloadAddress(netip.AddrFrom4([4]byte(raw[12:16])))
	}
	for _, prefix := range nonPublicDownloadPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func sameDownloadOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func validateDownloadURL(parsed *url.URL) error {
	if parsed == nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("invalid image download URL shape")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("invalid image download URL scheme %q", parsed.Scheme)
	}
	return nil
}

func (r *sharecrmMediaResolver) fetchBytes(ctx context.Context, rawURL string) ([]byte, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, "", errors.New("invalid image download URL")
	}
	if err := validateDownloadURL(parsed); err != nil {
		return nil, "", err
	}
	fetchCtx, cancel := context.WithTimeout(ctx, shareCRMImageFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", errors.New("build image download request failed")
	}
	req.Header.Set("Accept", "image/*,*/*;q=0.8")
	resp, err := r.http.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return nil, "", fmt.Errorf("download image request failed: %w", urlErr.Err)
		}
		return nil, "", errors.New("download image request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("download image: http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxShareCRMImageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read image: %w", err)
	}
	if len(data) > maxShareCRMImageBytes {
		return nil, "", fmt.Errorf("image exceeds the %d MB limit", maxShareCRMImageBytes>>20)
	}
	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	contentType := http.DetectContentType(sniff)
	if semi := strings.IndexByte(contentType, ';'); semi >= 0 {
		contentType = strings.TrimSpace(contentType[:semi])
	}
	if _, ok := allowedShareCRMImageTypes[contentType]; !ok {
		return nil, "", fmt.Errorf("disallowed content type %q", contentType)
	}
	return data, contentType, nil
}

func (r *sharecrmMediaResolver) logWarn(msg channel.InboundMessage, err error) {
	r.logger.Warn("sharecrm media resolve skipped", "message_id", msg.MessageID, "error", err)
}
