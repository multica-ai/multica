package kagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKubernetesOwnedLeafPatchAndReturnedVersion(t *testing.T) {
	d := testDesired()
	name, _ := ResourceName(d.AgentID)
	var received map[string]any
	server := newContractServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rotated" {
			t.Error("credential missing")
		}
		if r.Method != "PATCH" || r.Header.Get("Content-Type") != "application/merge-patch+json" {
			t.Errorf("unexpected request %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		// A real Kubernetes patch response contains the post-write version and
		// controller status from the previous generation. It cannot count as ready.
		a := Agent{APIVersion: GroupVersionAPI, Kind: KindAgent, Metadata: ObjectMeta{Name: name, Namespace: d.Namespace, UID: "bound-uid", ResourceVersion: "rv-new", Generation: 2}, Spec: AgentSpec{Template: d.Inline}}
		a.Status.Conditions = []Condition{{Type: ConditionReady, Status: "True", ObservedGeneration: 1}}
		json.NewEncoder(w).Encode(a)
	}))
	defer server.Close()
	store, err := NewKubernetesStore(HTTPConfig{AllowInsecureLoopback: true, Endpoint: server.URL, Client: server.Client(), Token: func(context.Context) (string, error) { return "rotated", nil }}, []string{d.Namespace})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := New(store, testConfig())
	a := r.buildAgent(d, name, DesiredRevision(d))
	a.Metadata.UID = "bound-uid"
	a.Metadata.ResourceVersion = "rv-old"
	a.Metadata.Labels["platform-label"] = "preserve"
	a.Metadata.Annotations["platform-annotation"] = "preserve"
	if err = store.Update(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if a.Metadata.ResourceVersion != "rv-new" {
		t.Fatal("stale returned version")
	}
	if ready, _ := readiness(a); ready {
		t.Fatal("stale controller status reported ready")
	}
	if _, ok := received["status"]; ok {
		t.Fatal("patch wrote status")
	}
	spec := received["spec"].(map[string]any)
	if len(spec) != 4 {
		t.Fatalf("unexpected managed spec paths: %v", spec)
	}
	tmpl := spec["template"].(map[string]any)
	for _, field := range []string{"tools", "skills", "plugins", "outputSchema", "promptTemplate", "egressPolicyRefs"} {
		if _, ok := tmpl[field]; ok {
			t.Fatalf("patch would prune %s", field)
		}
	}
	if len(tmpl) != 4 || tmpl["systemPromptFrom"] != nil {
		t.Fatalf("managed leaves not cleared: %v", tmpl)
	}
	meta := received["metadata"].(map[string]any)
	if meta["uid"] != "bound-uid" || meta["resourceVersion"] != "rv-old" {
		t.Fatal("patch lost concurrency preconditions")
	}
	if len(meta["labels"].(map[string]any)) != 2 || len(meta["annotations"].(map[string]any)) != 3 {
		t.Fatal("foreign metadata was authored")
	}
}

func TestKubernetesCreateDeleteAndNamespaceFence(t *testing.T) {
	d := testDesired()
	name, _ := ResourceName(d.AgentID)
	calls := 0
	server := newContractServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.HasPrefix(r.URL.Path, "/apis/"+GroupVersionAPI+"/namespaces/"+d.Namespace+"/agents") {
			t.Error("wrong API path")
		}
		switch r.Method {
		case "POST":
			var a Agent
			json.NewDecoder(r.Body).Decode(&a)
			a.Metadata.UID = "uid-created"
			a.Metadata.ResourceVersion = "7"
			a.Metadata.Generation = 1
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(a)
		case "DELETE":
			var body struct{ Preconditions DeletePreconditions }
			json.NewDecoder(r.Body).Decode(&body)
			if body.Preconditions.UID != "uid-created" || body.Preconditions.ResourceVersion != "7" {
				t.Error("delete was not fenced")
			}
			w.WriteHeader(409)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	store, _ := NewKubernetesStore(HTTPConfig{AllowInsecureLoopback: true, Endpoint: server.URL, Client: server.Client()}, []string{d.Namespace})
	r, _ := New(store, testConfig())
	a := r.buildAgent(d, name, DesiredRevision(d))
	if err := store.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if a.Metadata.ResourceVersion != "7" || a.Metadata.UID != "uid-created" {
		t.Fatal("create response identity lost")
	}
	if err := store.Delete(context.Background(), d.Namespace, name, DeletePreconditions{a.Metadata.UID, a.Metadata.ResourceVersion}); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("delete conflict: %v", err)
	}
	if _, err := store.Get(context.Background(), "forbidden", name); !errors.Is(err, ErrNamespaceNotAllowed) {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), d.Namespace, name, DeletePreconditions{}); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("unexpected requests: %d", calls)
	}
}

func TestKubernetesListPaginationAndErrors(t *testing.T) {
	calls := 0
	server := newContractServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("labelSelector") != LabelManagedBy+"="+LabelManagedByValue {
			t.Error("missing managed selector")
		}
		if calls == 1 {
			w.Write([]byte(`{"metadata":{"continue":"opaque/+="},"items":[]}`))
			return
		}
		if r.URL.Query().Get("continue") != "opaque/+=" {
			t.Error("bad continuation")
		}
		w.Write([]byte(`{"metadata":{},"items":[]}`))
	}))
	defer server.Close()
	store, _ := NewKubernetesStore(HTTPConfig{AllowInsecureLoopback: true, Endpoint: server.URL, Client: server.Client()}, []string{"kagent-system"})
	if _, err := store.List(context.Background(), "kagent-system"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("did not paginate")
	}
}

// Sandboxed checks cannot bind sockets. Dispatch real http.Client requests to
// an HTTP consumer handler while preserving encoding, headers and responses.
type contractServer struct {
	URL     string
	handler http.Handler
}

func newContractServer(h http.Handler) *contractServer {
	return &contractServer{"http://localhost", h}
}
func (s *contractServer) Close() {}
func (s *contractServer) Client() *http.Client {
	return &http.Client{Transport: handlerTransport{s.handler}}
}

type handlerTransport struct{ handler http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	w := httptest.NewRecorder()
	t.handler.ServeHTTP(w, r)
	return w.Result(), nil
}
