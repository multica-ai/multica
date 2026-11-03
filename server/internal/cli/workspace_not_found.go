package cli

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// InvocationContext describes the configuration scope selected by a CLI call.
// It is supplied by the caller after normal flag/env/config resolution; error
// rendering never discovers profiles or loads configuration itself.
type InvocationContext struct {
	Profile     string
	ManagedTask bool
}

// workspaceNotFoundError keeps safe diagnostics with the failing request,
// rather than a process-wide last-request value. Error and Unwrap preserve the
// original chain, including command-specific messages and debug output.
type workspaceNotFoundError struct {
	err         *HTTPError
	profile     string
	origin      string
	managedTask bool
}

func (e *workspaceNotFoundError) Error() string { return e.err.Error() }
func (e *workspaceNotFoundError) Unwrap() error { return e.err }

func (c *APIClient) responseError(method, path string, resp *http.Response) error {
	return c.withWorkspaceNotFoundContext(newHTTPError(method, path, resp), resp, workspaceAPIPath(path, c.WorkspaceID != ""))
}

func (c *APIClient) withWorkspaceNotFoundContext(err *HTTPError, resp *http.Response, workspaceScoped bool) error {
	if err.StatusCode != http.StatusNotFound || c.InvocationContext == nil || !workspaceScoped || !c.workspaceResponse(err, resp) {
		return err
	}
	return &workspaceNotFoundError{
		err:         err,
		profile:     safeProfileLabel(c.InvocationContext.Profile),
		origin:      safeServerOrigin(c.BaseURL),
		managedTask: c.InvocationContext.ManagedTask,
	}
}

// Redirects can end at an object store or an account-level route. Only the
// actual response request can establish that workspace guidance still applies.
func (c *APIClient) workspaceResponse(err *HTTPError, resp *http.Response) bool {
	if resp == nil || resp.Request == nil || resp.Request.URL == nil {
		return false
	}
	finalURL := resp.Request.URL
	origin := safeServerOrigin(finalURL.String())
	selected := safeServerOrigin(c.BaseURL)
	if origin == "[redacted]" || selected == "[redacted]" || !strings.EqualFold(origin, selected) {
		return false
	}
	// Uploads carry workspace scope in their form fields, not their route.
	if err.Path == "/api/upload-file" && finalURL.Path == "/api/upload-file" {
		return true
	}
	return workspaceAPIPath(finalURL.RequestURI(), c.WorkspaceID != "")
}

// Only known workspace API families get routing guidance. A workspace header
// alone is insufficient: account-level requests can carry it as well.
func workspaceAPIPath(path string, hasWorkspace bool) bool {
	u, err := url.Parse(path)
	if err != nil || u.IsAbs() || u.Host != "" {
		return false
	}
	if strings.HasPrefix(u.Path, "/api/workspaces/") && strings.TrimPrefix(u.Path, "/api/workspaces/") != "" {
		return true
	}
	if !hasWorkspace {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/api/"), "/")
	if !strings.HasPrefix(u.Path, "/api/") {
		return false
	}
	// Attachment object delivery does not require workspace membership; its 404 does not
	// describe workspace routing, even when it stays on the API origin.
	if parts[0] == "attachments" && len(parts) == 3 {
		switch parts[2] {
		case "download", "download-metadata", "signed-download":
			return false
		}
	}
	// These routes are protected by RequireWorkspaceMember in cmd/server/router.go.
	switch parts[0] {
	case "issues", "tasks", "quick-actions", "properties", "labels", "issue-statuses", "issue-workflows", "projects", "squads", "autopilots", "attachments", "comments", "agents", "skills", "runtimes", "chat":
		return true
	default:
		return false
	}
}

func safeProfileLabel(profile string) string {
	if profile == "" {
		return "default"
	}
	if len(profile) > 64 {
		return "[redacted]"
	}
	for _, r := range profile {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return "[redacted]"
		}
	}
	return profile
}

func safeServerOrigin(raw string) string {
	// Reject terminal controls even outside the authority before discarding the
	// path/query. Never echo parsing errors: they can contain the original URL.
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return "[redacted]"
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.Hostname() == "" {
		return "[redacted]"
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
	default:
		return "[redacted]"
	}
	if strings.ContainsAny(u.Host, "[]") || strings.Contains(u.Hostname(), ":") {
		host, _, _ := strings.Cut(u.Hostname(), "%")
		if !strings.HasPrefix(u.Host, "[") || net.ParseIP(host) == nil {
			return "[redacted]"
		}
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "[redacted]"
		}
	}
	// Escaped host characters (including an IPv6 zone) must not reintroduce
	// terminal controls when url.Parse decodes them.
	for _, r := range u.Host {
		if unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) || strings.ContainsRune("@/\\?#", r) {
			return "[redacted]"
		}
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
}

func (e *workspaceNotFoundError) contextMessage(lang Language) string {
	if e.managedTask {
		if lang == LangZH {
			return fmt.Sprintf("本次调用：managed task，server=%s。请检查运行时提供的服务器和工作区上下文；不要改用个人 profile。资源可能位于其他服务器、已不存在或无法访问。", e.origin)
		}
		return fmt.Sprintf("Current invocation: managed task, server=%s. Check the runtime-provided server and workspace context; do not fall back to a member profile. The resource may belong to another server, no longer exist, or be inaccessible.", e.origin)
	}
	if lang == LangZH {
		return fmt.Sprintf("本次调用：profile=%s，server=%s。请检查 --profile 和服务器配置，并使用相同选项运行 `multica workspace list`。资源可能位于其他服务器、已不存在或无法访问。", e.profile, e.origin)
	}
	return fmt.Sprintf("Current invocation: profile=%s, server=%s. Check --profile and server configuration; use the same options with `multica workspace list`. The resource may belong to another server, no longer exist, or be inaccessible.", e.profile, e.origin)
}
