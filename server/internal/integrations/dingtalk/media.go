package dingtalk

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
	"golang.org/x/sync/errgroup"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DingTalk publishes no inbound image limits. Keep the adapter's memory and
// remote-I/O budget deliberately below the shared Router's 45-second media
// deadline: at most two 10 MiB downloads are buffered concurrently.
const (
	maxImagesPerMessage   = 4
	maxInboundImageBytes  = 10 << 20
	imageFetchTimeout     = 30 * time.Second
	mediaFetchConcurrency = 2
	maxDownloadRedirects  = 3
)

var allowedImageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
	"image/bmp":  ".bmp",
}

type mediaResolver struct {
	client  *Client
	decrypt Decrypter
	store   storage.Storage
	ledger  engine.MediaIntentLedger
	logger  *slog.Logger
	fetch   *http.Client
}

var _ engine.MediaResolver = (*mediaResolver)(nil)

func NewMediaResolver(client *Client, decrypt Decrypter, store storage.Storage, ledger engine.MediaIntentLedger, logger *slog.Logger) engine.MediaResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &mediaResolver{
		client:  client,
		decrypt: decrypt,
		store:   store,
		ledger:  ledger,
		logger:  logger,
		fetch:   newMediaHTTPClient(),
	}
}

// DingTalk's authenticated messageFiles/download API can return a public HTTP
// URL as well as HTTPS. The adapter must honor that provider-issued URL without
// rewriting its scheme, but the URL remains untrusted egress input: resolve the
// host ourselves, reject every non-public answer, and dial the validated IP
// directly so DNS rebinding cannot redirect the connection into the local
// network. Proxy use is disabled because a proxy would resolve the target again
// and bypass this guarantee.
func newMediaHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = (&publicDownloadDialer{
		resolver: net.DefaultResolver,
		dialer:   &net.Dialer{Timeout: imageFetchTimeout},
	}).DialContext
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxDownloadRedirects {
				return errors.New("too many redirects")
			}
			// net/http derives Referer from the previous URL. The query string
			// is a short-lived bearer credential, so it must never cross into a
			// redirect request, including an otherwise-allowed same-origin hop.
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
	// IPv6 transition mechanisms can encapsulate an otherwise-blocked IPv4
	// destination in an address that netip classifies as global unicast. The
	// download client does not need these legacy/local transition ranges, so
	// fail closed instead of attempting to decode every deployment-specific
	// mapping and risking a route into loopback or RFC1918 space.
	netip.MustParsePrefix("::/96"),          // deprecated IPv4-compatible
	netip.MustParsePrefix("64:ff9b:1::/48"), // local-use NAT64
	netip.MustParsePrefix("100::/64"),       // discard-only
	netip.MustParsePrefix("2001::/32"),      // Teredo
	netip.MustParsePrefix("2001:2::/48"),    // benchmarking
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2001:10::/28"), // deprecated ORCHID
	netip.MustParsePrefix("2001:20::/28"), // ORCHIDv2
	netip.MustParsePrefix("2002::/16"),    // 6to4
	netip.MustParsePrefix("3fff::/20"),    // documentation
}

var wellKnownNAT64Prefix = netip.MustParsePrefix("64:ff9b::/96")

func isPublicDownloadAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	if wellKnownNAT64Prefix.Contains(addr) {
		// The standard NAT64 prefix carries an IPv4 address in its low 32
		// bits. Permit a synthesized public target, but apply the complete IPv4
		// deny policy to prevent an attacker-controlled AAAA record from
		// smuggling loopback or RFC1918 through an apparently global IPv6 value.
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

// HasMedia is a pure decode of the already-received callback metadata. Remote
// work stays behind ResolveMedia, after dedup, identity and membership checks.
func (m *mediaResolver) HasMedia(msg channel.InboundMessage) bool {
	raw, err := decodeDingTalkRaw(msg)
	return err == nil && len(raw.Media) > 0
}

func (m *mediaResolver) ResolveMedia(ctx context.Context, inst engine.ResolvedInstallation, _ engine.ResolvedIdentity, _ pgtype.UUID, chatMessageID pgtype.UUID, msg channel.InboundMessage) channel.InboundMessage {
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) == 0 {
		return msg
	}
	if len(raw.Media) > maxImagesPerMessage {
		m.logWarn(msg, fmt.Errorf("%d images exceed the limit of %d", len(raw.Media), maxImagesPerMessage))
		return msg
	}
	if m.client == nil || m.store == nil || m.ledger == nil {
		m.logWarn(msg, errors.New("media dependency missing"))
		return msg
	}
	row, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		m.logWarn(msg, errors.New("installation platform row unavailable"))
		return msg
	}
	creds, err := decodeCredentials(row.Config, m.decrypt)
	if err != nil {
		m.logWarn(msg, fmt.Errorf("decode credentials: %w", err))
		return msg
	}

	refs := make([]channel.MediaRef, len(raw.Media))
	valid := make([]bool, len(raw.Media))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(mediaFetchConcurrency)
	for i, resource := range raw.Media {
		g.Go(func() error {
			key := dingtalkMediaObjectKey(inst, chatMessageID, resource, i)
			link := m.store.ObjectURL(key)
			owned, err := m.ledger.RecordPendingMediaObject(gctx, engine.RecordPendingMediaObjectParams{
				StorageKey:     key,
				WorkspaceID:    inst.WorkspaceID,
				ChatMessageID:  chatMessageID,
				StorageURL:     link,
				InstallationID: inst.ID,
			})
			if err != nil {
				m.logWarn(msg, fmt.Errorf("record media intent: %w", err))
				return nil
			}
			if !owned {
				m.logWarn(msg, errors.New("media key owned by reconciler"))
				return nil
			}
			data, contentType, err := m.fetchResource(gctx, creds, resource)
			if err != nil {
				m.logWarn(msg, err)
				return nil
			}
			ext := allowedImageTypes[contentType]
			filename := fmt.Sprintf("dingtalk-image-%d%s", i+1, ext)
			if _, err := m.store.Upload(gctx, key, data, contentType, filename); err != nil {
				m.logWarn(msg, fmt.Errorf("upload image: %w", err))
				return nil
			}
			refs[i] = channel.MediaRef{
				Type:       channel.MsgTypeImage,
				StorageKey: key,
				StorageURL: link,
				Filename:   filename,
				MimeType:   contentType,
				SizeBytes:  int64(len(data)),
			}
			valid[i] = true
			return nil
		})
	}
	_ = g.Wait()
	for i, ref := range refs {
		if valid[i] {
			msg.MediaRefs = append(msg.MediaRefs, ref)
		}
	}
	return msg
}

func dingtalkMediaObjectKey(inst engine.ResolvedInstallation, chatMessageID pgtype.UUID, resource dingtalkMediaResource, index int) string {
	sum := sha256.Sum256([]byte(util.UUIDToString(chatMessageID) + "\x00" + resource.Ref + "\x00" + fmt.Sprint(index)))
	return path.Join("workspaces", util.UUIDToString(inst.WorkspaceID), "dingtalk", util.UUIDToString(inst.ID), hex.EncodeToString(sum[:]))
}

func (m *mediaResolver) fetchResource(ctx context.Context, creds credentials, resource dingtalkMediaResource) ([]byte, string, error) {
	data, contentType, primaryErr := m.fetchByCode(ctx, creds, resource.Ref)
	if primaryErr == nil || resource.Alt == "" || resource.Alt == resource.Ref {
		return data, contentType, primaryErr
	}
	data, contentType, fallbackErr := m.fetchByCode(ctx, creds, resource.Alt)
	if fallbackErr == nil {
		return data, contentType, nil
	}
	return nil, "", errors.Join(
		fmt.Errorf("primary media reference: %w", primaryErr),
		fmt.Errorf("fallback media reference: %w", fallbackErr),
	)
}

func (m *mediaResolver) fetchByCode(ctx context.Context, creds credentials, code string) ([]byte, string, error) {
	downloadURL, err := m.client.messageFileDownloadURL(ctx, creds.AppKey, creds.AppSecret, creds.RobotCode, code)
	if err != nil {
		return nil, "", fmt.Errorf("resolve download url: %w", err)
	}
	return m.fetchBytes(ctx, downloadURL)
}

func (m *mediaResolver) fetchBytes(ctx context.Context, rawURL string) ([]byte, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", errors.New("invalid image download URL")
	}
	if err := validateDownloadURL(parsed); err != nil {
		return nil, "", err
	}
	fetchCtx, cancel := context.WithTimeout(ctx, imageFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", errors.New("build image download request failed")
	}
	resp, err := m.fetch.Do(req)
	if err != nil {
		// net/http's *url.Error includes the full request URL. DingTalk download
		// URLs are short-lived bearer credentials, so unwrap it before the caller
		// logs the error and never persist the signed query string.
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
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxInboundImageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read image: %w", err)
	}
	if len(data) > maxInboundImageBytes {
		return nil, "", fmt.Errorf("image exceeds the %d MB limit", maxInboundImageBytes>>20)
	}
	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	contentType := http.DetectContentType(sniff)
	if semi := strings.IndexByte(contentType, ';'); semi >= 0 {
		contentType = strings.TrimSpace(contentType[:semi])
	}
	if _, ok := allowedImageTypes[contentType]; !ok {
		return nil, "", fmt.Errorf("disallowed content type %q", contentType)
	}
	return data, contentType, nil
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

func (m *mediaResolver) logWarn(msg channel.InboundMessage, err error) {
	m.logger.Warn("dingtalk media resolve skipped", "message_id", msg.MessageID, "error", err)
}
