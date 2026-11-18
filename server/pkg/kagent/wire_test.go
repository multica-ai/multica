//go:build !windows

package kagent

// Real HTTP transport conformance + lifecycle tests for the kagent slice.
// Requests run through in-process HTTP handlers exercising the real wireClient,
// KubernetesStore REST payloads and A2A JSON-RPC envelopes. No live cluster,
// no external network.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const testNS = "dev"

// --- fake Kubernetes REST API server ---------------------------------------

type fakeKube struct {
	t       *testing.T
	mu      sync.Mutex
	objs    map[string]map[string]any // ns/name -> generic object (unknown fields preserved)
	methods []string                  // recorded "METHOD /path"
}

func newFakeKube() *fakeKube { return &fakeKube{t: nil, objs: map[string]map[string]any{}} }

func (f *fakeKube) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.methods = append(f.methods, r.Method+" "+r.URL.Path)
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		// apis/api.kagent.dev/v1alpha3/namespaces/{ns}/agents[/{name}]
		if (len(parts) != 6 && len(parts) != 7) || parts[0] != "apis" || parts[3] != "namespaces" || parts[5] != "agents" {
			http.Error(w, `{"reason":"NotFound"}`, http.StatusNotFound)
			return
		}
		ns := parts[4]
		key := ns
		if len(parts) == 7 {
			key += "/" + parts[6]
		}
		single := len(parts) == 7

		switch r.Method {
		case http.MethodPost:
			var obj map[string]any
			if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
				http.Error(w, `{"message":"bad body"}`, http.StatusBadRequest)
				return
			}
			meta := obj["metadata"].(map[string]any)
			key = ns + "/" + meta["name"].(string)
			if _, exists := f.objs[key]; exists {
				http.Error(w, `{"reason":"Conflict"}`, http.StatusConflict)
				return
			}
			meta["uid"] = "uid-1"
			meta["resourceVersion"] = "1"
			f.objs[key] = obj
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, obj)

		case http.MethodGet:
			if single {
				obj, ok := f.objs[key]
				if !ok {
					http.Error(w, `{"reason":"NotFound"}`, http.StatusNotFound)
					return
				}
				writeJSON(w, obj)
				return
			}
			items := []any{}
			for k, o := range f.objs {
				if strings.HasPrefix(k, ns+"/") {
					items = append(items, o)
				}
			}
			writeJSON(w, map[string]any{"items": items, "metadata": map[string]any{"continue": ""}})

		case http.MethodPatch:
			var patch map[string]any
			if ct := r.Header.Get("Content-Type"); ct != "application/merge-patch+json" {
				http.Error(w, `{"message":"unsupported media type"}`, http.StatusUnsupportedMediaType)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				http.Error(w, `{"message":"bad patch"}`, http.StatusBadRequest)
				return
			}
			obj, ok := f.objs[key]
			if !ok {
				http.Error(w, `{"reason":"NotFound"}`, http.StatusNotFound)
				return
			}
			mergePatch(obj, patch)
			obj["metadata"].(map[string]any)["resourceVersion"] = "2"
			writeJSON(w, obj)

		case http.MethodDelete:
			var opts struct {
				Preconditions map[string]string `json:"preconditions"`
			}
			_ = json.NewDecoder(r.Body).Decode(&opts)
			obj, ok := f.objs[key]
			if !ok {
				http.Error(w, `{"reason":"NotFound"}`, http.StatusNotFound)
				return
			}
			meta := obj["metadata"].(map[string]any)
			if opts.Preconditions["uid"] != meta["uid"] || opts.Preconditions["resourceVersion"] != meta["resourceVersion"] {
				http.Error(w, `{"reason":"Conflict"}`, http.StatusConflict)
				return
			}
			delete(f.objs, key)
			writeJSON(w, obj)

		default:
			http.Error(w, `{"message":"method"}`, http.StatusMethodNotAllowed)
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// mergePatch implements RFC 7386 merge-patch (the subset the store uses: nil
// deletes, maps merge recursively, scalars replace).
func mergePatch(dst map[string]any, patch map[string]any) {
	for k, v := range patch {
		if v == nil {
			delete(dst, k)
			continue
		}
		if pm, ok := v.(map[string]any); ok {
			dm, _ := dst[k].(map[string]any)
			if dm == nil {
				dm = map[string]any{}
			}
			mergePatch(dm, pm)
			dst[k] = dm
			continue
		}
		dst[k] = v
	}
}

func kubeStore(t *testing.T, f *fakeKube) *KubernetesStore {
	t.Helper()
	s, err := NewKubernetesStore(HTTPConfig{AllowInsecureLoopback: true, Endpoint: "http://localhost", Client: newContractServer(f.handler()).Client(), Token: func(context.Context) (string, error) { return "tok", nil }}, []string{testNS})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (f *fakeKube) serve(t *testing.T) string {
	srv := newContractServer(f.handler())
	t.Cleanup(srv.Close)
	return srv.URL
}

func validAgent(name string) *Agent {
	return &Agent{
		APIVersion: GroupVersionAPI, Kind: KindAgent,
		Metadata: ObjectMeta{Name: name, Namespace: testNS, UID: "uid-1", ResourceVersion: "1",
			Labels:      map[string]string{LabelManagedBy: LabelManagedByValue, LabelAgentID: "x"},
			Annotations: map[string]string{AnnotationOwnerToken: "t", AnnotationAgentName: "n", AnnotationDesiredRevision: "r"}},
		Spec: AgentSpec{HarnessRef: &LocalObjectReference{Name: "h"},
			Template: &AgentTemplateSpec{Description: "d", SystemPrompt: "sp"}},
	}
}

// Test 1: PATCH must be a merge-patch touching only owned leaves and must
// adopt the returned RV/UID; unmodeled spec fields survive server-side.
func TestKubernetesUpdateMergePatchPreservesUnknownFields(t *testing.T) {
	f := newFakeKube()
	s := kubeStore(t, f)

	// Seed: resource already on cluster with unmodeled fields (tools/skills)
	// as if written by another component. Store must not prune them.
	seed := map[string]any{
		"apiVersion": GroupVersionAPI, "kind": KindAgent,
		"metadata": map[string]any{"name": "multica-agent-11111111-1111-1111-1111-111111111111",
			"namespace": testNS, "uid": "uid-1", "resourceVersion": "1",
			"labels":      map[string]string{LabelManagedBy: LabelManagedByValue, LabelAgentID: "x"},
			"annotations": map[string]string{AnnotationOwnerToken: "t"}},
		"spec": map[string]any{
			"template": map[string]any{"description": "old", "tools": []string{"t1"}, "skills": []string{"s1"}},
		},
	}
	f.objs[testNS+"/multica-agent-11111111-1111-1111-1111-111111111111"] = seed

	a := validAgent("multica-agent-11111111-1111-1111-1111-111111111111")
	a.Metadata.UID = "uid-1"
	if err := s.Update(context.Background(), a); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if a.Metadata.UID != "uid-1" || a.Metadata.ResourceVersion != "2" {
		t.Fatalf("returned RV/UID not adopted: uid=%q rv=%q", a.Metadata.UID, a.Metadata.ResourceVersion)
	}
	// Server side: unmodeled fields survived the PATCH.
	got := f.objs[testNS+"/multica-agent-11111111-1111-1111-1111-111111111111"]
	tmpl := got["spec"].(map[string]any)["template"].(map[string]any)
	if _, ok := tmpl["tools"]; !ok {
		t.Fatal("merge patch pruned spec.template.tools server-side")
	}
	if _, ok := tmpl["skills"]; !ok {
		t.Fatal("merge patch pruned spec.template.skills server-side")
	}
	if tmpl["description"] != "d" || tmpl["systemPrompt"] != "sp" {
		t.Fatalf("owned leaves not updated: %v", tmpl)
	}
}

// Test 2: exact PATCH payload shape (owned leaves only, nulls for exactly-one
// counterparts, empty description -> explicit null).
func TestKubernetesPatchPayloadConformance(t *testing.T) {
	var payload map[string]any
	var contentType string
	srv := newContractServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&payload)
		writeJSON(w, validAgent("multica-agent-22222222-2222-2222-2222-222222222222"))
	}))
	defer srv.Close()
	s, err := NewKubernetesStore(HTTPConfig{AllowInsecureLoopback: true, Endpoint: srv.URL, Client: srv.Client()}, []string{testNS})
	if err != nil {
		t.Fatal(err)
	}
	a := validAgent("multica-agent-22222222-2222-2222-2222-222222222222")
	a.Spec.Template = &AgentTemplateSpec{ModelConfig: &LocalObjectReference{Name: "m"}, SystemPromptFrom: &ConfigMapKeyReference{Name: "cm", Key: "k"}}
	if err := s.Update(context.Background(), a); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if contentType != "application/merge-patch+json" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	meta := payload["metadata"].(map[string]any)
	if meta["uid"] != "uid-1" || meta["resourceVersion"] != "1" {
		t.Fatalf("metadata preconditions missing: %v", meta)
	}
	if lbls := meta["labels"].(map[string]any); len(lbls) != 2 {
		t.Fatalf("foreign labels would be pruned: %v", lbls)
	}
	spec := payload["spec"].(map[string]any)
	if v, ok := spec["harness"]; !ok || v != nil {
		t.Fatalf("spec.harness must be explicit null, got %v", v)
	}
	if v, ok := spec["templateRef"]; !ok || v != nil {
		t.Fatalf("spec.templateRef must be explicit null, got %v", v)
	}
	tmpl := spec["template"].(map[string]any)
	want := []string{"description", "modelConfig", "systemPrompt", "systemPromptFrom"}
	if len(tmpl) != len(want) {
		t.Fatalf("template leaves = %v, want exactly %v", tmpl, want)
	}
	if v, ok := tmpl["description"]; !ok || v != nil {
		t.Fatalf("empty description must be explicit null, got %v", v)
	}
	if value, ok := tmpl["systemPrompt"]; !ok || value != nil {
		t.Fatal("systemPrompt must be explicitly cleared when systemPromptFrom set")
	}
}

// Test 3: create adopts server identity; delete carries UID+RV preconditions
// and maps 409/404 correctly; empty preconditions fail closed.
func TestKubernetesCreateAndDeletePreconditions(t *testing.T) {
	f := newFakeKube()
	s := kubeStore(t, f)
	ctx := context.Background()

	a := validAgent("multica-agent-33333333-3333-3333-3333-333333333333")
	if err := s.Create(ctx, a); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.Metadata.UID != "uid-1" || a.Metadata.ResourceVersion != "1" {
		t.Fatalf("create response identity not adopted: %+v", a.Metadata)
	}

	// Empty preconditions: caller error, no HTTP call.
	before := len(f.methods)
	if err := s.Delete(ctx, testNS, a.Metadata.Name, DeletePreconditions{}); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("empty preconditions: %v", err)
	}
	if len(f.methods) != before {
		t.Fatal("empty preconditions reached the server")
	}

	// Wrong UID => 409 => ErrPreconditionFailed.
	if err := s.Delete(ctx, testNS, a.Metadata.Name, DeletePreconditions{UID: "other", ResourceVersion: "1"}); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("uid mismatch: %v", err)
	}
	// Correct preconditions delete.
	if err := s.Delete(ctx, testNS, a.Metadata.Name, DeletePreconditions{UID: "uid-1", ResourceVersion: "1"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(ctx, testNS, a.Metadata.Name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete Get = %v, want ErrNotFound", err)
	}
	// Unknown name maps 404 -> ErrNotFound on Get too.
	if _, err := s.Get(ctx, testNS, "multica-agent-44444444-4444-4444-4444-444444444444"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing: %v", err)
	}
}

// Test 4: HTTP security — endpoint validation, redirects never followed,
// credentials never leak in errors, response size/shape limits enforced.
func TestHTTPSecurityAndRedaction(t *testing.T) {
	ctx := context.Background()
	secret := "super-secret-token"

	for _, ep := range []string{"not-a-url", "ftp://h/p", "https://user:pw@h/p", "https://h/p?x=1", "https://h/#frag"} {
		if _, err := NewKubernetesStore(HTTPConfig{AllowInsecureLoopback: true, Endpoint: ep}, []string{testNS}); err == nil {
			t.Fatalf("endpoint %q accepted", ep)
		}
	}
	srv := newContractServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("missing Accept header")
		}
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Errorf("bad Authorization: %q", r.Header.Get("Authorization"))
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/redirect"):
			w.Header().Set("Location", "/target")
			w.WriteHeader(http.StatusFound)
		case strings.HasSuffix(r.URL.Path, "/big"):
			w.Write(bytes.Repeat([]byte("x"), 8<<20+10))
		case strings.HasSuffix(r.URL.Path, "/malformed"):
			w.Write([]byte("<html>not json</html>"))
		default:
			writeJSON(w, validAgent("multica-agent-55555555-5555-5555-5555-555555555555"))
		}
	}))
	defer srv.Close()

	mk := func(ep string) *KubernetesStore {
		s, err := NewKubernetesStore(HTTPConfig{AllowInsecureLoopback: true, Endpoint: ep, Client: srv.Client(), Token: func(context.Context) (string, error) { return secret, nil }}, []string{testNS})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := mk(srv.URL)

	// 302 must surface as HTTPError 302, never followed.
	_, err := s.Get(ctx, testNS, "redirect")
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusFound {
		t.Fatalf("redirect: %v, want HTTPError 302", err)
	}

	if _, err := s.Get(ctx, testNS, "big"); err == nil || !strings.Contains(err.Error(), "oversized") {
		t.Fatalf("oversized: %v", err)
	}
	if _, err := s.Get(ctx, testNS, "malformed"); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("malformed: %v", err)
	}

	// Transport failure must not leak the credential.
	dead := mk("http://127.0.0.1:1")
	_, err = dead.Get(ctx, testNS, "multica-agent-66666666-6666-6666-6666-666666666666")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("credential leak in transport error: %v", err)
	}

	// Token provider failure is reported without retrying.
	if _, err := mkErr().Get(ctx, testNS, "x-y"); err == nil || !strings.Contains(err.Error(), "obtain credential") {
		t.Fatalf("token error: %v", err)
	}
}

func mkErr() *KubernetesStore {
	s, _ := NewKubernetesStore(HTTPConfig{AllowInsecureLoopback: true, Endpoint: "http://127.0.0.1:1", Token: func(context.Context) (string, error) { return "", errors.New("no") }}, []string{testNS})
	return s
}

// --- A2A JSON-RPC -----------------------------------------------------------

type a2aReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func rpcServer(t *testing.T, check func(a2aReq), reply any) *contractServer {
	return newContractServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req a2aReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		check(req)
		if envelope, ok := reply.(map[string]any); ok && envelope["id"] != "wrong" {
			envelope["id"] = req.ID
		}
		writeJSON(w, reply)
	}))
}

func task(id, ctxID, state, text string) map[string]any {
	return map[string]any{"id": id, "contextId": ctxID,
		"status":    map[string]any{"state": state, "message": map[string]any{"messageId": "m", "role": "ROLE_AGENT", "parts": []map[string]any{{"text": text}}}},
		"artifacts": []map[string]any{{"parts": []map[string]any{{"text": text}}}}}
}

func TestA2AJSONRPCSendEnvelopeAndResult(t *testing.T) {
	var reqID string
	srv2 := newContractServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req a2aReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		reqID = req.ID
		if req.JSONRPC != "2.0" || req.Method != "SendMessage" {
			t.Errorf("envelope: %+v", req)
		}
		var p struct {
			Message struct {
				MessageID string `json:"messageId"`
				ContextID string `json:"contextId"`
				Role      string `json:"role"`
				Parts     []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"message"`
			Configuration struct {
				ReturnImmediately   bool     `json:"returnImmediately"`
				AcceptedOutputModes []string `json:"acceptedOutputModes"`
			} `json:"configuration"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			t.Errorf("params: %v", err)
		}
		if p.Message.Role != "ROLE_USER" || p.Message.ContextID != "11111111-1111-1111-1111-111111111111" {
			t.Errorf("message identity: %+v", p.Message)
		}
		if len(p.Message.Parts) != 1 || p.Message.Parts[0].Text != "hello" {
			t.Errorf("parts: %+v", p.Message.Parts)
		}
		if !p.Configuration.ReturnImmediately || p.Configuration.AcceptedOutputModes[0] != "text/plain" {
			t.Errorf("configuration: %+v", p.Configuration)
		}
		writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": req.ID,
			"result": map[string]any{"task": task("task-1", "11111111-1111-1111-1111-111111111111", StateCompleted, "hi")}})
	}))
	defer srv2.Close()

	c, err := NewA2AClient(HTTPConfig{AllowInsecureLoopback: true, Endpoint: srv2.URL, Client: srv2.Client()})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := c.Send(context.Background(), testNS, "multica-agent-77777777-7777-7777-7777-777777777777",
		"99999999-9999-9999-9999-999999999999", "11111111-1111-1111-1111-111111111111", "hello")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if tk.ID != "task-1" || !tk.Terminal() || tk.Text() != "hi" {
		t.Fatalf("task: %+v text=%q", tk, tk.Text())
	}
	if reqID == "" {
		t.Fatal("request id empty")
	}

	// Mismatched id => protocol error.
	badID := rpcServer(t, func(a2aReq) {}, map[string]any{"jsonrpc": "2.0", "id": "wrong", "result": map[string]any{"task": task("t", "11111111-1111-1111-1111-111111111111", StateCompleted, "x")}})
	defer badID.Close()
	c2, _ := NewA2AClient(HTTPConfig{AllowInsecureLoopback: true, Endpoint: badID.URL, Client: badID.Client()})
	if _, err := c2.Send(context.Background(), testNS, "multica-agent-77777777-7777-7777-7777-777777777777",
		"99999999-9999-9999-9999-999999999999", "11111111-1111-1111-1111-111111111111", "x"); err == nil || !strings.Contains(err.Error(), "mismatched") {
		t.Fatalf("id mismatch: %v", err)
	}

	// JSON-RPC error envelope => RPCError.
	errSrv := rpcServer(t, func(a2aReq) {}, map[string]any{"jsonrpc": "2.0", "id": "z", "error": map[string]any{"code": 13, "message": "no"}})
	defer errSrv.Close()
	c3, _ := NewA2AClient(HTTPConfig{AllowInsecureLoopback: true, Endpoint: errSrv.URL, Client: errSrv.Client()})
	_, err = c3.Send(context.Background(), testNS, "multica-agent-77777777-7777-7777-7777-777777777777",
		"99999999-9999-9999-9999-999999999999", "11111111-1111-1111-1111-111111111111", "x")
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Code != 13 {
		t.Fatalf("error envelope: %v", err)
	}

	// Result carrying a message instead of a task => fail closed.
	msgSrv := rpcServer(t, func(a2aReq) {}, map[string]any{"jsonrpc": "2.0", "id": "z", "result": map[string]any{"message": map[string]any{}}})
	defer msgSrv.Close()
	c4, _ := NewA2AClient(HTTPConfig{AllowInsecureLoopback: true, Endpoint: msgSrv.URL, Client: msgSrv.Client()})
	if _, err := c4.Send(context.Background(), testNS, "multica-agent-77777777-7777-7777-7777-777777777777",
		"99999999-9999-9999-9999-999999999999", "11111111-1111-1111-1111-111111111111", "x"); err == nil || !strings.Contains(err.Error(), "task response required") {
		t.Fatalf("message result: %v", err)
	}

	// Session identity must not change.
	swSrv := rpcServer(t, func(a2aReq) {}, map[string]any{"jsonrpc": "2.0", "id": "z",
		"result": map[string]any{"task": task("task-1", "22222222-2222-2222-2222-222222222222", StateWorking, "x")}})
	defer swSrv.Close()
	c5, _ := NewA2AClient(HTTPConfig{AllowInsecureLoopback: true, Endpoint: swSrv.URL, Client: swSrv.Client()})
	if _, err := c5.Send(context.Background(), testNS, "multica-agent-77777777-7777-7777-7777-777777777777",
		"99999999-9999-9999-9999-999999999999", "11111111-1111-1111-1111-111111111111", "x"); err == nil || !strings.Contains(err.Error(), "session identity changed") {
		t.Fatalf("session drift: %v", err)
	}

	// Unknown remote state fails closed.
	usSrv := rpcServer(t, func(a2aReq) {}, map[string]any{"jsonrpc": "2.0", "id": "z",
		"result": map[string]any{"task": task("task-1", "11111111-1111-1111-1111-111111111111", "TASK_STATE_WEIRD", "x")}})
	defer usSrv.Close()
	c6, _ := NewA2AClient(HTTPConfig{AllowInsecureLoopback: true, Endpoint: usSrv.URL, Client: usSrv.Client()})
	if _, err := c6.Send(context.Background(), testNS, "multica-agent-77777777-7777-7777-7777-777777777777",
		"99999999-9999-9999-9999-999999999999", "11111111-1111-1111-1111-111111111111", "x"); err == nil || !strings.Contains(err.Error(), "unsupported remote state") {
		t.Fatalf("unknown state: %v", err)
	}
}

func TestA2AGetAndCancelTasks(t *testing.T) {
	var method, paramID string
	srv := newContractServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req a2aReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		method = req.Method
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		paramID = p["id"]
		if req.Method == "SendMessage" {
			t.Errorf("unexpected method")
		}
		writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": req.ID,
			"result": task("task-9", "11111111-1111-1111-1111-111111111111", StateWorking, "x")})
	}))
	defer srv.Close()
	c, err := NewA2AClient(HTTPConfig{AllowInsecureLoopback: true, Endpoint: srv.URL, Client: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ns, name := testNS, "multica-agent-77777777-7777-7777-7777-777777777777"
	sess := "11111111-1111-1111-1111-111111111111"
	if _, err := c.Get(context.Background(), ns, name, "task-9", sess); err != nil || method != "GetTask" || paramID != "task-9" {
		t.Fatalf("Get: method=%q id=%q err=%v", method, paramID, err)
	}
	if _, err := c.Cancel(context.Background(), ns, name, "task-9", sess); err != nil || method != "CancelTask" {
		t.Fatalf("Cancel: %v", err)
	}
	// Task identity changed => fail closed.
	chSrv := rpcServer(t, func(a2aReq) {}, map[string]any{"jsonrpc": "2.0", "id": "k",
		"result": task("other", sess, StateWorking, "x")})
	defer chSrv.Close()
	c2, _ := NewA2AClient(HTTPConfig{AllowInsecureLoopback: true, Endpoint: chSrv.URL, Client: chSrv.Client()})
	if _, err := c2.Get(context.Background(), ns, name, "task-9", sess); err == nil || !strings.Contains(err.Error(), "task identity changed") {
		t.Fatalf("identity change: %v", err)
	}
	// Client-side validation fires before any request.
	if _, err := c.Get(context.Background(), ns, name, "", sess); !errors.Is(err, ErrInvalidDesired) {
		t.Fatalf("empty task id: %v", err)
	}
	if _, err := c.Send(context.Background(), ns, name, "bad-id", sess, "x"); !errors.Is(err, ErrInvalidDesired) {
		t.Fatalf("bad message id: %v", err)
	}
}

// --- Journal tombstone / restart --------------------------------------------

func TestLifecycleTombstoneAndRestart(t *testing.T) {
	f := newFakeKube()
	dir := filepath.Join(t.TempDir(), "journal")
	j, err := NewJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := New(kubeStore(t, f), Config{AllowedNamespaces: []string{testNS}})
	if err != nil {
		t.Fatal(err)
	}
	lc, err := NewLifecycle(rec, j)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := "aaaaaaaa-1111-4111-8111-111111111111"

	d := Desired{AgentID: id, AgentName: "demo", Namespace: testNS, HarnessRefName: "plat-harness",
		Inline: &AgentTemplateSpec{Description: "first"}}
	if err := lc.Enroll(d); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	b, err := lc.Acquire(ctx, id)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if b.Outcome.UID == "" || b.Outcome.Action != ActionCreated {
		t.Fatalf("binding: %+v", b)
	}
	uid := b.Outcome.UID
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	// Delete without drain fails closed.
	if err := lc.Delete(ctx, id, false); !errors.Is(err, ErrDrainRequired) {
		t.Fatalf("no drain: %v", err)
	}
	// Tombstone written before cleanup; resource deleted with preconditions.
	if err := lc.Delete(ctx, id, true); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := rec.store.Get(ctx, testNS, "multica-agent-"+id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("resource not deleted: %v", err)
	}

	// Deleted ID cannot be silently re-enrolled or acquired.
	if err := lc.Enroll(Desired{AgentID: id, AgentName: "n", Namespace: testNS, HarnessRefName: "h", Inline: &AgentTemplateSpec{Description: "d"}}); !errors.Is(err, ErrDeleted) {
		t.Fatalf("re-enroll: %v", err)
	}
	if _, err := lc.Acquire(ctx, id); !errors.Is(err, ErrDeleted) {
		t.Fatalf("acquire after delete: %v", err)
	}

	// Restart: a brand-new Journal over the same directory must still see the
	// tombstone (durable ownership + replay fence).
	j2, err := NewJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	lc2, err := NewLifecycle(rec, j2)
	if err != nil {
		t.Fatal(err)
	}
	if err := lc2.Enroll(Desired{AgentID: id, AgentName: "n", Namespace: testNS, HarnessRefName: "h", Inline: &AgentTemplateSpec{Description: "d"}}); !errors.Is(err, ErrDeleted) {
		t.Fatalf("re-enroll after restart: %v", err)
	}
	if _, err := lc2.Acquire(ctx, id); !errors.Is(err, ErrDeleted) {
		t.Fatalf("acquire after restart: %v", err)
	}
	_ = uid
}

func TestJournalLockIsExclusive(t *testing.T) {
	j, err := NewJournal(filepath.Join(t.TempDir(), "journal"))
	if err != nil {
		t.Fatal(err)
	}
	r1, err := j.Lock("k")
	if err != nil {
		t.Fatal(err)
	}
	defer r1.Close()
	if _, err := j.Lock("k"); !errors.Is(err, ErrBusy) {
		t.Fatalf("second lock: %v, want ErrBusy", err)
	}
}

// guard: namespace outside allowlist never reaches the wire.
func TestNamespaceAllowlistFailsClosed(t *testing.T) {
	f := newFakeKube()
	before := len(f.methods)
	s := kubeStore(t, f)
	if _, err := s.Get(context.Background(), "other", "multica-agent-88888888-8888-8888-8888-888888888888"); !errors.Is(err, ErrNamespaceNotAllowed) {
		t.Fatalf("Get: %v", err)
	}
	if len(f.methods) != before {
		t.Fatal("request left the allowlist")
	}
}
