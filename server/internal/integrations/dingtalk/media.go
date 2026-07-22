package dingtalk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the DingTalk engine.MediaIngester: it resolves each inbound
// downloadCode to its temporary URL, streams the bytes under hard limits,
// allowlists the sniffed image type, and stages the object in Multica storage.
// The attachment row is created later, inside the binder's append transaction.
// It runs only after the Router has cleared dedup/identity/membership, so an
// unauthorized or replayed message never costs a download.

// Tunables. DingTalk publishes no inbound size/count limits, so these are
// deliberately conservative; adjust here if product needs change.
const (
	maxImagesPerMessage   = 10
	maxInboundImageBytes  = 20 << 20
	imageFetchTimeout     = 30 * time.Second
	mediaIngestTimeout    = 90 * time.Second
	// mediaDiscardTimeout bounds best-effort cleanup after a failed ingest. The
	// cleanup is detached from the (possibly already-expired) ingest deadline,
	// but must stay bounded so a hung DeleteKeys cannot leak the goroutine.
	mediaDiscardTimeout = 30 * time.Second
	mediaFetchConcurrency = 4
	maxDownloadRedirects  = 3
)

// allowedImageTypes maps sniffed content types to storage extensions. SVG and
// anything non-raster is rejected: stored markup served through the CDN would
// be a stored-XSS vector. The extension is ours to pick — DingTalk's download
// carries no filename (the documented default extension is a literal ".file").
var allowedImageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
	"image/bmp":  ".bmp",
}

type mediaIngester struct {
	client  *Client
	decrypt Decrypter
	store   storage.Storage
	logger  *slog.Logger
	fetch   *http.Client
}

var _ engine.MediaIngester = (*mediaIngester)(nil)

// NewMediaIngester builds the DingTalk media ingester. The fetch client caps
// redirects and pins the scheme to http/https so the temporary download URL
// (the only non-fixed egress target) cannot be bounced anywhere exotic.
func NewMediaIngester(client *Client, decrypt Decrypter, store storage.Storage, logger *slog.Logger) *mediaIngester {
	if logger == nil {
		logger = slog.Default()
	}
	return &mediaIngester{
		client:  client,
		decrypt: decrypt,
		store:   store,
		logger:  logger,
		fetch: &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxDownloadRedirects {
					return errors.New("too many redirects")
				}
				if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
					return fmt.Errorf("disallowed redirect scheme %q", req.URL.Scheme)
				}
				return nil
			},
		},
	}
}

// Ingest fetches every pending image and stages it in object storage.
// All-or-nothing: on any failure it deletes the objects it already uploaded
// (best-effort — see Discard's caveat on backends without delete support) and
// returns the error, so the pipeline never appends a turn with a partial image
// set.
func (m *mediaIngester) Ingest(ctx context.Context, p engine.IngestParams) ([]engine.StagedMedia, error) {
	if len(p.Media) == 0 {
		return nil, nil
	}
	if len(p.Media) > maxImagesPerMessage {
		return nil, fmt.Errorf("dingtalk media: %d images exceed the per-message limit of %d", len(p.Media), maxImagesPerMessage)
	}
	row, ok := p.Installation.Platform.(db.ChannelInstallation)
	if !ok {
		return nil, errors.New("dingtalk media: installation platform row unavailable")
	}
	creds, err := decodeCredentials(row.Config, m.decrypt)
	if err != nil {
		return nil, fmt.Errorf("dingtalk media: decode credentials: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, mediaIngestTimeout)
	defer cancel()

	staged := make([]engine.StagedMedia, len(p.Media))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(mediaFetchConcurrency)
	for i, item := range p.Media {
		g.Go(func() error {
			sm, err := m.fetchOne(gctx, creds, p.WorkspaceID, item, i)
			if err != nil {
				return fmt.Errorf("dingtalk media: image %d: %w", i+1, err)
			}
			staged[i] = sm
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		// Cleanup must survive the (possibly expired) ingest deadline, but stay
		// bounded so a hung DeleteKeys cannot block this goroutine forever.
		dctx, dcancel := context.WithTimeout(context.WithoutCancel(ctx), mediaDiscardTimeout)
		m.Discard(dctx, staged)
		dcancel()
		return nil, err
	}
	return staged, nil
}

// Discard best-effort deletes staged objects. Used by Ingest's own failure
// path and by the Router when a later pipeline step fails after Ingest.
//
// Caveat: this is only as capable as the backend's DeleteKeys. Some storage
// backends do not implement deletion, in which case a discarded object stays
// as an unreferenced blob — an accepted leak (an orphaned object with no
// attachment row, not a correctness bug), while backends that support it
// delete for real.
func (m *mediaIngester) Discard(ctx context.Context, staged []engine.StagedMedia) {
	keys := make([]string, 0, len(staged))
	for _, sm := range staged {
		if sm.StorageKey != "" {
			keys = append(keys, sm.StorageKey)
		}
	}
	if len(keys) == 0 {
		return
	}
	m.store.DeleteKeys(ctx, keys)
}

func (m *mediaIngester) fetchOne(ctx context.Context, creds credentials, workspaceID pgtype.UUID, item channel.PendingMedia, idx int) (engine.StagedMedia, error) {
	start := time.Now()
	data, contentType, err := m.fetchByCode(ctx, creds, item.Ref)
	if err != nil && item.Alt != "" && item.Alt != item.Ref {
		// Both codes on a picture item are documented as downloadable; if the
		// primary fails at EITHER resolve OR download (an expired signed link, a
		// transient 5xx), try the secondary before giving up — otherwise a
		// single transient hiccup drops the whole all-or-nothing message.
		data, contentType, err = m.fetchByCode(ctx, creds, item.Alt)
	}
	if err != nil {
		return engine.StagedMedia{}, err
	}
	ext, ok := allowedImageTypes[contentType]
	if !ok {
		return engine.StagedMedia{}, fmt.Errorf("disallowed content type %q", contentType)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return engine.StagedMedia{}, fmt.Errorf("mint attachment id: %w", err)
	}
	key := storage.WorkspaceObjectKey(util.UUIDToString(workspaceID), id.String()+ext)
	filename := fmt.Sprintf("image-%d%s", idx+1, ext)
	link, err := m.store.Upload(ctx, key, data, contentType, filename)
	if err != nil {
		return engine.StagedMedia{}, fmt.Errorf("stage to storage: %w", err)
	}
	m.logger.DebugContext(ctx, "dingtalk media: staged image",
		"index", idx+1, "bytes", len(data), "content_type", contentType,
		"elapsed_ms", time.Since(start).Milliseconds())
	return engine.StagedMedia{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		StorageKey:  key,
		URL:         link,
		Filename:    filename,
		ContentType: contentType,
		SizeBytes:   int64(len(data)),
	}, nil
}

// fetchByCode resolves one download code to its temporary URL and streams the
// bytes. Split out so fetchOne can retry the whole resolve+download with the
// secondary code when the primary fails at either step.
func (m *mediaIngester) fetchByCode(ctx context.Context, creds credentials, code string) ([]byte, string, error) {
	dlURL, err := m.client.messageFileDownloadURL(ctx, creds.AppKey, creds.AppSecret, creds.RobotCode, code)
	if err != nil {
		return nil, "", fmt.Errorf("resolve download url: %w", err)
	}
	return m.fetchBytes(ctx, dlURL)
}

// fetchBytes GETs the temporary download URL under the per-image timeout and
// size cap, and returns the bytes plus the sniffed content type. The URL is
// never logged — it is a signed, short-lived credential.
func (m *mediaIngester) fetchBytes(ctx context.Context, url string) ([]byte, string, error) {
	fctx, cancel := context.WithTimeout(ctx, imageFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(fctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build download request: %w", err)
	}
	resp, err := m.fetch.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download image: %w", err)
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
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		contentType = strings.TrimSpace(contentType[:i])
	}
	return data, contentType, nil
}
