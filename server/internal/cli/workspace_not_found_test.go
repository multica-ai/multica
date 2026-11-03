package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSafeServerOrigin(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"https://user:synthetic-password@example.invalid:8443/private?id=synthetic-token#secret", "https://example.invalid:8443"},
		{"https://user%40name:pass%0Aword@example.invalid/path", "https://example.invalid"},
		{"http://localhost:1234/path", "http://localhost:1234"},
		{"http://[::1]:8080/path", "http://[::1]:8080"},
		{"http://[not:ipv6]/path", "[redacted]"},
		{"http://[not-ip]/path", "[redacted]"},
		{"https://[2001:db8::1]/path", "https://[2001:db8::1]"},
		{"http://[fe80::1%25en0]:8080/path", "http://[fe80::1%25en0]:8080"},
		{"ws://localhost:8080/ws", "http://localhost:8080"},
		{"wss://example.invalid/ws", "https://example.invalid"},
		{"https://example.invalid/encoded%0Apath?token=synthetic", "https://example.invalid"},
		{"https://example.invalid:0/", "[redacted]"},
		{"https://example.invalid:65536/", "[redacted]"},
		{"https://example.invalid:bad/", "[redacted]"},
		{"https:///private", "[redacted]"},
		{"https:private", "[redacted]"},
		{"example.invalid/private", "[redacted]"},
		{"https://example.invalid/%zz", "[redacted]"},
		{"https://example.invalid/\x1b[31m", "[redacted]"},
		{"https://example.invalid/\nsecret", "[redacted]"},
		{"https://example.invalid/\u202esecret", "[redacted]"},
		{"https://[fe80::1%25%0a]:80/", "[redacted]"},
		{"https://user%ZZ@example.invalid", "[redacted]"},
		{"file:///private", "[redacted]"},
		{"", "[redacted]"},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if got := safeServerOrigin(tc.raw); got != tc.want {
				t.Errorf("safe origin = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSafeProfileLabel(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"", "default"}, {"dev-1.example_local", "dev-1.example_local"},
		{"dev\nunsafe", "[redacted]"}, {"dev\x1b[0m", "[redacted]"},
		{"$(echo unsafe)", "[redacted]"}, {"`echo unsafe`", "[redacted]"},
		{"dev;echo unsafe", "[redacted]"}, {"../../private", "[redacted]"},
		{"dev\u202eunsafe", "[redacted]"}, {strings.Repeat("x", 65), "[redacted]"},
	} {
		if got := safeProfileLabel(tc.raw); got != tc.want {
			t.Errorf("profile label = %q, want %q", got, tc.want)
		}
	}
}

func TestWorkspaceNotFoundScopeAndErrorContract(t *testing.T) {
	withLang(t, "en_US.UTF-8")
	t.Setenv("MULTICA_DEBUG", "")
	for _, tc := range []struct {
		path        string
		workspace   bool
		status      int
		wantContext bool
	}{
		{"/api/issues/synthetic", true, 404, true},
		{"/api/issues/synthetic?include=details", true, 404, true},
		{"/api/workspaces/synthetic", false, 404, true},
		{"/api/projects/synthetic", true, 404, true},
		{"/api/issues/synthetic", false, 404, false},
		{"/api/issues/synthetic", true, 401, false},
		{"/api/issues/synthetic", true, 403, false},
		{"/api/issues/synthetic", true, 400, false},
		{"/api/user/me", true, 404, false},
		{"/api/workspaces", true, 404, false},
		{"/api/cloud-billing/balance", true, 404, false},
		{"/api/upload-file", true, 404, false},
		{"/api/issues-other/synthetic", true, 404, false},
		{"/health", true, 404, false},
	} {
		t.Run(fmt.Sprintf("%s-%t-%d", tc.path, tc.workspace, tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"synthetic-body"}`))
			}))
			defer srv.Close()
			ws := ""
			if tc.workspace {
				ws = "synthetic-workspace"
			}
			c := NewAPIClient(srv.URL, ws, "synthetic-token")
			c.InvocationContext = &InvocationContext{Profile: "staging"}
			var out any
			err := c.GetJSON(context.Background(), tc.path, &out)
			var he *HTTPError
			if !errors.As(err, &he) || he.StatusCode != tc.status {
				t.Fatalf("HTTPError lost: %v", err)
			}
			got := FormatError(fmt.Errorf("get fixture: %w", err), false)
			if strings.Contains(got, "server=") != tc.wantContext {
				t.Fatalf("context scope wrong: %s", got)
			}
			if strings.Contains(got, "synthetic-workspace") || strings.Contains(got, "synthetic-token") {
				t.Fatalf("diagnostic leaked private input: %s", got)
			}
			if tc.status == 404 && ExitCodeFor(err) != 4 {
				t.Errorf("exit code changed")
			}
			if got := FormatError(WithUserMessage("command-specific guidance", err), false); got != "command-specific guidance" {
				t.Errorf("custom message changed: %s", got)
			}
			if got := FormatError(err, true); !strings.Contains(got, err.Error()) {
				t.Errorf("debug chain lost")
			}
		})
	}
	if got := FormatError(&os.PathError{Op: "open", Path: "synthetic-local-file", Err: os.ErrNotExist}, false); strings.Contains(got, "server=") {
		t.Errorf("local error got API guidance")
	}
	if got := FormatError(&NetworkError{Kind: KindNetworkDNS, Err: errors.New("synthetic lookup")}, false); strings.Contains(got, "server=") {
		t.Errorf("network error got 404 context")
	}
}

func TestWorkspaceNotFoundHelpersAndIsolation(t *testing.T) {
	withLang(t, "en_US.UTF-8")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer srv.Close()
	c := NewAPIClient(srv.URL, "synthetic-workspace", "synthetic-token")
	c.InvocationContext = &InvocationContext{Profile: "first"}
	ctx := context.Background()
	var out any
	calls := []func() error{
		func() error { return c.GetJSON(ctx, "/api/issues/synthetic", &out) },
		func() error {
			_, e := c.UploadIssueAttachment(ctx, []byte("fixture"), "fixture.txt", "synthetic-issue")
			return e
		},
		func() error {
			_, e := c.UploadChatAttachment(ctx, []byte("fixture"), "fixture.txt", "synthetic-task")
			return e
		},
		func() error { _, e := c.GetJSONWithHeaders(ctx, "/api/issues/synthetic", &out); return e },
		func() error { return c.PostJSON(ctx, "/api/issues", nil, &out) },
		func() error { return c.PutJSON(ctx, "/api/issues/synthetic", nil, &out) },
		func() error { return c.PatchJSON(ctx, "/api/issues/synthetic", nil, &out) },
		func() error { return c.DeleteJSON(ctx, "/api/issues/synthetic") },
		func() error { return c.DeleteJSONWithBody(ctx, "/api/issues/synthetic", nil) },
		func() error { _, e := c.DownloadFile(ctx, "/api/attachments/synthetic/download"); return e },
	}
	for i, call := range calls {
		if got := FormatError(call(), false); !strings.Contains(got, "profile=first") {
			t.Errorf("helper %d missing context: %s", i, got)
		}
	}
	first := calls[0]()
	c.InvocationContext.Profile = "second"
	second := calls[0]()
	if !strings.Contains(FormatError(first, false), "profile=first") || !strings.Contains(FormatError(second, false), "profile=second") {
		t.Fatal("request context leaked across calls")
	}
	c.InvocationContext.Profile = "bad; echo unsafe"
	err := calls[0]()
	if got := FormatError(err, false); !strings.Contains(got, "profile=[redacted]") || strings.Contains(got, "echo unsafe") {
		t.Fatalf("unsafe profile label: %s", got)
	}
	_, downloadErr := c.DownloadFile(ctx, "/api/attachments/synthetic/download")
	var downloadHTTPError *HTTPError
	if !errors.As(downloadErr, &downloadHTTPError) || downloadHTTPError.Path != srv.URL+"/api/attachments/synthetic/download" {
		t.Error("download error chain changed")
	}
	_, avatarErr := c.UploadIssueAttachment(ctx, []byte("fixture"), "fixture.png", "")
	if got := FormatError(avatarErr, false); strings.Contains(got, "server=") {
		t.Error("unscoped upload gained workspace context")
	}
	c.InvocationContext = nil
	if got := FormatError(calls[0](), false); strings.Contains(got, "server=") {
		t.Errorf("contextless client gained profile context")
	}
	c.InvocationContext = &InvocationContext{Profile: "download"}
	_, err = c.DownloadFile(ctx, srv.URL+"/api/attachments/synthetic/download")
	if got := FormatError(err, false); strings.Contains(got, "server=") {
		t.Errorf("absolute object download gained API context")
	}
}
