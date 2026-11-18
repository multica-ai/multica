package kagentbroker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/pkg/kagent"
)

func testOwnership(t *testing.T, p Policy, committed bool) *OwnershipJournal {
	t.Helper()
	initial := p
	initial.ExpectedUID = ""
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	j, e := OpenOwnershipJournal(filepath.Join(root, "ownership"), initial, true)
	if e != nil {
		t.Fatal(e)
	}
	if committed {
		if j.save(ownershipRecord{Version: 1, Policy: fingerprint(p), State: bound, Digest: fingerprintBytes([]byte("test")), UID: p.ExpectedUID}) != nil {
			t.Fatal("save")
		}
	}
	t.Cleanup(func() { _ = j.Close() })
	return j
}
func createBody(p Policy) string {
	a := agent(p)
	a.Metadata.UID = ""
	a.Metadata.ResourceVersion = ""
	b, _ := json.Marshal(a)
	return string(b)
}
func creationFixture(t *testing.T) (*fixture, *int) {
	t.Helper()
	f := setup(t)
	p := testPolicy()
	p.ExpectedUID = ""
	f.b.policy = p
	f.b.ownership = testOwnership(t, p, false)
	posts := new(int)
	f.b.kube.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("not json"))}, nil
		}
		*posts++
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		meta := object(m["metadata"])
		meta["uid"] = testUID
		meta["resourceVersion"] = "1"
		meta["generation"] = json.Number("1")
		f.observed = m
		out := reply(m)
		out.StatusCode = 201
		return out, nil
	})
	return f, posts
}
func liveTransport(f *fixture, posts *int) roundTrip {
	return func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return reply(f.observed), nil
		}
		if r.Method == "POST" {
			*posts++
			return nil, errors.New("unexpected POST")
		}
		var patch map[string]any
		_ = json.NewDecoder(r.Body).Decode(&patch)
		// Fake Kubernetes merge patch, preserving null clears.
		var merge func(map[string]any, map[string]any)
		merge = func(dst, src map[string]any) {
			for k, v := range src {
				if v == nil {
					delete(dst, k)
				} else if m, ok := v.(map[string]any); ok {
					d := object(dst[k])
					if d == nil {
						d = map[string]any{}
						dst[k] = d
					}
					merge(d, m)
				} else {
					dst[k] = v
				}
			}
		}
		merge(f.observed, patch)
		object(f.observed["metadata"])["resourceVersion"] = "2"
		return reply(f.observed), nil
	}
}
func TestDurableCreateReplayAndRealStorePatch(t *testing.T) {
	f, posts := creationFixture(t)
	p := f.b.policy
	body := createBody(p)
	if w := serve(f.b, "POST", p.collection(), body, nil); w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	j := f.b.ownership
	path := j.path
	_ = j.Close()
	reopened, e := OpenOwnershipJournal(path, p, false)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	f.b.ownership = reopened
	f.b.kube.client.Transport = liveTransport(f, posts)
	if w := serve(f.b, "POST", p.collection(), body, nil); w.Code != 201 || *posts != 1 {
		t.Fatalf("replay %d posts=%d", w.Code, *posts)
	}
	if w := serve(f.b, "POST", p.collection(), strings.Replace(body, "Be helpful", "changed", 1), nil); w.Code != 409 {
		t.Fatal(w.Code)
	}
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		r.RequestURI = r.URL.RequestURI()
		r.Header.Set(InnerTokenHeader, "caller-credential")
		w := serveRequest(f.b, r)
		return w, nil
	})}
	store, e := kagent.NewKubernetesStore(kagent.HTTPConfig{Endpoint: "https://broker.invalid", Client: client}, []string{"dev"})
	if e != nil {
		t.Fatal(e)
	}
	a, e := store.Get(context.Background(), "dev", p.name())
	if e != nil {
		t.Fatal(e)
	}
	a.Spec.Template.SystemPrompt = ""
	a.Spec.Template.Description = "updated"
	if e = store.Update(context.Background(), a); e != nil {
		t.Fatal(e)
	}
	if _, exists := object(object(f.observed["spec"])["template"])["systemPrompt"]; exists {
		t.Fatal("null clear lost")
	}
	if a.Metadata.ResourceVersion != "2" {
		t.Fatal("RV not returned")
	}
	a.Metadata.ResourceVersion = "stale"
	if e = store.Update(context.Background(), a); !errors.Is(e, kagent.ErrConflict) {
		t.Fatalf("conflict: %v", e)
	}
	object(f.observed["metadata"])["uid"] = testOwner
	if w := serve(f.b, "GET", p.resource(), "", nil); w.Code != 502 {
		t.Fatal("replaced UID accepted")
	}
}

func serveRequest(b *Broker, r *http.Request) *http.Response {
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	return w.Result()
}

func TestCreateOutcomesRemainFenced(t *testing.T) {
	for _, scenario := range []string{"lost-response", "409", "malformed", "unsafe-spec", "terminating", "wrong-token", "wrong-name", "missing-rv", "commit-failure", "intent-only"} {
		t.Run(scenario, func(t *testing.T) {
			f, posts := creationFixture(t)
			p := f.b.policy
			body := createBody(p)
			original := f.b.kube.client.Transport
			f.b.kube.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Method == "GET" {
					return original.RoundTrip(r)
				}
				rec, _ := f.b.ownership.snapshot()
				if rec.State != createIntent {
					t.Error("POST before intent")
				}
				out, e := original.RoundTrip(r)
				if e != nil {
					return out, e
				}
				switch scenario {
				case "lost-response":
					return nil, errors.New("secret transport error")
				case "409":
					out.StatusCode = 409
				case "malformed":
					out.Body = io.NopCloser(strings.NewReader("{"))
				case "unsafe-spec":
					object(object(f.observed["spec"])["template"])["tools"] = nil
					out = reply(f.observed)
					out.StatusCode = 201
				case "terminating":
					object(f.observed["metadata"])["deletionTimestamp"] = "now"
					out = reply(f.observed)
					out.StatusCode = 201
				case "wrong-token":
					object(object(f.observed["metadata"])["annotations"])[ownerAnnotation] = testUID
					out = reply(f.observed)
					out.StatusCode = 201
				case "wrong-name":
					object(f.observed["metadata"])["name"] = "other"
					out = reply(f.observed)
					out.StatusCode = 201
				case "missing-rv":
					delete(object(f.observed["metadata"]), "resourceVersion")
					out = reply(f.observed)
					out.StatusCode = 201
				}
				return out, nil
			})
			j := f.b.ownership
			if scenario == "commit-failure" {
				j.persist = func(r ownershipRecord) error {
					if r.State == bound {
						return errDenied
					}
					return j.durableReplace(r)
				}
			}
			if scenario == "intent-only" {
				r, _ := j.snapshot()
				r.State = createIntent
				r.Digest = requestDigest(asMapJSON(t, body))
				if j.save(r) != nil {
					t.Fatal("intent save")
				}
			} else if w := serve(f.b, "POST", p.collection(), body, nil); w.Code < 400 {
				t.Fatal("accepted uncertain create")
			}
			count := *posts
			path := j.path
			j.Close()
			reopen, e := OpenOwnershipJournal(path, p, false)
			if e != nil {
				t.Fatal(e)
			}
			defer reopen.Close()
			f.b.ownership = reopen
			f.b.kube.client.Transport = liveTransport(f, posts)
			if w := serve(f.b, "POST", p.collection(), body, nil); w.Code != 409 || *posts != count {
				t.Fatalf("unfenced restart %d posts=%d", w.Code, *posts)
			}
			if w := serve(f.b, "GET", p.resource(), "", nil); w.Code != 403 {
				t.Fatal("intent adopted")
			}
		})
	}
}
func asMapJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	m, e := parseObject([]byte(s))
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func TestCreateRequiresActual404(t *testing.T) {
	for _, status := range []int{200, 201, 403, 409, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f, posts := creationFixture(t)
			f.b.kube.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" {
					*posts++
				}
				out := reply(map[string]any{"kind": "Status", "code": 404})
				out.StatusCode = status
				return out, nil
			})
			p := f.b.policy
			if w := serve(f.b, "POST", p.collection(), createBody(p), nil); w.Code < 400 || *posts != 0 {
				t.Fatal("fake absence authorized create")
			}
			rec, _ := f.b.ownership.snapshot()
			if rec.State != uninitialized {
				t.Fatal("unexpected intent")
			}
		})
	}
	// A foreign object under the exact name with copied owner token never enrolls.
	f, posts := creationFixture(t)
	f.b.kube.client.Transport = liveTransport(f, posts)
	p := f.b.policy
	if w := serve(f.b, "POST", p.collection(), createBody(p), nil); w.Code != 409 || *posts != 0 {
		t.Fatal("foreign object adopted")
	}
}
func TestOwnershipJournalValidation(t *testing.T) {
	p := testPolicy()
	p.ExpectedUID = ""
	t.Run("missing", func(t *testing.T) {
		if _, e := OpenOwnershipJournal(filepath.Join(t.TempDir(), "absent"), p, false); e == nil {
			t.Fatal("missing initialized")
		}
	})
	for _, scenario := range []string{"lock", "policy", "missing-record", "torn", "version", "mode", "symlink", "hardlink", "directory-mode", "initialize-again"} {
		t.Run(scenario, func(t *testing.T) {
			j := testOwnership(t, p, false)
			path := j.path
			if scenario == "lock" {
				if _, e := OpenOwnershipJournal(path, p, false); e == nil {
					t.Fatal("second owner")
				}
				return
			}
			j.Close()
			switch scenario {
			case "policy":
				p.Model = "other"
			case "missing-record":
				os.Remove(filepath.Join(path, "record"))
			case "torn":
				os.WriteFile(filepath.Join(path, "record"), []byte("{"), 0600)
			case "version":
				raw, _ := os.ReadFile(filepath.Join(path, "record"))
				os.WriteFile(filepath.Join(path, "record"), []byte(strings.Replace(string(raw), `"version":1`, `"version":2`, 1)), 0600)
			case "mode":
				os.Chmod(filepath.Join(path, "record"), 0644)
			case "symlink":
				os.Rename(filepath.Join(path, "record"), filepath.Join(path, "real"))
				os.Symlink("real", filepath.Join(path, "record"))
			case "hardlink":
				os.Link(filepath.Join(path, "record"), filepath.Join(path, "copy"))
			case "directory-mode":
				os.Chmod(path, 0755)
			}
			if next, e := OpenOwnershipJournal(path, p, scenario == "initialize-again"); e == nil {
				next.Close()
				t.Fatal("unsafe journal accepted")
			}
		})
	}
}
func TestIntentPersistenceFailureAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		f, posts := creationFixture(t)
		j := f.b.ownership
		p := f.b.policy
		if !cancelled {
			j.persist = func(ownershipRecord) error { return errDenied }
		}
		w := serve(f.b, "POST", p.collection(), createBody(p), func(r *http.Request) {
			if cancelled {
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				*r = *r.WithContext(ctx)
			}
		})
		if w.Code < 400 || *posts != 0 {
			t.Fatal("dispatch despite failure")
		}
		if !cancelled {
			if _, ok := j.snapshot(); ok {
				t.Fatal("journal not poisoned")
			}
		}
	}
}
func TestConcurrentCreatesIssueOnePOST(t *testing.T) {
	f, posts := creationFixture(t)
	p := f.b.policy
	original := f.b.kube.client.Transport
	f.b.kube.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" && *posts > 0 {
			return reply(f.observed), nil
		}
		return original.RoundTrip(r)
	})
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := serve(f.b, "POST", p.collection(), createBody(p), nil); w.Code != 201 {
				t.Errorf("create/replay: %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if *posts != 1 {
		t.Fatal("multiple POSTs", *posts)
	}
}

func TestOwnershipPersistenceFaultBoundaries(t *testing.T) {
	for _, stage := range []string{"write", "file-sync", "rename", "directory-sync"} {
		t.Run(stage, func(t *testing.T) {
			f, posts := creationFixture(t)
			j := f.b.ownership
			p := f.b.policy
			j.fault = func(s string) error {
				if s == stage {
					return errDenied
				}
				return nil
			}
			if w := serve(f.b, "POST", p.collection(), createBody(p), nil); w.Code != 503 || *posts != 0 {
				t.Fatal("intent fault dispatched", w.Code, *posts)
			}
			if w := serve(f.b, "POST", p.collection(), createBody(p), nil); w.Code != 503 {
				t.Fatal("poison not retained")
			}
			path := j.path
			j.Close()
			next, e := OpenOwnershipJournal(path, p, false)
			if e != nil {
				t.Fatal(e)
			}
			defer next.Close()
			rec, _ := next.snapshot()
			if rec.State != uninitialized && rec.State != createIntent {
				t.Fatal("torn publication")
			}
			if stage == "directory-sync" && rec.State != createIntent {
				t.Fatal("renamed intent missing")
			}
		})
	}
}
func TestPatchRejectsDriftAndDoesNotRetry(t *testing.T) {
	for _, scenario := range []string{"copied-token", "replaced-uid", "unsafe-current", "terminating-current", "stale-rv", "upstream-conflict", "lost-response", "unsafe-response", "wrong-response-uid"} {
		t.Run(scenario, func(t *testing.T) {
			f, posts := creationFixture(t)
			p := f.b.policy
			if w := serve(f.b, "POST", p.collection(), createBody(p), nil); w.Code != 201 {
				t.Fatal(w.Code)
			}
			p.ExpectedUID = testUID
			req := map[string]any{"metadata": map[string]any{"uid": testUID, "resourceVersion": "1", "labels": object(f.observed["metadata"])["labels"], "annotations": object(f.observed["metadata"])["annotations"]}, "spec": map[string]any{"harnessRef": map[string]any{"name": p.Harness}, "harness": nil, "templateRef": nil, "template": map[string]any{"modelConfig": map[string]any{"name": p.Model}, "description": nil, "systemPrompt": nil, "systemPromptFrom": nil}}}
			patches := 0
			live := liveTransport(f, posts)
			f.b.kube.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Method == "GET" {
					return live.RoundTrip(r)
				}
				patches++
				if scenario == "lost-response" {
					return nil, errors.New("secret")
				}
				if scenario == "upstream-conflict" {
					out := reply(nil)
					out.StatusCode = 409
					return out, nil
				}
				out, e := live.RoundTrip(r)
				if scenario == "unsafe-response" {
					object(object(f.observed["spec"])["template"])["tools"] = nil
					out = reply(f.observed)
				}
				if scenario == "wrong-response-uid" {
					object(f.observed["metadata"])["uid"] = testOwner
					out = reply(f.observed)
				}
				return out, e
			})
			switch scenario {
			case "copied-token", "replaced-uid":
				object(f.observed["metadata"])["uid"] = testOwner
			case "unsafe-current":
				object(object(f.observed["spec"])["template"])["skills"] = nil
			case "terminating-current":
				object(f.observed["metadata"])["deletionTimestamp"] = "now"
			case "stale-rv":
				object(req["metadata"])["resourceVersion"] = "stale"
			}
			raw, _ := json.Marshal(req)
			w := serve(f.b, "PATCH", p.resource(), string(raw), nil)
			if w.Code < 400 {
				t.Fatal("bad patch accepted")
			}
			want := 0
			switch scenario {
			case "upstream-conflict", "lost-response", "unsafe-response", "wrong-response-uid":
				want = 1
			}
			if patches != want {
				t.Fatal("patch dispatch/retry", patches, want)
			}
		})
	}
}
func TestCreateUnsafeShapesAndContextGate(t *testing.T) {
	for _, field := range []string{"tools", "skills", "plugins", "outputSchema", "mcp", "futureCapability"} {
		f, posts := creationFixture(t)
		p := f.b.policy
		m := asMapJSON(t, createBody(p))
		object(object(m["spec"])["template"])[field] = nil
		raw, _ := json.Marshal(m)
		if w := serve(f.b, "POST", p.collection(), string(raw), nil); w.Code != 400 || *posts != 0 {
			t.Fatal("unsafe template accepted", field)
		}
	}
	f, posts := creationFixture(t)
	p := f.b.policy
	j := f.b.ownership
	j.gate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if j.acquire(ctx) {
		t.Fatal("cancelled gate acquired")
	}
	<-j.gate
	if *posts != 0 {
		t.Fatal("unexpected dispatch")
	}
	if w := serve(f.b, "GET", p.resource(), "", nil); w.Code != 404 {
		t.Fatal("real absence hidden", w.Code)
	}
}

func TestRealKubernetesStoreCreate(t *testing.T) {
	f, posts := creationFixture(t)
	p := f.b.policy
	store, e := kagent.NewKubernetesStore(kagent.HTTPConfig{Endpoint: "https://broker.invalid", Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		r.RequestURI = r.URL.RequestURI()
		r.Header.Set(InnerTokenHeader, "caller-credential")
		return serveRequest(f.b, r), nil
	})}}, []string{"dev"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.Get(context.Background(), "dev", p.name()); !errors.Is(e, kagent.ErrNotFound) {
		t.Fatalf("true absence: %v", e)
	}
	a := agent(p)
	a.Metadata.ResourceVersion = ""
	if e = store.Create(context.Background(), a); e != nil {
		t.Fatal(e)
	}
	if a.Metadata.UID != testUID || a.Metadata.ResourceVersion != "1" || *posts != 1 {
		t.Fatal("create identity not returned")
	}
	f.b.kube.client.Transport = liveTransport(f, posts)
	if _, e = store.Get(context.Background(), "dev", p.name()); e != nil {
		t.Fatal(e)
	}
}
func TestOwnershipStrictRecordsAndLockPath(t *testing.T) {
	for _, scenario := range []string{"unknown-key", "duplicate-version", "null-digest", "bad-digest", "bad-state", "symlink-lock", "missing-lock", "ancestor-symlink"} {
		t.Run(scenario, func(t *testing.T) {
			p := testPolicy()
			p.ExpectedUID = ""
			j := testOwnership(t, p, false)
			path := j.path
			j.Close()
			recordPath := filepath.Join(path, "record")
			raw, e := os.ReadFile(recordPath)
			if e != nil {
				t.Fatal(e)
			}
			switch scenario {
			case "unknown-key":
				raw = append([]byte(`{"extra":true,`), raw[1:]...)
			case "duplicate-version":
				raw = append([]byte(`{"version":1,`), raw[1:]...)
			case "null-digest":
				raw = []byte(strings.Replace(string(raw), `"digest":""`, `"digest":null`, 1))
			case "bad-digest":
				raw = []byte(strings.Replace(string(raw), `"digest":""`, `"digest":"garbage"`, 1))
			case "bad-state":
				raw = []byte(strings.Replace(string(raw), uninitialized, "Unknown", 1))
			case "symlink-lock":
				os.Rename(filepath.Join(path, "lock"), filepath.Join(path, "real-lock"))
				os.Symlink("real-lock", filepath.Join(path, "lock"))
			case "missing-lock":
				os.Remove(filepath.Join(path, "lock"))
			case "ancestor-symlink":
				alias := filepath.Join(filepath.Dir(path), "alias")
				os.Symlink(path, alias)
				path = alias
			}
			if e = os.WriteFile(recordPath, raw, 0600); e != nil {
				t.Fatal(e)
			}
			if next, e := OpenOwnershipJournal(path, p, false); e == nil {
				next.Close()
				t.Fatal("unsafe state accepted")
			}
		})
	}
}
