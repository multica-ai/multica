package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWorkspaceNotFoundFinalResponse(t *testing.T) {
	withLang(t, "en_US.UTF-8")
	t.Setenv("MULTICA_DEBUG", "")
	objectCalls := 0
	object := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		objectCalls++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer object.Close()
	apiCalls := 0
	target := ""
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		if target != "" && r.URL.Query().Get("final") != "1" {
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer api.Close()
	c := NewAPIClient(api.URL, "synthetic-workspace", "synthetic-token")
	c.InvocationContext = &InvocationContext{Profile: "fixture"}
	ctx := context.Background()
	var out any
	for _, helper := range []struct {
		name string
		call func() error
	}{
		{"get", func() error { return c.GetJSON(ctx, "/api/issues/synthetic", &out) }},
		{"get_headers", func() error { _, e := c.GetJSONWithHeaders(ctx, "/api/issues/synthetic", &out); return e }},
		{"post", func() error { return c.PostJSON(ctx, "/api/issues", nil, &out) }},
		{"put", func() error { return c.PutJSON(ctx, "/api/issues/synthetic", nil, &out) }},
		{"patch", func() error { return c.PatchJSON(ctx, "/api/issues/synthetic", nil, &out) }},
		{"delete", func() error { return c.DeleteJSON(ctx, "/api/issues/synthetic") }},
		{"delete_body", func() error { return c.DeleteJSONWithBody(ctx, "/api/issues/synthetic", nil) }},
		{"issue_upload", func() error {
			_, e := c.UploadIssueAttachment(ctx, []byte("fixture"), "fixture.txt", "synthetic-issue")
			return e
		}},
		{"chat_upload", func() error {
			_, e := c.UploadChatAttachment(ctx, []byte("fixture"), "fixture.txt", "synthetic-task")
			return e
		}},
		{"skill_import", func() error { return c.ImportSkillFile(ctx, []byte("fixture"), "fixture.zip", "skip", &out) }},
		{"plugin_upload", func() error {
			return c.UploadPrivatePlugin(ctx, "/api/skills/synthetic", []byte("fixture"), "fixture.zip", &out)
		}},
		{"download", func() error { _, e := c.DownloadFile(ctx, "/api/attachments/synthetic"); return e }},
	} {
		t.Run(helper.name, func(t *testing.T) {
			for _, tc := range []struct {
				name, location string
				wantContext    bool
				api, object    int
			}{
				{"direct", "", true, 1, 0},
				{"other_origin", object.URL + "/api/issues/missing?final=1", false, 1, 1},
				{"same_origin_account", api.URL + "/api/user/me?final=1", false, 2, 0},
				{"same_origin_download", api.URL + "/api/attachments/synthetic/download?final=1", false, 2, 0},
				{"same_origin_metadata", api.URL + "/api/attachments/synthetic/download-metadata?final=1", false, 2, 0},
				{"same_origin_workspace", api.URL + "/api/issues/missing?final=1", true, 2, 0},
			} {
				t.Run(tc.name, func(t *testing.T) {
					target, apiCalls, objectCalls = tc.location, 0, 0
					err := helper.call()
					var he *HTTPError
					if !errors.As(err, &he) || he.StatusCode != 404 || ExitCodeFor(err) != 4 {
						t.Fatalf("HTTP error contract changed: %v", err)
					}
					if got := FormatError(err, false); strings.Contains(got, "server=") != tc.wantContext {
						t.Fatalf("wrong final response scope: %s", got)
					}
					if apiCalls != tc.api || objectCalls != tc.object {
						t.Fatalf("unexpected requests: api=%d object=%d", apiCalls, objectCalls)
					}
				})
			}
		})
	}
	target = ""
	for _, path := range []string{"/api/attachments/synthetic/download", "/api/attachments/synthetic/download-metadata", "/api/attachments/synthetic/signed-download"} {
		for _, prefix := range []string{"", api.URL} {
			_, err := c.DownloadFile(ctx, prefix+path)
			if got := FormatError(err, false); strings.Contains(got, "server=") {
				t.Errorf("auth-only download got workspace context: %s", got)
			}
		}
	}
}

func TestWorkspaceNotFoundMissingFinalRequest(t *testing.T) {
	withLang(t, "en_US.UTF-8")
	c := NewAPIClient("https://example.invalid", "synthetic-workspace", "synthetic-token")
	c.InvocationContext = &InvocationContext{Profile: "fixture"}
	for _, req := range []*http.Request{nil, {}, {URL: &url.URL{Path: "/api/issues/synthetic"}}} {
		resp := &http.Response{StatusCode: 404, Request: req, Body: http.NoBody}
		if got := FormatError(c.responseError(http.MethodGet, "/api/issues/synthetic", resp), false); strings.Contains(got, "server=") {
			t.Errorf("unknown final origin got workspace context: %s", got)
		}
	}
}
