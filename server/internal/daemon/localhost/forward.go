package localhost

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// AuthorizeDesktop admits Desktop's main process or renderer by their
// bearer credentials, for loopback routes outside /local/v2 that both use.
func AuthorizeDesktop(r *http.Request, token string) bool {
	if !loopbackHost(r) {
		return false
	}
	bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	origin := r.Header.Get("Origin")
	return (equal(bearer, token) && origin == "") || (equal(bearer, RendererToken(token)) && rendererOrigin(origin))
}

// AuthorizeMain admits only the native Desktop process, never renderer or file credentials.
func AuthorizeMain(r *http.Request, token string) bool {
	bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && loopbackHost(r) && r.Header.Get("Origin") == "" && equal(bearer, token)
}

var commandPath = regexp.MustCompile(`^/sessions/[^/]+/commands$`)

// Admit authenticates a /local/v2 request this daemon forwards to another
// device and applies this device's side of the policy: the remote device
// trusts what this daemon forwards, so only this device's main process may
// widen what it reads. On success the credentials are replaced by the
// scope and client headers they stand for; on failure Admit has answered.
func (v *V2) Admit(w http.ResponseWriter, r *http.Request) bool {
	kind, ok := v.authenticate(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	path := strings.TrimPrefix(r.URL.Path, "/local/v2")
	command := ""
	if kind != callerMain && r.Method == http.MethodPost && commandPath.MatchString(path) && r.Body != nil {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, v2MaxBodyBytes))
		if err != nil {
			writeV2Error(w, AppError("invalid_request", "request body is too large"))
			return false
		}
		var peek struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(body, &peek)
		command = peek.Type
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	if !allowed(kind, r.Method, path, command) {
		writeV2Error(w, AppError("forbidden", "This caller cannot use this local conversation route"))
		return false
	}
	r.Header.Del("Authorization")
	r.Header.Del("Origin")
	// Only Desktop forwards through this entry; the other device cannot tell.
	r.Header.Set(headerClientKind, timing.ClientDesktop)
	q := r.URL.Query()
	for _, k := range []string{"ticket", "file_key", "api_origin", "user_id", "workspace_id"} {
		q.Del(k)
	}
	r.URL.RawQuery = q.Encode()
	return true
}
