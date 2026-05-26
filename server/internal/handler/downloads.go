package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

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
//   GET /api/downloads/<filename>  →  s3://<bucket>/<prefix>/<filename>
//
// The desktop client (electron-updater, generic provider) polls
// `latest-mac.yml` / `latest.yml` / `latest-arm64.yml` / `latest-linux.yml`
// through this route to discover new versions, then downloads the
// installer the YML references. Binary downloads are streamed unbuffered —
// installer files are 100MB+ and must not transit through `io.ReadAll`.
//
// Why proxy through the Go server instead of pre-signed URLs to OSS:
//   - One host (multica.lilithgames.com) keeps everything same-origin
//     with the rest of the API. No CORS preflight, no extra DNS hop on
//     hourly polls.
//   - Auth / IP-filtering / observability hooks all attach here cleanly.

// s3GetObjectAPI matches the minimum surface of *s3.Client we need;
// tests substitute a stub to assert behaviour without an OSS round-trip.
type s3GetObjectAPI interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type DownloadsProxy struct {
	client s3GetObjectAPI
	bucket string
	prefix string
	logger *slog.Logger
}

// DownloadsProxyConfig captures the knobs read from env. The endpoint
// + region + credentials are all optional — when empty they fall back
// to the AWS SDK default chain (so a deployment that already sets
// `AWS_*` envs for attachment storage gets the same credentials here
// for free; just set the bucket).
type DownloadsProxyConfig struct {
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
	// Prefix is the directory within the bucket where the binaries
	// live. Trimmed of slashes. Default `downloads`.
	Prefix string
	// AccessKey / SecretKey are optional. Empty → default credential chain.
	AccessKey string
	SecretKey string
	Logger    *slog.Logger
}

func NewDownloadsProxy(cfg DownloadsProxyConfig) (*DownloadsProxy, error) {
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

	return &DownloadsProxy{
		client: s3.NewFromConfig(awsCfg, s3Opts...),
		bucket: cfg.Bucket,
		prefix: prefix,
		logger: logger,
	}, nil
}

// Bucket / Prefix accessors are exposed for log lines and tests.
func (p *DownloadsProxy) Bucket() string { return p.bucket }
func (p *DownloadsProxy) Prefix() string { return p.prefix }

func (p *DownloadsProxy) binaryKey(filename string) string {
	return p.prefix + "/" + filename
}

// GetBinary opens an authenticated GET on the binary object. Caller is
// responsible for closing the returned Body.
func (p *DownloadsProxy) GetBinary(ctx context.Context, filename string) (*s3.GetObjectOutput, error) {
	return p.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(p.binaryKey(filename)),
	})
}

// isAliyunOSSEndpoint mirrors the helper in server/internal/storage/s3.go.
// Kept inline to avoid widening that package's public surface.
func isAliyunOSSEndpoint(endpointURL string) bool {
	endpoint := strings.ToLower(endpointURL)
	return strings.Contains(endpoint, ".aliyuncs.com") && strings.Contains(endpoint, "oss-")
}

// IsSafeDownloadFilename guards the binary proxy route param against
// path traversal and other shenanigans. The route param is interpolated
// into an OSS object key, so a filename of `../something` would resolve
// to a different object. Accept only ASCII filename characters, no
// separators, no leading dot.
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
	// by `apps/desktop/scripts/package.mjs` (and the latest-*.yml metadata
	// files electron-builder emits) are bare ASCII; if that ever changes
	// the regex here is the single point to extend.
	for _, r := range name {
		if r < 0x20 || r > 0x7E {
			return false
		}
	}
	return true
}

// GetDownloadFile streams a single artifact from OSS through to the
// client. Mounted at `/api/downloads/{filename}`. Serves both the
// installer binaries and the `latest-*.yml` metadata files that
// electron-updater polls.
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
	// Cache lifetime depends on the artifact class:
	//   - latest-*.yml is the electron-updater poll target; clients need
	//     a fresh-enough copy that a republish becomes visible quickly.
	//     Short TTL (60s) here matches what the upload step in
	//     .github/workflows/lilith-desktop-release.yml sets on the OSS
	//     object metadata.
	//   - Versioned installer filenames are immutable by contract — the
	//     version-in-filename scheme means a re-launched same-release
	//     desktop can reuse its cached dmg indefinitely.
	if strings.HasPrefix(filename, "latest") && strings.HasSuffix(filename, ".yml") {
		w.Header().Set("Cache-Control", "public, max-age=60")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, out.Body); err != nil {
		// Connection cut mid-stream — log but can't recover the
		// response (headers + status already flushed).
		h.Downloads.logger.WarnContext(r.Context(), "downloads: stream interrupted",
			"err", err, "filename", filename)
	}
}
