package kagentbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/pkg/kagent"
)

const testID = "11111111-1111-4111-8111-111111111111"
const testUID = "22222222-2222-4222-8222-222222222222"
const testOwner = "33333333-3333-4333-8333-333333333333"
const testTask = "44444444-4444-4444-8444-444444444444"
const testContext = "55555555-5555-4555-8555-555555555555"

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type reviewFunc func(context.Context, string, string) (Identity, error)

func (f reviewFunc) Review(c context.Context, t, a string) (Identity, error) { return f(c, t, a) }
func testPolicy() Policy {
	return Policy{Namespace: "dev", AgentID: testID, AgentName: "demo", Harness: "qualified-harness", Model: "qualified-model", ExpectedUID: testUID, OwnerToken: testOwner, Audience: "broker-audience", ServiceAccountUsername: "system:serviceaccount:dev:caller", ServiceAccountUID: "caller-uid"}
}
func agent(p Policy) *kagent.Agent {
	return &kagent.Agent{APIVersion: kagent.GroupVersionAPI, Kind: kagent.KindAgent, Metadata: kagent.ObjectMeta{Name: p.name(), Namespace: p.Namespace, UID: p.ExpectedUID, ResourceVersion: "17", Labels: map[string]string{managedBy: "multica", agentIDLabel: p.AgentID}, Annotations: map[string]string{ownerAnnotation: p.OwnerToken, nameAnnotation: p.AgentName, revisionAnnotation: "revision"}}, Spec: kagent.AgentSpec{HarnessRef: &kagent.LocalObjectReference{Name: p.Harness}, Template: &kagent.AgentTemplateSpec{ModelConfig: &kagent.LocalObjectReference{Name: p.Model}, SystemPrompt: "Be helpful"}}}
}
func asMap(v any) map[string]any { b, _ := json.Marshal(v); m, _ := parseObject(b); return m }
func reply(v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{StatusCode: 200, Header: http.Header{"X-Upstream-Secret": []string{"hidden"}}, Body: io.NopCloser(bytes.NewReader(b))}
}

type fixture struct {
	callsMu   sync.Mutex
	b         *Broker
	kube, a2a int
	observed  map[string]any
	upstream  func(*http.Request) (*http.Response, error)
}

func setup(t *testing.T) *fixture {
	t.Helper()
	p := testPolicy()
	f := &fixture{observed: asMap(agent(p))}
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		want := "kubernetes-credential"
		if r.URL.Host == "a2a.invalid" {
			want = "gateway-credential"
		}
		if r.Header.Get("Authorization") != "Bearer "+want {
			t.Error("wrong upstream credential")
		}
		for _, k := range []string{InnerTokenHeader, "Cookie", "Forwarded", "X-Forwarded-Host", "X-Remote-User", "Impersonate-User", "X-Custom"} {
			if r.Header.Get(k) != "" {
				t.Errorf("forwarded forbidden header %s", k)
			}
		}
		if r.Host != "cluster.invalid" && r.Host != "a2a.invalid" {
			t.Error("caller authority forwarded")
		}
		if r.URL.Host == "cluster.invalid" {
			f.callsMu.Lock()
			f.kube++
			f.callsMu.Unlock()
			return reply(f.observed), nil
		}
		f.callsMu.Lock()
		f.a2a++
		f.callsMu.Unlock()
		if f.upstream != nil {
			return f.upstream(r)
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		task := map[string]any{"id": testTask, "contextId": testContext, "status": map[string]any{"state": "TASK_STATE_WORKING"}}
		var result any = task
		if req["method"] == "SendMessage" {
			result = map[string]any{"task": task}
		}
		return reply(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": result}), nil
	})}
	journal, err := OpenAttemptJournal(filepath.Join(t.TempDir(), "attempts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	ownership := testOwnership(t, p, true)
	b, err := New(Config{Ownership: ownership, Attempts: journal, Policy: p, Reviewer: reviewFunc(func(_ context.Context, tok, a string) (Identity, error) {
		if tok != "caller-credential" || a != p.Audience {
			return Identity{}, errDenied
		}
		return Identity{p.ServiceAccountUsername, p.ServiceAccountUID}, nil
	}), Kubernetes: Endpoint{URL: "https://cluster.invalid", Credential: "kubernetes-credential", Client: client}, A2A: Endpoint{URL: "https://a2a.invalid", Credential: "gateway-credential", Client: client}})
	if err != nil {
		t.Fatal(err)
	}
	f.b = b
	return f
}
func serve(b *Broker, method, path, body string, modify func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set(InnerTokenHeader, "caller-credential")
	if method != "GET" {
		r.Header.Set("Content-Type", "application/json")
	}
	if method == "PATCH" {
		r.Header.Set("Content-Type", "application/merge-patch+json")
	}
	if modify != nil {
		modify(r)
	}
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	return w
}
func sendBody() string {
	return `{"jsonrpc":"2.0","id":"` + testID + `","method":"SendMessage","params":{"message":{"messageId":"` + testOwner + `","role":"ROLE_USER","parts":[{"text":"hello"}]},"configuration":{"returnImmediately":true,"acceptedOutputModes":["text/plain"]}}}`
}
func TestExactRouteDenials(t *testing.T) {
	p := testPolicy()
	for _, path := range []string{p.resource() + "?", p.resource() + "?watch=true", p.collection(), p.resource() + "/", strings.Replace(p.resource(), "/agents/", "/agents%2f", 1), p.resource() + "/../other", p.resource() + "%5c", p.resource() + "%252f", strings.Replace(p.resource(), "/dev/", "/other/", 1), strings.Replace(p.resource(), testID, testUID, 1), p.resource() + "/status"} {
		t.Run(path, func(t *testing.T) {
			f := setup(t)
			w := serve(f.b, "GET", path, "", nil)
			if w.Code < 400 || f.kube+f.a2a != 0 {
				t.Fatalf("accepted route %s: %d", path, w.Code)
			}
		})
	}
	f := setup(t)
	for _, method := range []string{"PUT", "HEAD", "OPTIONS", "CONNECT", "TRACE"} {
		if w := serve(f.b, method, p.resource(), "", nil); w.Code < 400 {
			t.Fatal("accepted method", method)
		}
	}
}
func TestAuthenticationDenials(t *testing.T) {
	for _, modify := range []func(*http.Request){func(r *http.Request) { r.Header.Del(InnerTokenHeader) }, func(r *http.Request) { r.Header.Add(InnerTokenHeader, "second") }, func(r *http.Request) { r.Header.Set(InnerTokenHeader, "one,two") }, func(r *http.Request) { r.Header["x-kagent-token"] = []string{"second"} }, func(r *http.Request) { r.Header.Set(InnerTokenHeader, "bad token") }} {
		f := setup(t)
		if w := serve(f.b, "GET", f.b.policy.resource(), "", modify); w.Code != 401 || f.kube != 0 {
			t.Fatal("header identity accepted")
		}
	}
	for _, id := range []Identity{{}, {"system:serviceaccount:dev:other", "caller-uid"}, {testPolicy().ServiceAccountUsername, "replacement-uid"}} {
		f := setup(t)
		f.b.reviewer = reviewFunc(func(context.Context, string, string) (Identity, error) { return id, nil })
		if w := serve(f.b, "GET", f.b.policy.resource(), "", nil); w.Code != 401 || f.kube != 0 {
			t.Fatal("wrong identity accepted")
		}
	}
	f := setup(t)
	f.b.reviewer = reviewFunc(func(context.Context, string, string) (Identity, error) {
		return Identity{}, errors.New("caller-credential")
	})
	w := serve(f.b, "GET", f.b.policy.resource(), "", nil)
	if w.Code != 401 || strings.Contains(w.Body.String(), "caller-credential") {
		t.Fatal("error reflected")
	}
}
func TestStrictJSONAndA2A(t *testing.T) {
	body := sendBody()
	bodies := []string{body + ` {}`, strings.Replace(body, `"jsonrpc":"2.0"`, `"jsonrpc":"2.0","jsonrpc":"2.0"`, 1), strings.Replace(body, `"text":"hello"`, `"text":"hello","text":"other"`, 1), strings.Replace(body, `"text":"hello"`, `"Text":"hello"`, 1), strings.Replace(body, `"text":"hello"`, `"text":"hello","file":{}`, 1), strings.Replace(body, `"role":"ROLE_USER"`, `"role":"ROLE_USER","metadata":{}`, 1), strings.Replace(body, `"SendMessage"`, `"SendStreamingMessage"`, 1), strings.Replace(body, `"messageId":"`+testOwner+`"`, `"messageId":"invalid"`, 1), strings.Replace(body, `"configuration":{`, `"configuration":{"pushNotificationConfig":{},`, 1), strings.Repeat(" ", bodyLimit) + body, `null`, `[]`, strings.Replace(body, `"role":"ROLE_USER"`, `"contextId":"bad","role":"ROLE_USER"`, 1)}
	for i, body := range bodies {
		f := setup(t)
		w := serve(f.b, "POST", f.b.policy.a2a(), body, nil)
		if w.Code != 400 || f.kube+f.a2a != 0 {
			t.Fatalf("case %d: status %d upstream %d", i, w.Code, f.kube+f.a2a)
		}
	}
	f := setup(t)
	w := serve(f.b, "POST", f.b.policy.a2a(), body, func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") })
	if w.Code != 400 {
		t.Fatal("encoded body accepted")
	}
}
func TestObservedDriftFailsClosed(t *testing.T) {
	for _, mutate := range []func(map[string]any){
		func(m map[string]any) { object(m["metadata"])["uid"] = testOwner },
		func(m map[string]any) { object(object(m["metadata"])["annotations"])[ownerAnnotation] = "other" },
		func(m map[string]any) { object(object(m["spec"])["template"])["tools"] = []any{} },
		func(m map[string]any) {
			object(object(m["spec"])["template"])["systemPromptFrom"] = map[string]any{"name": "secret", "key": "key"}
		},
		func(m map[string]any) { object(m["spec"])["harnessRef"] = map[string]any{"name": "other"} },
		func(m map[string]any) { object(m["metadata"])["deletionTimestamp"] = "now" },
		func(m map[string]any) { object(m["spec"])["MCP"] = nil },
	} {
		f := setup(t)
		mutate(f.observed)
		w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil)
		if w.Code != 502 || f.a2a != 0 {
			t.Fatal("drift accepted")
		}
	}
}
func TestRealClientShapesAndHeaderStripping(t *testing.T) {
	f := setup(t)
	transport := roundTrip(func(r *http.Request) (*http.Response, error) {
		r.RequestURI = r.URL.RequestURI()
		r.Host = "untrusted.invalid"
		r.Header.Set(InnerTokenHeader, "caller-credential")
		for _, k := range []string{"Cookie", "Forwarded", "X-Forwarded-Host", "X-Remote-User", "Impersonate-User", "X-Custom"} {
			r.Header.Set(k, "attacker")
		}
		w := httptest.NewRecorder()
		f.b.ServeHTTP(w, r)
		if w.Header().Get("X-Upstream-Secret") != "" {
			t.Error("upstream headers reflected")
		}
		return w.Result(), nil
	})
	config := kagent.HTTPConfig{Endpoint: "https://broker.invalid", Client: &http.Client{Transport: transport}}
	store, err := kagent.NewKubernetesStore(config, []string{"dev"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Get(context.Background(), "dev", f.b.policy.name()); err != nil {
		t.Fatal(err)
	}
	a := agent(f.b.policy)
	a.Metadata.UID = ""
	a.Metadata.ResourceVersion = ""
	// A real create includes status:{}; a different committed digest is fenced.
	assert403 := func(err error) {
		t.Helper()
		var e *kagent.HTTPError
		if !errors.As(err, &e) || e.Status != 403 {
			t.Fatalf("wanted disabled write: %v", err)
		}
	}
	if err := store.Create(context.Background(), a); !errors.Is(err, kagent.ErrConflict) {
		t.Fatalf("expected fenced create: %v", err)
	}
	a = agent(f.b.policy)
	if err := store.Update(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	assert403(store.Delete(context.Background(), "dev", a.Metadata.Name, kagent.DeletePreconditions{UID: testUID, ResourceVersion: "17"}))
	if f.kube != 3 {
		t.Fatal("unexpected upstream call count", f.kube)
	}
	client, err := kagent.NewA2AClient(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Get(context.Background(), "dev", a.Metadata.Name, testTask, testContext); err == nil {
		t.Fatal("unbound task accepted")
	}
	if _, err = client.Send(context.Background(), "dev", a.Metadata.Name, testOwner, "", "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Get(context.Background(), "dev", a.Metadata.Name, testTask, testContext); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Cancel(context.Background(), "dev", a.Metadata.Name, testTask, testContext); err != nil {
		t.Fatal(err)
	}
	if f.a2a != 3 {
		t.Fatalf("A2A requests=%d", f.a2a)
	}
	// A new broker has no authority over old task IDs.
	fresh := setup(t)
	get := `{"jsonrpc":"2.0","id":"` + testID + `","method":"GetTask","params":{"id":"` + testTask + `"}}`
	if w := serve(fresh.b, "POST", fresh.b.policy.a2a(), get, nil); w.Code != 403 || fresh.a2a != 0 {
		t.Fatal("task adopted on restart")
	}
}
func TestMutationShapeDenials(t *testing.T) {
	p := testPolicy()
	a := asMap(agent(p))
	delete(object(a["metadata"]), "uid")
	delete(object(a["metadata"]), "resourceVersion")
	object(a["status"])["conditions"] = []any{}
	raw, _ := json.Marshal(a)
	f := setup(t)
	if w := serve(f.b, "POST", p.collection(), string(raw), nil); w.Code != 400 {
		t.Fatal("create status accepted")
	}
	for _, body := range []string{`{"metadata":{"uid":"` + testUID + `"},"spec":{}}`, `{"metadata":{"uid":"` + testUID + `","resourceVersion":"1"},"spec":{},"status":{}}`} {
		if w := serve(f.b, "PATCH", p.resource(), body, nil); w.Code != 400 {
			t.Fatal("bad patch accepted")
		}
	}
	for _, body := range []string{`{"apiVersion":"v1","kind":"DeleteOptions","preconditions":{"uid":"` + testUID + `"}}`, `{"apiVersion":"v1","kind":"DeleteOptions","preconditions":{"uid":"` + testOwner + `","resourceVersion":"1"}}`} {
		if w := serve(f.b, "DELETE", p.resource(), body, nil); w.Code != 400 {
			t.Fatal("bad delete accepted")
		}
	}
	if f.kube+f.a2a != 0 {
		t.Fatal("mutation reached upstream")
	}
}

func TestUpstreamFailuresAndCredentialReflection(t *testing.T) {
	for _, up := range []func(*http.Request) (*http.Response, error){
		func(*http.Request) (*http.Response, error) { return nil, errors.New("caller-credential") },
		func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.invalid"}}, Body: io.NopCloser(strings.NewReader("secret"))}, nil
		},
		func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", responseLimit+1)))}, nil
		},
		func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":1,"id":2}`))}, nil
		},
		func(r *http.Request) (*http.Response, error) {
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			return reply(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{"task": map[string]any{"id": testTask, "contextId": testContext, "status": map[string]any{"state": "TASK_STATE_WORKING", "message": map[string]any{"messageId": testID, "role": "ROLE_AGENT", "parts": []any{map[string]any{"text": "caller-credential gateway-credential"}}}}}}}), nil
		},
	} {
		f := setup(t)
		f.upstream = up
		w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil)
		if w.Code != 502 || strings.Contains(w.Body.String(), "credential") {
			t.Fatalf("unsafe response: %d %s", w.Code, w.Body.String())
		}
	}
}

func FuzzParseObject(f *testing.F) {
	for _, s := range []string{`{}`, `{"a":1,"a":2}`, `{"nested":{"a":1,"a":2}}`, `{"x":[null,true,{}]}`, sendBody()} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > bodyLimit {
			return
		}
		m, err := parseObject([]byte(s))
		if err == nil {
			raw, e := json.Marshal(m)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = parseObject(raw); e != nil {
				t.Fatal(e)
			}
			_, _ = rpc(m)
		}
	})
}
