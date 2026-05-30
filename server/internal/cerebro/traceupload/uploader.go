package traceupload

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

// UploadResult is the parsed registry response.
type UploadResult struct {
	Status     string `json:"status"` // "ingested" | "noop"
	EventCount int    `json:"event_count"`
}

// Uploader sends one batch to the registry. It is an interface so the Manager
// can be tested without a live endpoint.
type Uploader interface {
	Upload(ctx context.Context, s Sidecar, jsonl []byte) (UploadResult, error)
}

// retryableError marks a failure the Manager should retry (network error, 5xx,
// 429). A non-retryable error (4xx other than 429) means the batch is
// malformed/rejected and retrying won't help.
type retryableError struct{ err error }

func (e retryableError) Error() string { return e.err.Error() }
func (e retryableError) Unwrap() error { return e.err }

// IsRetryable reports whether err is a transient failure worth retrying.
func IsRetryable(err error) bool {
	var re retryableError
	return errors.As(err, &re)
}

// HTTPUploader is the production Uploader: it gzips meta-line + raw JSONL and
// POSTs to the registry ingest endpoint with the x-api-key header.
type HTTPUploader struct {
	Endpoint string
	APIKey   string
	Client   *http.Client
}

// Upload builds the wire body (gzip of: trace_meta line + "\n" + raw JSONL)
// and POSTs it. Returns a retryableError for transient failures.
func (u *HTTPUploader) Upload(ctx context.Context, s Sidecar, jsonl []byte) (UploadResult, error) {
	body, err := BuildBody(s, jsonl)
	if err != nil {
		return UploadResult{}, err // malformed input — not retryable
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.Endpoint, bytes.NewReader(body))
	if err != nil {
		return UploadResult{}, err
	}
	// The registry gunzips the body manually (request.arrayBuffer() →
	// parseGzippedJsonl), so we send the gzip bytes as an opaque octet-stream
	// and deliberately do NOT set Content-Encoding: gzip — that header could
	// make an intermediary (or the Next runtime) transparently decompress the
	// body before the handler sees it, leaving it with plain JSONL it then
	// rejects as "not valid gzip". Matches the registry's own client.
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("x-api-key", u.APIKey)

	client := u.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return UploadResult{}, retryableError{fmt.Errorf("post: %w", err)}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	switch {
	case resp.StatusCode == http.StatusOK:
		var out UploadResult
		if err := json.Unmarshal(respBody, &out); err != nil {
			// 200 but unparseable body — treat as success; the registry
			// confirmed receipt, body shape is informational.
			return UploadResult{Status: "ingested"}, nil
		}
		return out, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return UploadResult{}, retryableError{fmt.Errorf("registry %d: %s", resp.StatusCode, string(respBody))}
	default:
		// 4xx (401/403/400/413, ...): retrying won't help.
		return UploadResult{}, fmt.Errorf("registry %d: %s", resp.StatusCode, string(respBody))
	}
}

// BuildBody returns gzip(trace_meta-line + "\n" + rawJSONL), the exact wire
// format the registry expects. The raw JSONL is the runtime transcript, the
// first line is the sidecar meta object.
func BuildBody(s Sidecar, jsonl []byte) ([]byte, error) {
	meta, err := s.MetaLine()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(meta); err != nil {
		return nil, err
	}
	if _, err := gz.Write([]byte("\n")); err != nil {
		return nil, err
	}
	if _, err := gz.Write(jsonl); err != nil {
		return nil, err
	}
	// Ensure the transcript ends with a newline so the last event is a
	// complete JSONL line even if the source file lacked a trailing newline.
	if n := len(jsonl); n == 0 || jsonl[n-1] != '\n' {
		if _, err := gz.Write([]byte("\n")); err != nil {
			return nil, err
		}
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// hashFile returns the sha256 hex digest and byte size of a file.
func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
