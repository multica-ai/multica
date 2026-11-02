// Package qoderruntime bridges Multica's daemon task protocol to Qoder's managed sessions API.
package qoderruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/httptransport"
)

type api struct {
	base, token, workspace string
	client                 *http.Client
}
type apiError struct {
	Status int
	// Code is the machine-readable "code" field of a 4xx JSON error body,
	// when present. The rest of the body is never kept.
	Code string
}

func (e *apiError) Error() string { return fmt.Sprintf("remote API returned HTTP %d", e.Status) }

func newAPI(base, token, workspace string) (*api, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid API base URL")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return nil, fmt.Errorf("API base URL must use HTTPS except on loopback")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("API token is required")
	}
	return &api{
		base: strings.TrimRight(base, "/"), token: token, workspace: workspace,
		client: &http.Client{
			Timeout: 30 * time.Second, Transport: httptransport.CloneDefault(),
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// Errors deliberately omit remote bodies and URLs, which may echo credentials or task content.
func (a *api) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	return a.do(ctx, method, path, "application/json", body, out)
}

// upload posts one file as multipart/form-data under the "file" field.
func (a *api) upload(ctx context.Context, path, name string, data []byte, out any) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := w.WriteField("name", name); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return a.do(ctx, http.MethodPost, path, w.FormDataContentType(), &body, out)
}

func (a *api) do(ctx context.Context, method, path, contentType string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, body)
	if err != nil {
		return fmt.Errorf("build API request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	if a.workspace != "" {
		req.Header.Set("X-Workspace-ID", a.workspace)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("remote API transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := &apiError{Status: resp.StatusCode}
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			var coded struct {
				Code string `json:"code"`
			}
			if b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10)); err == nil && json.Unmarshal(b, &coded) == nil {
				e.Code = coded.Code
			}
		}
		return e
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if err != nil {
		return fmt.Errorf("read API response failed")
	}
	if len(b) > 8<<20 {
		return errResponseTooLarge
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("invalid API JSON response")
		}
	}
	return nil
}

// download fetches at most limit bytes. A site-relative URL is served by this
// API and carries its credential; an absolute URL is presigned and does not.
// Redirects are followed (Go drops the credential on cross-host hops) because
// attachment downloads redirect to the storage provider.
func (a *api) download(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	relative := strings.HasPrefix(rawURL, "/") && !strings.HasPrefix(rawURL, "//")
	target := rawURL
	if relative {
		target = a.base + rawURL
	} else if u, err := url.Parse(rawURL); err != nil || u.Scheme != "https" && !(u.Scheme == "http" && strings.HasPrefix(a.base, "http://")) {
		return nil, fmt.Errorf("invalid download URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid download URL")
	}
	if relative {
		req.Header.Set("Authorization", "Bearer "+a.token)
		if a.workspace != "" {
			req.Header.Set("X-Workspace-ID", a.workspace)
		}
	}
	client := &http.Client{Timeout: 2 * time.Minute, Transport: a.client.Transport}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("download transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &apiError{Status: resp.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read download failed")
	}
	if int64(len(b)) > limit {
		return nil, errTooLarge
	}
	return b, nil
}

var errTooLarge = errors.New("file too large")

// errResponseTooLarge reports a response body larger than api.do reads.
var errResponseTooLarge = errors.New("API response exceeds 8 MiB")
