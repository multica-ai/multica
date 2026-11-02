package localhost

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// callerKind is who reached the v2 API. The entry layer authenticates a
// request into a caller; one policy table decides what each kind may do,
// and the conversation capabilities never branch on it.
type callerKind int

const (
	// callerMain is Desktop's main process with the profile token.
	callerMain callerKind = iota + 1
	// callerRenderer is Desktop's renderer with the derived renderer
	// token, a WebSocket ticket or a file key.
	callerRenderer
	// callerRemote is another device of this account over the device link.
	callerRemote
)

// headerClientID names one Desktop client instance. Host requests for a
// conversation go to the client that last acted on it, and survive that
// client reconnecting.
const headerClientID = "X-Multica-Client-ID"

// caller is who made a request: its kind, the client key host requests
// route by ("local/<id>" or "<peer>/<id>"), and for a remote device its
// identity.
type caller struct {
	kind       callerKind
	clientKey  string
	peer       string
	deviceName string
}

func clientKey(prefix string, h http.Header) string {
	id := h.Get(headerClientID)
	if id == "" || len(id) > 128 {
		return ""
	}
	return prefix + "/" + id
}

type callerKey struct{}

func withCaller(ctx context.Context, c caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

func callerOf(ctx context.Context) caller {
	c, _ := ctx.Value(callerKey{}).(caller)
	return c
}

type remotePeerKey struct{}

// RemotePeer is the device a device-link connection comes from.
type RemotePeer struct{ ID, DeviceName string }

// WithRemotePeer records the device a device-link connection comes from;
// the device-link server sets it from the authenticated stream.
func WithRemotePeer(ctx context.Context, p RemotePeer) context.Context {
	return context.WithValue(ctx, remotePeerKey{}, p)
}

// RendererToken derives the renderer's bearer token from the profile token,
// so it rotates with it and Desktop's main process can compute it without
// another credential file.
func RendererToken(token string) string { return derive(token, "multica-local-renderer") }

// FileKey derives the key that lets the renderer stream conversation files
// from URLs such as <img src>, which cannot carry an Authorization header.
// It only opens the file routes, so a leaked URL cannot run anything.
func FileKey(token string) string { return derive(token, "multica-local-file") }

func derive(token, purpose string) string {
	if token == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(purpose))
	return hex.EncodeToString(mac.Sum(nil))
}

func equal(a, b string) bool {
	return b != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func loopbackHost(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	switch strings.ToLower(strings.Trim(host, "[]")) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// rendererOrigin admits the Origins Desktop's renderer sends. Measured with
// Desktop's window settings (sandbox, webSecurity off): fetch from the
// packaged file:// page and the dev server sends none, while WebSocket sends
// "file://" or the dev server's loopback origin. A web page elsewhere has
// another origin and, without the token, no access either way.
func rendererOrigin(origin string) bool {
	if origin == "" || origin == "file://" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Path != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// authenticate is the entry layer for loopback callers. The main token keeps
// the no-Origin rule of Authorize; renderer credentials accept the
// renderer's own Origins.
func (v *V2) authenticate(r *http.Request) (callerKind, bool) {
	if !loopbackHost(r) {
		return 0, false
	}
	origin := r.Header.Get("Origin")
	if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		switch {
		case equal(bearer, v.token) && origin == "":
			return callerMain, true
		case equal(bearer, RendererToken(v.token)) && rendererOrigin(origin):
			return callerRenderer, true
		}
		return 0, false
	}
	if !rendererOrigin(origin) {
		return 0, false
	}
	q := r.URL.Query()
	if ticket := q.Get("ticket"); ticket != "" && r.Method == http.MethodGet && strings.TrimPrefix(r.URL.Path, "/local/v2") == "/ws" {
		t, ok := v.tickets.take(ticket)
		if !ok {
			return 0, false
		}
		t.scope.set(r.Header)
		if t.clientID != "" {
			r.Header.Set(headerClientID, t.clientID)
		}
		return t.kind, true
	}
	if key := q.Get("file_key"); key != "" && r.Method == http.MethodGet && equal(key, FileKey(v.token)) && isFilePath(strings.TrimPrefix(r.URL.Path, "/local/v2")) {
		scopeFromQuery(q).set(r.Header)
		return callerRenderer, true
	}
	return 0, false
}

// allowed is the policy table: what each caller may reach. command is the
// decoded command type for POST /sessions/{id}/commands, "" otherwise.
func allowed(k callerKind, method, path, command string) bool {
	switch {
	case path == "/preview/services" || strings.HasPrefix(path, "/preview/services/"):
		return k == callerMain || k == callerRemote
	// Updates of this device stop its own providers.
	case path == "/computer-host" || path == "/drain" || path == "/undrain" || path == "/stop-providers":
		return k == callerMain
	// Tickets are for this device's renderer; a remote device's daemon
	// issues its own.
	case path == "/ws-ticket":
		return k == callerMain || k == callerRenderer
	// Paths that widen what the daemon may read come only from a picker the
	// renderer cannot fake: this device's OS picker, or a remote device's
	// main process, which its own daemon admits alone to these routes.
	case method == http.MethodPost && (path == "/folder-projects" || path == "/preview/roots"), command == "set_directory":
		return k == callerMain || k == callerRemote
	}
	return k == callerMain || k == callerRenderer || k == callerRemote
}

var filePathPattern = regexp.MustCompile(`^/(attachments/[\w-]{1,128}|sessions/[\w:.~-]{1,160}/image|preview/content)$`)

// isFilePath reports whether path is a route a file key may stream.
func isFilePath(path string) bool { return filePathPattern.MatchString(path) }

// requestScope is the account and workspace a v2 request acts for.
type requestScope struct{ apiOrigin, userID, workspaceID string }

func scopeFromHeaders(h http.Header) requestScope {
	return requestScope{h.Get(headerAPIOrigin), h.Get(headerUserID), h.Get(headerWorkspaceID)}
}

// File URLs carry the scope as query parameters because <img> cannot send
// the scope headers.
func scopeFromQuery(q url.Values) requestScope {
	return requestScope{q.Get("api_origin"), q.Get("user_id"), q.Get("workspace_id")}
}

func (s requestScope) set(h http.Header) {
	h.Set(headerAPIOrigin, s.apiOrigin)
	h.Set(headerUserID, s.userID)
	h.Set(headerWorkspaceID, s.workspaceID)
}

const ticketTTL = 30 * time.Second

type ticket struct {
	kind     callerKind
	scope    requestScope
	clientID string
	expires  time.Time
}

// tickets are single-use WebSocket credentials: a browser WebSocket cannot
// send the Authorization or scope headers, so the renderer trades an
// authenticated request for a ticket bound to them.
type tickets struct {
	mu    sync.Mutex
	items map[string]ticket
	now   func() time.Time
}

func (t *tickets) issue(kind callerKind, scope requestScope, clientID string) string {
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	id := hex.EncodeToString(buf)
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for k, v := range t.items {
		if now.After(v.expires) {
			delete(t.items, k)
		}
	}
	t.items[id] = ticket{kind: kind, scope: scope, clientID: clientID, expires: now.Add(ticketTTL)}
	return id
}

func (t *tickets) take(id string) (ticket, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v, ok := t.items[id]
	delete(t.items, id)
	if !ok || t.now().After(v.expires) {
		return ticket{}, false
	}
	return v, true
}

// ticketResponse is the body of POST /local/v2/ws-ticket.
func ticketResponse(id string) map[string]any {
	return map[string]any{"ticket": id, "expires_in": int(ticketTTL / time.Second)}
}
