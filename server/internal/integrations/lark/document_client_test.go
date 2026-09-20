package lark

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTTPDocumentClientUsesDocsAIEndpoints(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_docs", 7200)
	fake.mux.HandleFunc("/open-apis/docs_ai/v1/documents/doc-token/fetch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("fetch method = %s, want POST", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{
			"format":        "xml",
			"export_option": map[string]any{"export_block_id": true},
			"read_option": map[string]any{
				"read_mode":      "section",
				"start_block_id": "block-a",
			},
		}
		if !reflect.DeepEqual(body, want) {
			t.Fatalf("fetch body = %#v, want %#v", body, want)
		}
		writeJSON(w, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{"document": map[string]any{
				"revision_id": 12,
				"content":     `<h1 block-id="block-a">Heading</h1>`,
			}},
		})
	})
	fake.mux.HandleFunc("/open-apis/docs_ai/v1/documents/doc-token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("update method = %s, want PUT", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{
			"format":      "xml",
			"command":     "block_insert_after",
			"block_id":    "-1",
			"content":     "hello",
			"revision_id": float64(12),
		}
		if !reflect.DeepEqual(body, want) {
			t.Fatalf("update body = %#v, want %#v", body, want)
		}
		writeJSON(w, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{"document": map[string]any{"revision_id": 13}, "result": "success"},
		})
	})

	client := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL()})
	snapshot, err := client.FetchDocument(context.Background(), testCreds(), DocumentFetchParams{
		Token:        "doc-token",
		Format:       "xml",
		Scope:        DocumentFetchScopeSection,
		StartBlockID: "block-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RevisionID != 12 || snapshot.Content == "" || !reflect.DeepEqual(snapshot.BlockIDs, []string{"block-a"}) {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	updated, err := client.UpdateDocument(context.Background(), testCreds(), DocumentUpdateParams{
		Token:      "doc-token",
		Format:     "xml",
		Command:    "append",
		Content:    "hello",
		RevisionID: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.RevisionID != 13 {
		t.Fatalf("revision = %d, want 13", updated.RevisionID)
	}
}

func TestHTTPDocumentClientDoesNotRetryUpdateTimeout(t *testing.T) {
	var updateCalls atomic.Int32
	transport := documentRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			return documentJSONResponse(http.StatusOK, `{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`), nil
		}
		updateCalls.Add(1)
		return nil, context.DeadlineExceeded
	})
	client := NewHTTPAPIClient(HTTPClientConfig{HTTPClient: &http.Client{Transport: transport}})
	_, err := client.UpdateDocument(context.Background(), testCreds(), DocumentUpdateParams{Token: "doc-token", Format: "xml", Command: "append", Content: "hello"})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if got := updateCalls.Load(); got != 1 {
		t.Fatalf("update calls = %d, want 1", got)
	}
}

func TestHTTPDocumentClientHonorsInstallationRegion(t *testing.T) {
	for _, tc := range []struct {
		region   Region
		wantHost string
	}{{RegionFeishu, "open.feishu.cn"}, {RegionLark, "open.larksuite.com"}} {
		t.Run(string(tc.region), func(t *testing.T) {
			var documentHost string
			transport := documentRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
					return documentJSONResponse(http.StatusOK, `{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`), nil
				}
				documentHost = r.URL.Host
				return documentJSONResponse(http.StatusOK, `{"code":0,"msg":"ok","data":{"document":{"revision_id":1,"content":"ok"}}}`), nil
			})
			client := NewHTTPAPIClient(HTTPClientConfig{HTTPClient: &http.Client{Transport: transport}})
			creds := testCreds()
			creds.Region = tc.region
			if _, err := client.FetchDocument(context.Background(), creds, DocumentFetchParams{Token: "doc-token", Format: "markdown"}); err != nil {
				t.Fatal(err)
			}
			if documentHost != tc.wantHost {
				t.Fatalf("host = %q, want %q", documentHost, tc.wantHost)
			}
		})
	}
}

func TestHTTPDocumentClientRefreshesRejectedTenantTokenOnce(t *testing.T) {
	var tokenCalls atomic.Int32
	var documentCalls atomic.Int32
	transport := documentRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			n := tokenCalls.Add(1)
			return documentJSONResponse(http.StatusOK, `{"code":0,"msg":"ok","tenant_access_token":"tok`+string(rune('0'+n))+`","expire":7200}`), nil
		}
		if documentCalls.Add(1) == 1 {
			return documentJSONResponse(http.StatusBadRequest, `{"code":99991663,"msg":"invalid token"}`), nil
		}
		return documentJSONResponse(http.StatusOK, `{"code":0,"msg":"ok","data":{"document":{"revision_id":1,"content":"ok"}}}`), nil
	})
	client := NewHTTPAPIClient(HTTPClientConfig{HTTPClient: &http.Client{Transport: transport}})
	if _, err := client.FetchDocument(context.Background(), testCreds(), DocumentFetchParams{Token: "doc-token", Format: "xml"}); err != nil {
		t.Fatal(err)
	}
	if tokenCalls.Load() != 2 || documentCalls.Load() != 2 {
		t.Fatalf("token calls = %d, document calls = %d", tokenCalls.Load(), documentCalls.Load())
	}
}

func TestHTTPDocumentClientDoesNotReplayUpdateAfterTokenRejection(t *testing.T) {
	var updateCalls atomic.Int32
	transport := documentRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			return documentJSONResponse(http.StatusOK, `{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`), nil
		}
		updateCalls.Add(1)
		return documentJSONResponse(http.StatusBadRequest, `{"code":99991663,"msg":"invalid token"}`), nil
	})
	client := NewHTTPAPIClient(HTTPClientConfig{HTTPClient: &http.Client{Transport: transport}})
	_, err := client.UpdateDocument(context.Background(), testCreds(), DocumentUpdateParams{Token: "doc-token", Format: "xml", Command: "append", Content: "hello"})
	if err == nil {
		t.Fatal("update unexpectedly succeeded")
	}
	if got := updateCalls.Load(); got != 1 {
		t.Fatalf("update calls = %d, want 1", got)
	}
}

func TestHTTPDocumentClientRejectsPartialUpdateResult(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok", 7200)
	fake.mux.HandleFunc("/open-apis/docs_ai/v1/documents/doc-token", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{"document": map[string]any{"revision_id": 13}, "result": "partial_success"},
		})
	})
	client := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL()})
	_, err := client.UpdateDocument(context.Background(), testCreds(), DocumentUpdateParams{Token: "doc-token", Format: "xml", Command: "append", Content: "hello"})
	if err == nil {
		t.Fatal("partial update result was reported as success")
	}
}

func TestHTTPDocumentClientRejectsOversizedResponse(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok", 7200)
	fake.mux.HandleFunc("/open-apis/docs_ai/v1/documents/doc-token/fetch", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", DocumentMaxResponseBytes+1))
	})
	client := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL()})
	if _, err := client.FetchDocument(context.Background(), testCreds(), DocumentFetchParams{Token: "doc-token", Format: "xml"}); err == nil {
		t.Fatal("accepted oversized response")
	}
}

func TestHTTPDocumentClientErrorExcludesResponseBody(t *testing.T) {
	const sentinel = "SENSITIVE-UPSTREAM-BODY"
	fake := newLarkFake(t)
	fake.stubToken("tok", 7200)
	fake.mux.HandleFunc("/open-apis/docs_ai/v1/documents/doc-token/fetch", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, sentinel)
	})
	client := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL()})
	_, err := client.FetchDocument(context.Background(), testCreds(), DocumentFetchParams{Token: "doc-token", Format: "xml"})
	if err == nil || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("unsafe error = %v", err)
	}
}

type documentRoundTripper func(*http.Request) (*http.Response, error)

func (f documentRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func documentJSONResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
