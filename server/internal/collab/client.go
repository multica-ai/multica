package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// SecretHeader authenticates the API and the collaboration service to each
// other.
const SecretHeader = "X-Collab-Secret"

// ErrUnavailable means the service could not be reached or failed; callers
// fall back to writing the stored doc directly.
var ErrUnavailable = errors.New("collaboration service unavailable")

// Client talks to the collaboration service's internal endpoints.
type Client struct {
	baseURL *url.URL
	secret  string
	http    *http.Client
}

// NewClient returns nil when either setting is empty: collaboration is off.
func NewClient(internalURL, secret string) (*Client, error) {
	internalURL, secret = strings.TrimSpace(internalURL), strings.TrimSpace(secret)
	if internalURL == "" || secret == "" {
		return nil, nil
	}
	u, err := url.Parse(strings.TrimRight(internalURL, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid COLLAB_INTERNAL_URL %q", internalURL)
	}
	return &Client{baseURL: u, secret: secret, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

// Secret is the shared secret, for verifying the service's calls back.
func (c *Client) Secret() string { return c.secret }

// WriteRequest is a whole-document write. BaseContent is the stored content
// at BaseRevision, the common ancestor for the three-way merge.
type WriteRequest struct {
	Actor        Actor   `json:"actor"`
	BaseRevision int64   `json:"base_revision,omitempty"`
	BaseContent  *string `json:"base_content,omitempty"`
	Content      *string `json:"content,omitempty"`
	Title        *string `json:"title,omitempty"`
	Message      string  `json:"message,omitempty"`
	Action       string  `json:"action,omitempty"`
	ActionRef    *int32  `json:"action_ref,omitempty"`
	TaskID       string  `json:"task_id,omitempty"`
}

// EditRequest is a list of find-and-replace edits against the live text.
type EditRequest struct {
	Actor   Actor      `json:"actor"`
	Edits   []TextEdit `json:"edits"`
	Message string     `json:"message,omitempty"`
	TaskID  string     `json:"task_id,omitempty"`
}

// MergeConflict is a block both sides changed differently.
type MergeConflict struct {
	Current  string `json:"current"`
	Incoming string `json:"incoming"`
}

// Result statuses.
const (
	StatusOK       = "ok"
	StatusConflict = "conflict"
	// StatusStale: a restore named a revision that is no longer the stored
	// one once pending edits are saved. Nothing was written.
	StatusStale      = "stale"
	StatusEditFailed = "edit_failed"
	StatusGone       = "gone"
	// StatusTooLarge: the write would make the doc larger than the API stores.
	// The live document is left as it was.
	StatusTooLarge = "too_large"
)

// Result of a write or edit. Content is the live text after the write, or at
// the time of a conflict or failed edit.
type Result struct {
	Status    string          `json:"status"`
	Revision  int64           `json:"revision"`
	Content   string          `json:"content"`
	Merged    bool            `json:"merged"`
	Conflicts []MergeConflict `json:"conflicts,omitempty"`
	Failure   *EditFailure    `json:"failure,omitempty"`
	// Loaded: a flushed document is open in the service.
	Loaded bool `json:"loaded,omitempty"`
}

// Write, Edit and Flush address a live document by its service name:
// DocumentName for a doc, IssueDocumentName for an issue description.
func (c *Client) Write(ctx context.Context, document string, req WriteRequest) (Result, error) {
	return c.call(ctx, document, "write", req)
}

func (c *Client) Edit(ctx context.Context, document string, req EditRequest) (Result, error) {
	return c.call(ctx, document, "edit", req)
}

// Flush persists pending editor changes of a loaded document, so a read sees
// what people have typed. Documents nobody has open return immediately;
// loaded reports whether it was open.
func (c *Client) Flush(ctx context.Context, document string) (loaded bool, err error) {
	result, err := c.call(ctx, document, "flush", struct{}{})
	return result.Loaded, err
}

// RevalidateRequest names the editors a revocation may affect. WorkspaceID
// is required; the other fields narrow it down.
type RevalidateRequest struct {
	WorkspaceID string `json:"workspace_id"`
	// Actor limits it to one person's or agent's connections; Name is unused.
	Actor *Actor `json:"actor,omitempty"`
	// Document limits it to one document (DocumentName, IssueDocumentName).
	Document string `json:"document,omitempty"`
	// Type limits it to documents of a type: "doc" or "issue".
	Type string `json:"type,omitempty"`
}

// Revalidate asks the editors connected under req for fresh tickets, which
// the API mints only for those still allowed in; the rest are disconnected.
// Call it after revoking access to a workspace, project or issue.
func (c *Client) Revalidate(ctx context.Context, req RevalidateRequest) error {
	raw, err := json.Marshal(req)
	if err != nil {
		return err
	}
	endpoint := c.baseURL.JoinPath("internal", "revalidate")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(SecretHeader, c.secret)
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("revalidate returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func (c *Client) call(ctx context.Context, document, op string, body any) (Result, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}
	endpoint := c.baseURL.JoinPath("internal", "documents", document, op)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(SecretHeader, c.secret)
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusConflict, http.StatusRequestEntityTooLarge:
		var result Result
		if err := json.Unmarshal(data, &result); err != nil {
			return Result{}, fmt.Errorf("%w: decode %s response: %v", ErrUnavailable, op, err)
		}
		if op == "flush" {
			result.Status = StatusOK
		}
		return result, nil
	case http.StatusNotFound:
		return Result{Status: StatusGone}, nil
	}
	err = fmt.Errorf("%s returned %d: %s", op, resp.StatusCode, strings.TrimSpace(string(data)))
	// Only a service that cannot answer is worked around by writing to the
	// database directly. A request it refused would be refused again, and
	// writing around a running service would leave its live copy behind.
	if resp.StatusCode >= 500 {
		return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return Result{}, err
}

// Proxy forwards editor WebSockets to the service. The ticket in the first
// message authenticates them, so the route needs no session.
func (c *Client) Proxy() http.Handler {
	target := *c.baseURL
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(&target)
			r.Out.URL.Path = "/"
			r.Out.URL.RawPath = ""
			// Browsers send cookies along; the service has no use for them.
			r.Out.Header.Del("Cookie")
			r.Out.Header.Del("Authorization")
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			slog.Warn("collab proxy failed", "error", err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "websocket upgrade required", http.StatusBadRequest)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}
