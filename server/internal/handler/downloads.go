package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/go-chi/chi/v5"
)

// Desktop release artifact proxy. The upstream bucket is private — no
// `public-read` ACL — so every fetch goes through authenticated S3 SDK
// calls. Aliyun OSS speaks S3-compat over its `oss-cn-<region>.aliyuncs.com`
// endpoint, which is the same shape `server/internal/storage/s3.go` already
// uses for attachment storage; we share the SDK but use independent
// configuration (separate bucket / prefix / optional credentials).
//
//   GET /api/downloads             →  s3://<bucket>/<prefix>/version.json
//   GET /api/downloads/<filename>  →  s3://<bucket>/<prefix>/<filename>
//
// Manifest cache is last-known-good: if OSS hiccups, we keep serving the
// previous manifest rather than 502ing every polling client. Binary
// downloads are streamed unbuffered — installer files are 100MB+ and
// must not transit through `io.ReadAll`.
//
// Why proxy through the Go server instead of pre-signed URLs to OSS:
//   - One host (multica.lilithgames.com) keeps everything same-origin
//     with the rest of the API. No CORS preflight, no extra DNS hop on
//     hourly polls.
//   - The manifest cache absorbs the polling fan-out — N installed
//     desktops × 1 poll/hour collapses to at most `60s / TTL × replicas`
//     OSS GETs per hour.
//   - Auth / IP-filtering / observability hooks all attach here cleanly.

// s3GetObjectAPI matches the minimum surface of *s3.Client we need;
// tests substitute a stub to assert behaviour without an OSS round-trip.
type s3GetObjectAPI interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type DownloadsCache struct {
	client s3GetObjectAPI
	bucket string
	prefix string
	ttl    time.Duration
	logger *slog.Logger

	mu      sync.RWMutex
	body    []byte
	fetched time.Time
}

// DownloadsCacheConfig captures the knobs read from env. The endpoint
// + region + credentials are all optional — when empty they fall back
// to the AWS SDK default chain (so a deployment that already sets
// `AWS_*` envs for attachment storage gets the same credentials here
// for free; just set the bucket).
type DownloadsCacheConfig struct {
	// Endpoint is the S3-compatible base URL. For Aliyun OSS this is
	// e.g. `https://oss-cn-shanghai.aliyuncs.com`; leave empty for
	// real AWS S3.
	Endpoint string
	// Region per the SDK's region resolver. For Aliyun OSS, the region
	// matches the endpoint (`oss-cn-shanghai`); the SDK requires *some*
	// value here so we default to `oss-cn-shanghai` when empty.
	Region string
	// Bucket is required.
	Bucket string
	// Prefix is the directory within the bucket where the manifest +
	// binaries live. Trimmed of slashes. Default `downloads`.
	Prefix string
	// AccessKey / SecretKey are optional. Empty → default credential chain.
	AccessKey string
	SecretKey string
	// TTL is the manifest cache lifetime; default DefaultDownloadsTTL.
	TTL    time.Duration
	Logger *slog.Logger
}

// MaxManifestBytes guards the manifest read. A real manifest is well
// under 4 KB; cap at 256 KB so a misconfigured bucket pointing at the
// wrong object cannot OOM the server.
const MaxManifestBytes = 256 * 1024

// DefaultDownloadsTTL is the cache lifetime for the manifest. Tuned
// so a fresh publish is visible globally within ~1 minute even in the
// worst case where every replica's cache just refreshed.
const DefaultDownloadsTTL = 60 * time.Second

func NewDownloadsCache(cfg DownloadsCacheConfig) (*DownloadsCache, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("downloads: bucket not configured")
	}

	region := cfg.Region
	if region == "" {
		// Aliyun-OSS-friendly default. Real AWS users should set this
		// explicitly — there's no sane default that covers both clouds.
		region = "oss-cn-shanghai"
	}

	opts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}
	if isAliyunOSSEndpoint(cfg.Endpoint) {
		// Aliyun OSS's S3-compat layer rejects requests that carry the
		// AWS-default Content-MD5/CRC32 checksum headers (they're
		// computed differently). Match the workaround already in use
		// by server/internal/storage/s3.go.
		opts = append(opts,
			config.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
			config.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
		)
	}
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("downloads: load aws config: %w", err)
	}

	s3Opts := []func(*s3.Options){}
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			// Aliyun OSS rejects path-style (it requires virtual-hosted
			// `<bucket>.<endpoint>` addressing). Real S3 endpoints
			// usually accept both; default to virtual-hosted there too
			// for parity with the storage layer.
			o.UsePathStyle = !isAliyunOSSEndpoint(cfg.Endpoint)
		})
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	prefix := strings.Trim(cfg.Prefix, "/")
	if prefix == "" {
		prefix = "downloads"
	}

	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = DefaultDownloadsTTL
	}

	return &DownloadsCache{
		client: s3.NewFromConfig(awsCfg, s3Opts...),
		bucket: cfg.Bucket,
		prefix: prefix,
		ttl:    ttl,
		logger: logger,
	}, nil
}

// Bucket / Prefix accessors are exposed for log lines and tests.
func (c *DownloadsCache) Bucket() string { return c.bucket }
func (c *DownloadsCache) Prefix() string { return c.prefix }

func (c *DownloadsCache) manifestKey() string {
	return c.prefix + "/version.json"
}

func (c *DownloadsCache) binaryKey(filename string) string {
	return c.prefix + "/" + filename
}

// Get returns the cached manifest bytes, refreshing from upstream when
// the cached value is older than the TTL. Safe for concurrent use.
func (c *DownloadsCache) Get(ctx context.Context) ([]byte, error) {
	c.mu.RLock()
	cached := c.body
	fresh := time.Since(c.fetched) < c.ttl
	c.mu.RUnlock()

	if cached != nil && fresh {
		return cached, nil
	}
	return c.refresh(ctx)
}

func (c *DownloadsCache) refresh(ctx context.Context) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring the write lock — concurrent waiters
	// blocked here all share the single refresh that already finished.
	if c.body != nil && time.Since(c.fetched) < c.ttl {
		return c.body, nil
	}

	body, err := c.fetchManifest(ctx)
	if err != nil {
		c.logger.WarnContext(ctx, "downloads: manifest fetch failed",
			"err", err, "bucket", c.bucket, "key", c.manifestKey(),
			"have_cached", c.body != nil)
		if c.body != nil {
			// Last-known-good — but leave `fetched` alone so the next
			// caller (after this in-flight unlocks) will re-attempt
			// upstream rather than serving stale forever.
			return c.body, nil
		}
		return nil, err
	}

	c.body = body
	c.fetched = time.Now()
	return body, nil
}

func (c *DownloadsCache) fetchManifest(ctx context.Context) ([]byte, error) {
	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.manifestKey()),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	// LimitReader at cap+1 so we can detect overflow (the over-cap byte
	// shows up in the read count) without leaking bytes past the cap.
	body, err := io.ReadAll(io.LimitReader(out.Body, MaxManifestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > MaxManifestBytes {
		return nil, fmt.Errorf("downloads: manifest exceeds %d bytes", MaxManifestBytes)
	}
	// Sanity-check parseability. We don't enforce a schema — the
	// manifest is a contract between desktop client and the OSS object
	// the operator publishes; this server is a caching proxy. Catching
	// non-JSON early prevents shipping a partial upload or an error
	// page from OSS to clients.
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("downloads manifest is not valid JSON: %w", err)
	}
	return body, nil
}

// GetBinary opens an authenticated GET on the binary object. Caller is
// responsible for closing the returned Body.
func (c *DownloadsCache) GetBinary(ctx context.Context, filename string) (*s3.GetObjectOutput, error) {
	return c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.binaryKey(filename)),
	})
}

// isAliyunOSSEndpoint mirrors the helper in server/internal/storage/s3.go.
// Kept inline to avoid widening that package's public surface.
func isAliyunOSSEndpoint(endpointURL string) bool {
	endpoint := strings.ToLower(endpointURL)
	return strings.Contains(endpoint, ".aliyuncs.com") && strings.Contains(endpoint, "oss-")
}

// GetDownloads serves the desktop release manifest. Mounted on the
// public (unauthenticated) route group — same trust profile as the
// /download page and `install.sh`.
func (h *Handler) GetDownloads(w http.ResponseWriter, r *http.Request) {
	if h.Downloads == nil {
		writeError(w, http.StatusServiceUnavailable, "downloads endpoint not configured")
		return
	}
	body, err := h.Downloads.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "downloads upstream unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Edge / browser cache lifetime. Independent of (and shorter than)
	// the server-side TTL so a publish propagates fast even when an
	// intermediate cache exists.
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// IsSafeDownloadFilename guards the binary proxy route param against
// path traversal and other shenanigans. The route param is interpolated
// into an OSS object key, so a filename of `../version.json` would
// resolve to a different object. Accept only ASCII filename characters,
// no separators, no leading dot.
func IsSafeDownloadFilename(name string) bool {
	if name == "" || len(name) > 255 {
		return false
	}
	if name[0] == '.' {
		return false
	}
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	// Reject control chars + non-ASCII. Real installer filenames produced
	// by `apps/desktop/scripts/package.mjs` are bare ASCII; if that ever
	// changes the regex here is the single point to extend.
	for _, r := range name {
		if r < 0x20 || r > 0x7E {
			return false
		}
	}
	return true
}

// GetDownloadFile streams a single binary from OSS through to the
// client. Mounted under the same public route as GetDownloads, on the
// `/{filename}` sub-path.
//
// Unbuffered streaming is load-bearing here: a dmg can be 200 MB, and
// holding even one of those in memory per concurrent download would
// trash the heap fast.
func (h *Handler) GetDownloadFile(w http.ResponseWriter, r *http.Request) {
	if h.Downloads == nil {
		writeError(w, http.StatusServiceUnavailable, "downloads endpoint not configured")
		return
	}
	filename := chi.URLParam(r, "filename")
	if !IsSafeDownloadFilename(filename) {
		writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}

	out, err := h.Downloads.GetBinary(r.Context(), filename)
	if err != nil {
		// S3 SDK returns a typed error for "missing object"; map it to
		// HTTP 404 so the client can distinguish "you asked for a file
		// that doesn't exist yet" from "the bucket is broken".
		var nsk *s3types.NoSuchKey
		if errors.As(err, &nsk) {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		h.Downloads.logger.WarnContext(r.Context(), "downloads: binary fetch failed",
			"err", err, "bucket", h.Downloads.bucket, "filename", filename)
		writeError(w, http.StatusBadGateway, "downloads upstream unavailable")
		return
	}
	defer out.Body.Close()

	// Pass through metadata that affects how the client saves the file
	// (filename, size, type, integrity). The S3 SDK exposes these
	// typed pointers; nil means "upstream didn't set this header".
	if out.ContentType != nil {
		w.Header().Set("Content-Type", *out.ContentType)
	}
	if out.ContentLength != nil && *out.ContentLength > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", *out.ContentLength))
	}
	if out.ContentDisposition != nil && *out.ContentDisposition != "" {
		w.Header().Set("Content-Disposition", *out.ContentDisposition)
	} else {
		// Default to the request filename so the browser saves with a
		// useful name even when the OSS object was uploaded without an
		// explicit Content-Disposition.
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	}
	if out.ETag != nil {
		w.Header().Set("ETag", *out.ETag)
	}
	// Versioned binary filenames are immutable by contract — `vX.Y.Z`
	// uniquely identifies the artifact. One year is the standard
	// long-cache value; combined with the version-in-filename scheme,
	// clients re-launching the same release reuse the cached dmg
	// without re-fetching.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, out.Body); err != nil {
		// Connection cut mid-stream — log but can't recover the
		// response (headers + status already flushed).
		h.Downloads.logger.WarnContext(r.Context(), "downloads: stream interrupted",
			"err", err, "filename", filename)
	}
}
