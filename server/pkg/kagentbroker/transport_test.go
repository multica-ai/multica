package kagentbroker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
)

func TestTokenReviewContract(t *testing.T) {
	p := testPolicy()
	good := func() map[string]any {
		return map[string]any{"apiVersion": "authentication.k8s.io/v1", "kind": "TokenReview", "status": map[string]any{"authenticated": true, "audiences": []any{p.Audience}, "user": map[string]any{"username": p.ServiceAccountUsername, "uid": p.ServiceAccountUID}}}
	}
	var got int
	makeReviewer := func(output map[string]any, transportErr error) *TokenReviewer {
		t.Helper()
		r, err := NewTokenReviewer(Endpoint{URL: "https://cluster.invalid", Credential: "review-credential", Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			got++
			if r.Method != "POST" || r.URL.Path != "/apis/authentication.k8s.io/v1/tokenreviews" || r.Header.Get("Authorization") != "Bearer review-credential" || r.Header.Get(InnerTokenHeader) != "" {
				t.Error("unsafe TokenReview request")
			}
			raw, _ := io.ReadAll(r.Body)
			m, err := parseObject(raw)
			if err != nil {
				t.Fatal(err)
			}
			spec := object(m["spec"])
			a, _ := spec["audiences"].([]any)
			if spec["token"] != "caller-credential" || len(a) != 1 || a[0] != p.Audience || m["kind"] != "TokenReview" {
				t.Error("wrong token/audience contract")
			}
			if transportErr != nil {
				return nil, transportErr
			}
			return reply(output), nil
		})}})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	id, err := makeReviewer(good(), nil).Review(context.Background(), "caller-credential", p.Audience)
	if err != nil || id != (Identity{p.ServiceAccountUsername, p.ServiceAccountUID}) {
		t.Fatalf("valid review: %+v %v", id, err)
	}
	for _, mutate := range []func(map[string]any){
		func(m map[string]any) { object(m["status"])["authenticated"] = false },
		func(m map[string]any) { object(m["status"])["authenticated"] = "true" },
		func(m map[string]any) { object(m["status"])["error"] = "identity unavailable" },
		func(m map[string]any) { object(m["status"])["audiences"] = []any{} },
		func(m map[string]any) { object(m["status"])["audiences"] = []any{"other"} },
		func(m map[string]any) { object(m["status"])["audiences"] = []any{p.Audience, "extra"} },
		func(m map[string]any) { delete(object(object(m["status"])["user"]), "uid") },
		func(m map[string]any) { delete(m, "status") },
		func(m map[string]any) { m["kind"] = "Other" },
	} {
		m := good()
		mutate(m)
		if _, err := makeReviewer(m, nil).Review(context.Background(), "caller-credential", p.Audience); err == nil {
			t.Fatal("invalid TokenReview accepted")
		}
	}
	_, err = makeReviewer(nil, errors.New("caller-credential")).Review(context.Background(), "caller-credential", p.Audience)
	if err == nil || strings.Contains(err.Error(), "credential") {
		t.Fatal("TokenReview transport fail-open or reflection")
	}
	before := got
	if _, err := makeReviewer(good(), nil).Review(context.Background(), "bad,token", p.Audience); err == nil || before != got {
		t.Fatal("unsafe token reviewed")
	}
}

func TestConfigurationFailsClosed(t *testing.T) {
	for _, s := range []string{"http://localhost", "https://u:p@host", "https://host/path", "https://host?query", "https://host?", "https://host#fragment", "https://host#", "https://host/#", "https://host/%2f", "//host"} {
		if _, err := newEndpoint(Endpoint{URL: s, Credential: "secret"}); err == nil {
			t.Fatal("invalid endpoint accepted", s)
		}
	}
	for _, s := range []string{"", "has space", "line\nbreak", "a,b"} {
		if _, err := newEndpoint(Endpoint{URL: "https://host", Credential: s}); err == nil {
			t.Fatal("invalid credential accepted")
		}
	}
	for _, mutate := range []func(*Policy){func(p *Policy) { p.AgentID = "../escape" }, func(p *Policy) { p.ExpectedUID = "bad" }, func(p *Policy) { p.OwnerToken = "" }, func(p *Policy) { p.Namespace = "other/namespace" }, func(p *Policy) { p.ServiceAccountUID = "" }, func(p *Policy) { p.ServiceAccountUsername = "system:admin" }, func(p *Policy) { p.Audience = "" }, func(p *Policy) { p.Model = "" }, func(p *Policy) { p.Harness = "" }} {
		p := testPolicy()
		mutate(&p)
		if p.valid() {
			t.Fatal("invalid policy accepted")
		}
	}
	if _, err := New(Config{Policy: testPolicy()}); err == nil {
		t.Fatal("missing auth accepted")
	}
}

func TestRedirectNotFollowedAndResponsesBounded(t *testing.T) {
	calls := 0
	e, err := newEndpoint(Endpoint{URL: "https://cluster.invalid", Credential: "upstream-credential", Client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 307, Header: http.Header{"Location": []string{"https://attacker.invalid"}}, Body: io.NopCloser(strings.NewReader("upstream-credential"))}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.call(context.Background(), "POST", "/agents/dev/name", "application/json", map[string]string{"text": "hi"}); err == nil || calls != 1 {
		t.Fatal("redirect followed")
	}
	for _, raw := range []string{strings.Repeat("x", responseLimit+1), `{"status":{},"status":{}}`, `{} {}`, `<html>broken</html>`} {
		e.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, nil
		})
		if _, err = e.call(context.Background(), "GET", "/resource", "", nil); err == nil {
			t.Fatal("unsafe upstream response accepted")
		}
	}
}

func TestResponseContextAndTaskBinding(t *testing.T) {
	for _, kind := range []string{"context", "metadata", "file", "id"} {
		f := setup(t)
		if w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil); w.Code != 200 {
			t.Fatal(w.Code)
		}
		f.upstream = func(r *http.Request) (*http.Response, error) {
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			task := map[string]any{"id": testTask, "contextId": testContext, "status": map[string]any{"state": "TASK_STATE_COMPLETED"}}
			switch kind {
			case "context":
				task["contextId"] = testID
			case "id":
				task["id"] = "not-uuid"
			case "metadata":
				task["metadata"] = "invalid"
			case "file":
				object(task["status"])["message"] = map[string]any{"messageId": testID, "role": "ROLE_AGENT", "parts": []any{map[string]any{"file": map[string]any{}}}}
			}
			return reply(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{"task": task}}), nil
		}
		if w := serve(f.b, "POST", f.b.policy.a2a(), continuationBody(testID), nil); w.Code != 502 || len(f.b.tasks) != 1 {
			t.Fatal("invalid task binding accepted", kind)
		}
	}
}

func TestCapabilityAndDuplicateKeyDenials(t *testing.T) {
	p := testPolicy()
	for _, field := range []string{"tools", "skills", "mcp", "mcpServers", "refs", "promptFrom", "promptTemplate", "outputSchema", "plugins", "ModelConfig"} {
		f := setup(t)
		object(object(f.observed["spec"])["template"])[field] = nil
		if w := serve(f.b, "POST", p.a2a(), sendBody(), nil); w.Code != 502 || f.a2a != 0 {
			t.Fatal("capability accepted", field)
		}
	}
	for _, raw := range []string{`{"text":"a","\u0074ext":"b"}`, `{"metadata":{"labels":{"agent":"a","agent":"b"}}}`, `{"a":[{"x":1,"x":2}]}`} {
		if _, err := parseObject([]byte(raw)); err == nil {
			t.Fatal("duplicate accepted", raw)
		}
	}
	f := setup(t)
	object(object(f.observed["spec"])["template"])["modelConfig"] = map[string]any{"name": "other"}
	if w := serve(f.b, "POST", p.a2a(), sendBody(), nil); w.Code != 502 || f.a2a != 0 {
		t.Fatal("model override accepted")
	}
}

func TestOutboundCookiesAreDisabled(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse("https://cluster.invalid")
	jar.SetCookies(u, []*http.Cookie{{Name: "ambient-credential", Value: "secret"}})
	e, err := newEndpoint(Endpoint{URL: u.String(), Credential: "explicit-credential", Client: &http.Client{Jar: jar, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" {
			t.Error("ambient cookie forwarded")
		}
		return reply(map[string]any{}), nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.call(context.Background(), "GET", "/resource", "", nil); err != nil {
		t.Fatal(err)
	}
}
