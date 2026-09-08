package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

// This file is a characterization (snapshot) test of the CLI client's
// OUTBOUND header set, one case per exported method that reaches the Multica
// server. It asserts the FULL set — not "contains X" — so that any refactor
// which moves header construction into a shared transport layer (MUL-6638)
// has to keep the wire bytes identical, and any accidental addition or
// omission fails here instead of at a server-side capability gate.
//
// Two entries below deliberately record a gap rather than a contract:
// HealthCheck and DownloadFile-with-an-absolute-URL send no identity headers
// at all. They are pinned as-is so the difference stays visible; changing
// them is a wire-contract decision, not a refactor side effect.

// wireHeaderKeys is the set of request headers this snapshot tracks: every
// Multica protocol header plus the transport headers whose presence is part
// of the contract. Anything outside this list (Host, User-Agent, Accept-
// Encoding, Content-Length) is set by net/http, not by our client code.
var wireHeaderKeys = []string{
	"Authorization",
	"Content-Type",
	"If-None-Match",
	"X-Agent-ID",
	"X-Client-Capabilities",
	"X-Client-OS",
	"X-Client-Platform",
	"X-Client-Version",
	"X-Task-ID",
	"X-Workspace-ID",
}

// captureWireHeaders renders the tracked headers of r as a stable, comparable
// string. A multipart Content-Type is collapsed to its media type because the
// boundary is random per request.
func captureWireHeaders(r *http.Request) string {
	var parts []string
	for _, key := range wireHeaderKeys {
		values, ok := r.Header[http.CanonicalHeaderKey(key)]
		if !ok {
			continue
		}
		joined := strings.Join(values, "|")
		if key == "Content-Type" && strings.HasPrefix(joined, "multipart/form-data") {
			joined = "multipart/form-data"
		}
		parts = append(parts, key+": "+joined)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

// wireHeadersTestClient is the fully-populated client every case below shares:
// every optional field is set so an omitted header is a real omission and not
// an unset input.
func wireHeadersTestClient(baseURL string) *APIClient {
	c := NewAPIClient(baseURL, "ws-1", "tok-1")
	c.AgentID = "agent-1"
	c.TaskID = "task-1"
	c.Platform = "cli-test"
	c.Version = "9.9.9"
	c.OS = "linux"
	return c
}

// fullWireHeaders is the header set every authenticated CLI request currently
// carries. Written out longhand (not composed from the client fields) so the
// expectation is a literal snapshot of the wire, readable without running the
// code.
const authWireHeader = "Authorization: Bearer tok-1"

const protocolWireHeaders = `X-Agent-ID: agent-1
X-Client-Capabilities: stable_attachment_urls
X-Client-OS: linux
X-Client-Platform: cli-test
X-Client-Version: 9.9.9
X-Task-ID: task-1
X-Workspace-ID: ws-1`

// The three variants differ only in Content-Type; captureWireHeaders sorts by
// header name, so Content-Type lands between Authorization and the X-* block.
const (
	fullWireHeaders      = authWireHeader + "\n" + protocolWireHeaders
	jsonBodyWireHeaders  = authWireHeader + "\nContent-Type: application/json\n" + protocolWireHeaders
	multipartWireHeaders = authWireHeader + "\nContent-Type: multipart/form-data\n" + protocolWireHeaders
)

func TestAPIClientOutboundHeaderSnapshot(t *testing.T) {
	// A response body that satisfies every decoder used below: an attachment
	// response, a plain object, and an opaque byte payload all read cleanly
	// from it.
	const responseBody = `{"id":"att-1","url":"/u","download_url":"/d","markdown_url":"/m","filename":"f.txt"}`

	cases := []struct {
		name string
		want string
		call func(ctx context.Context, c *APIClient) error
	}{
		{
			name: "GetJSON",
			want: fullWireHeaders,
			call: func(ctx context.Context, c *APIClient) error {
				return c.GetJSON(ctx, "/api/test", nil)
			},
		},
		{
			name: "GetJSONWithHeaders",
			want: fullWireHeaders,
			call: func(ctx context.Context, c *APIClient) error {
				_, err := c.GetJSONWithHeaders(ctx, "/api/test", nil)
				return err
			},
		},
		{
			name: "DeleteJSON",
			want: fullWireHeaders,
			call: func(ctx context.Context, c *APIClient) error {
				return c.DeleteJSON(ctx, "/api/test")
			},
		},
		{
			name: "DeleteJSONWithBody",
			want: jsonBodyWireHeaders,
			call: func(ctx context.Context, c *APIClient) error {
				return c.DeleteJSONWithBody(ctx, "/api/test", map[string]string{"k": "v"})
			},
		},
		{
			name: "PostJSON",
			want: jsonBodyWireHeaders,
			call: func(ctx context.Context, c *APIClient) error {
				return c.PostJSON(ctx, "/api/test", map[string]string{"k": "v"}, nil)
			},
		},
		{
			name: "PutJSON",
			want: jsonBodyWireHeaders,
			call: func(ctx context.Context, c *APIClient) error {
				return c.PutJSON(ctx, "/api/test", map[string]string{"k": "v"}, nil)
			},
		},
		{
			name: "PatchJSON",
			want: jsonBodyWireHeaders,
			call: func(ctx context.Context, c *APIClient) error {
				return c.PatchJSON(ctx, "/api/test", map[string]string{"k": "v"}, nil)
			},
		},
		{
			name: "UploadFile",
			want: multipartWireHeaders,
			call: func(ctx context.Context, c *APIClient) error {
				_, err := c.UploadFile(ctx, []byte("x"), "f.txt", "issue-1")
				return err
			},
		},
		{
			name: "UploadChatAttachment",
			want: multipartWireHeaders,
			call: func(ctx context.Context, c *APIClient) error {
				_, err := c.UploadChatAttachment(ctx, []byte("x"), "f.txt", "task-1")
				return err
			},
		},
		{
			name: "UploadFileWithURL",
			want: multipartWireHeaders,
			call: func(ctx context.Context, c *APIClient) error {
				_, _, err := c.UploadFileWithURL(ctx, []byte("x"), "f.txt")
				return err
			},
		},
		{
			name: "ImportSkillFile",
			want: multipartWireHeaders,
			call: func(ctx context.Context, c *APIClient) error {
				return c.ImportSkillFile(ctx, []byte("x"), "s.skill", "skip", nil)
			},
		},
		{
			name: "UploadPrivatePlugin",
			want: multipartWireHeaders,
			call: func(ctx context.Context, c *APIClient) error {
				return c.UploadPrivatePlugin(ctx, "/api/plugins/private", []byte("x"), "p.zip", nil)
			},
		},
		{
			// A server-relative download_url is resolved against BaseURL and
			// gets the full header set — this is the path that fetches an
			// attachment from the Multica server itself.
			name: "DownloadFile relative URL",
			want: fullWireHeaders,
			call: func(ctx context.Context, c *APIClient) error {
				_, err := c.DownloadFile(ctx, "/api/attachments/att-1/download")
				return err
			},
		},
		{
			// Characterized gap #1: an absolute URL is assumed to be a signed
			// third-party (S3/CloudFront) URL, so NO headers are attached —
			// deliberately, to avoid disturbing the query-string signature.
			// A shared transport must preserve this carve-out; attaching
			// operator-configured extra headers here would leak a ZTN
			// credential to the object store.
			name: "DownloadFile absolute URL sends no client headers",
			want: "",
			call: func(ctx context.Context, c *APIClient) error {
				_, err := c.DownloadFile(ctx, c.BaseURL+"/signed/object?sig=abc")
				return err
			},
		},
		{
			// Characterized gap #2: HealthCheck skips setHeaders entirely, so
			// /health is the one CLI request that carries neither auth nor
			// identity. Behind a header-injecting reverse proxy or ZTN this
			// request cannot authenticate, which is why `multica setup`'s
			// reachability probe misreads such deployments (MUL-6638).
			name: "HealthCheck sends no client headers",
			want: "",
			call: func(ctx context.Context, c *APIClient) error {
				_, err := c.HealthCheck(ctx)
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			var requests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				got = captureWireHeaders(r)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, responseBody)
			}))
			defer srv.Close()

			if err := tc.call(context.Background(), wireHeadersTestClient(srv.URL)); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if requests != 1 {
				t.Fatalf("%s: server saw %d requests, want 1", tc.name, requests)
			}
			if got != tc.want {
				t.Errorf("%s outbound headers changed.\ngot:\n%s\nwant:\n%s", tc.name, got, tc.want)
			}
		})
	}
}

// TestAPIClientIdentityHeadersOmittedWhenUnset pins the other half of the
// contract: an unset field must omit its header rather than send an empty
// value, because the server distinguishes "absent" from "empty" for the
// capability and attribution headers.
func TestAPIClientIdentityHeadersOmittedWhenUnset(t *testing.T) {
	origPlatform, origVersion, origOS := ClientPlatform, ClientVersion, ClientOS
	ClientPlatform, ClientVersion, ClientOS = "", "", ""
	t.Cleanup(func() {
		ClientPlatform, ClientVersion, ClientOS = origPlatform, origVersion, origOS
	})

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = captureWireHeaders(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	// No token, workspace, agent, task, or identity overrides: only the
	// always-on capability header should survive.
	if err := NewAPIClient(srv.URL, "", "").GetJSON(context.Background(), "/api/test", nil); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	const want = "X-Client-Capabilities: stable_attachment_urls"
	if got != want {
		t.Errorf("outbound headers = %q, want %q", got, want)
	}
}
