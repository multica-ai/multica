package kagentbroker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/kagent"
)

func continuationBody(messageID string) string {
	return strings.Replace(strings.Replace(sendBody(), `"role":"ROLE_USER"`, `"contextId":"`+testContext+`","role":"ROLE_USER"`, 1), `"messageId":"`+testOwner+`"`, `"messageId":"`+messageID+`"`, 1)
}

func taskBody(method string) string {
	return `{"jsonrpc":"2.0","id":"` + testID + `","method":"` + method + `","params":{"id":"` + testTask + `"}}`
}

func realClients(t *testing.T, b *Broker) (*kagent.A2AClient, *kagent.KubernetesStore) {
	t.Helper()
	config := kagent.HTTPConfig{Endpoint: "https://broker.invalid", Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		r.RequestURI = r.URL.RequestURI()
		r.Header.Set(InnerTokenHeader, "caller-credential")
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		return w.Result(), nil
	})}}
	a, err := kagent.NewA2AClient(config)
	if err != nil {
		t.Fatal(err)
	}
	k, err := kagent.NewKubernetesStore(config, []string{"dev"})
	if err != nil {
		t.Fatal(err)
	}
	return a, k
}

func loadAlpha7(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/alpha7-task.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := parseObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func rpcReply(r *http.Request, task map[string]any) *http.Response {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	var result any = task
	if req["method"] == "SendMessage" {
		result = map[string]any{"task": task}
	}
	return reply(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": result})
}

func TestUnboundContextAndRestartDenyBeforeDispatch(t *testing.T) {
	f := setup(t)
	if w := serve(f.b, "POST", f.b.policy.a2a(), continuationBody(testOwner), nil); w.Code != 403 || f.kube+f.a2a != 0 {
		t.Fatalf("unbound session dispatched: status=%d kube=%d a2a=%d", w.Code, f.kube, f.a2a)
	}
	if w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := serve(f.b, "POST", f.b.policy.a2a(), continuationBody(testID), nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	fresh := setup(t)
	for _, body := range []string{continuationBody(testID), taskBody("GetTask"), taskBody("CancelTask")} {
		if w := serve(fresh.b, "POST", fresh.b.policy.a2a(), body, nil); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	if fresh.kube+fresh.a2a != 0 {
		t.Fatal("restart adopted caller identity")
	}
}

func TestAlpha7SDKFixtureThroughRealClient(t *testing.T) {
	f := setup(t)
	f.upstream = func(r *http.Request) (*http.Response, error) { return rpcReply(r, loadAlpha7(t)), nil }
	client, _ := realClients(t, f.b)
	first, err := client.Send(context.Background(), "dev", f.b.policy.name(), testOwner, "", "hello")
	if err != nil || first.Text() != "alpha7 answer" || !first.Terminal() {
		t.Fatalf("send: %+v %v", first, err)
	}
	for _, get := range []func(context.Context, string, string, string, string) (*kagent.RemoteTask, error){client.Get, client.Cancel} {
		task, err := get(context.Background(), "dev", f.b.policy.name(), first.ID, first.ContextID)
		if err != nil || task.Text() != "alpha7 answer" {
			t.Fatalf("get/cancel: %+v %v", task, err)
		}
	}
	w := serve(f.b, "POST", f.b.policy.a2a(), taskBody("GetTask"), nil)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, forbidden := range []string{"credential", "metadata", "history", "extensions"} {
		if strings.Contains(w.Body.String(), forbidden) {
			t.Fatalf("optional data reflected: %s", w.Body.String())
		}
	}
}

func TestFailedOutputRetainsBindingAndBlocksResend(t *testing.T) {
	for _, kind := range []string{"state", "credential", "history", "metadata", "timestamp"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			f.upstream = func(r *http.Request) (*http.Response, error) {
				task := loadAlpha7(t)
				switch kind {
				case "state":
					object(task["status"])["state"] = "UNKNOWN"
				case "credential":
					object(object(task["status"])["message"])["parts"] = []any{map[string]any{"text": "gateway-credential"}}
				case "history":
					task["history"] = "invalid"
				case "metadata":
					task["metadata"] = false
				case "timestamp":
					object(task["status"])["timestamp"] = "invalid"
				}
				return rpcReply(r, task), nil
			}
			w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil)
			if w.Code != 502 || strings.Contains(w.Body.String(), "credential") {
				t.Fatal(w.Code, w.Body.String())
			}
			if f.b.tasks[testTask] != testContext {
				t.Fatal("lost recovery binding")
			}
			if w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil); w.Code != 409 || f.a2a != 1 {
				t.Fatal("ambiguous send retried", w.Code, f.a2a)
			}
			f.upstream = nil
			for _, method := range []string{"GetTask", "CancelTask"} {
				if w := serve(f.b, "POST", f.b.policy.a2a(), taskBody(method), nil); w.Code != 200 {
					t.Fatal("recovery unavailable", w.Code)
				}
			}
			if w := serve(f.b, "POST", f.b.policy.a2a(), continuationBody(testID), nil); w.Code != 409 {
				t.Fatal("uncertain turn continued", w.Code)
			}
		})
	}
}

func TestAmbiguousDispatchWithoutIdentityFailsClosed(t *testing.T) {
	for _, kind := range []string{"transport", "correlation", "contextCollision"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			if kind == "contextCollision" {
				if w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil); w.Code != 200 {
					t.Fatal(w.Code)
				}
			}
			f.upstream = func(r *http.Request) (*http.Response, error) {
				if kind == "transport" {
					return nil, errors.New("ambiguous connection failure")
				}
				if kind == "correlation" {
					return reply(map[string]any{"jsonrpc": "2.0", "id": testUID, "result": map[string]any{"task": loadAlpha7(t)}}), nil
				}
				return rpcReply(r, loadAlpha7(t)), nil
			}
			body := strings.Replace(sendBody(), testOwner, testID, 1)
			if w := serve(f.b, "POST", f.b.policy.a2a(), body, nil); w.Code != 502 {
				t.Fatal(w.Code)
			}
			before := f.a2a
			for _, body := range []string{body, strings.Replace(body, testID, testUID, -1)} {
				if w := serve(f.b, "POST", f.b.policy.a2a(), body, nil); w.Code != 403 {
					t.Fatal("ambiguous work redispatched", w.Code)
				}
			}
			if f.a2a != before {
				t.Fatal("retry reached gateway")
			}
		})
	}
}

func TestRecoveryAllowsSpecDriftButRejectsReplacementAndOutage(t *testing.T) {
	f := setup(t)
	client, _ := realClients(t, f.b)
	if _, err := client.Send(context.Background(), "dev", f.b.policy.name(), testOwner, "", "hello"); err != nil {
		t.Fatal(err)
	}
	object(f.observed["spec"])["harnessRef"] = map[string]any{"name": "drifted"}
	object(f.observed["metadata"])["deletionTimestamp"] = "2026-11-18T10:00:00Z"
	for _, method := range []string{"GetTask", "CancelTask"} {
		if w := serve(f.b, "POST", f.b.policy.a2a(), taskBody(method), nil); w.Code != 200 {
			t.Fatal("drift blocked recovery", w.Code)
		}
	}
	before := f.a2a
	if w := serve(f.b, "POST", f.b.policy.a2a(), continuationBody(testID), nil); w.Code != 502 {
		t.Fatal("drift allowed new execution", w.Code)
	}
	object(f.observed["metadata"])["uid"] = testOwner
	for _, method := range []string{"GetTask", "CancelTask"} {
		if w := serve(f.b, "POST", f.b.policy.a2a(), taskBody(method), nil); w.Code != 502 {
			t.Fatal("replacement accepted", w.Code)
		}
	}
	f.b.kube.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("unavailable") })
	if w := serve(f.b, "POST", f.b.policy.a2a(), taskBody("CancelTask"), nil); w.Code != 502 {
		t.Fatal("outage bypassed identity verification", w.Code)
	}
	if f.a2a != before {
		t.Fatal("unsafe operation dispatched")
	}
}

func TestReadinessThroughRealKubernetesClient(t *testing.T) {
	f := setup(t)
	object(f.observed["metadata"])["generation"] = json.Number("7")
	f.observed["status"] = map[string]any{"latestSuccessfulRevision": "private-unused", "conditions": []any{map[string]any{"type": "Ready", "status": "True", "reason": "Reconciled", "message": "ready", "observedGeneration": json.Number("7"), "lastTransitionTime": "2026-11-18T10:00:00Z"}}}
	_, store := realClients(t, f.b)
	a, err := store.Get(context.Background(), "dev", f.b.policy.name())
	if err != nil || a.Metadata.Generation != 7 || len(a.Status.Conditions) != 1 || a.Status.Conditions[0].ObservedGeneration != 7 || a.Status.Conditions[0].Status != "True" {
		t.Fatalf("readiness lost: %+v %v", a, err)
	}
	for _, bad := range []any{json.Number("7.1"), json.Number("-1"), json.Number("9007199254740992"), "7", nil} {
		object(f.observed["metadata"])["generation"] = bad
		if _, err := store.Get(context.Background(), "dev", f.b.policy.name()); err == nil {
			t.Fatal("invalid generation accepted", bad)
		}
	}
	object(f.observed["metadata"])["generation"] = json.Number("7")
	conditions := object(f.observed["status"])["conditions"].([]any)
	object(f.observed["status"])["conditions"] = append(conditions, conditions[0])
	if _, err := store.Get(context.Background(), "dev", f.b.policy.name()); err == nil {
		t.Fatal("duplicate readiness accepted")
	}
}

func TestSlowSendDoesNotBlockGetCancelOrCancelDeadline(t *testing.T) {
	f := setup(t)
	if w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	f.upstream = func(r *http.Request) (*http.Response, error) {
		raw := make(map[string]any)
		_ = json.NewDecoder(r.Body).Decode(&raw)
		if raw["method"] == "SendMessage" {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		if raw["method"] == "CancelTask" && r.Context().Value(deadlineTestKey{}) == true {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		task := map[string]any{"id": testTask, "contextId": testContext, "status": map[string]any{"state": "TASK_STATE_WORKING"}}
		var result any = task
		if raw["method"] == "SendMessage" {
			result = map[string]any{"task": task}
		}
		return reply(map[string]any{"jsonrpc": "2.0", "id": raw["id"], "result": result}), nil
	}
	sendDone := make(chan int, 1)
	go func() { sendDone <- serve(f.b, "POST", f.b.policy.a2a(), continuationBody(testID), nil).Code }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("send never entered")
	}
	var jobs sync.WaitGroup
	for _, method := range []string{"GetTask", "CancelTask"} {
		jobs.Add(1)
		go func() {
			defer jobs.Done()
			if w := serve(f.b, "POST", f.b.policy.a2a(), taskBody(method), nil); w.Code != 200 {
				t.Error("independent request blocked", w.Code)
			}
		}()
	}
	jobs.Add(1)
	go func() {
		defer jobs.Done()
		if w := serve(f.b, "POST", f.b.policy.a2a(), continuationBody(testUID), nil); w.Code != 409 {
			t.Error("concurrent turn dispatched", w.Code)
		}
	}()
	jobs.Add(1)
	go func() {
		defer jobs.Done()
		client, _ := realClients(t, f.b)
		ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), deadlineTestKey{}, true), 40*time.Millisecond)
		defer cancel()
		if _, err := client.Cancel(ctx, "dev", f.b.policy.name(), testTask, testContext); err == nil {
			t.Error("expired cancel succeeded")
		}
	}()
	done := make(chan struct{})
	go func() { jobs.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("slow send starved get/cancel/deadline")
	}
	// Cancel the send's transport via its bounded request deadline rather than
	// holding the test open; explicitly release and join before inspecting state.
	release <- struct{}{}
	select {
	case status := <-sendDone:
		if status != 200 {
			t.Fatal(status)
		}
	case <-time.After(time.Second):
		t.Fatal("send did not finish")
	}
}

type deadlineTestKey struct{}

func TestCapacityPreservesActiveAndUncertainBindings(t *testing.T) {
	f := setup(t)
	if w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	for i := 1; i < bindingLimit; i++ {
		f.b.attempts[kagent.NewID()] = true
	}
	if w := serve(f.b, "POST", f.b.policy.a2a(), continuationBody(testID), nil); w.Code != 403 {
		t.Fatal("capacity exceeded", w.Code)
	}
	for _, method := range []string{"GetTask", "CancelTask"} {
		if w := serve(f.b, "POST", f.b.policy.a2a(), taskBody(method), nil); w.Code != 200 {
			t.Fatal("capacity evicted recovery", w.Code)
		}
	}
	if f.b.tasks[testTask] != testContext || len(f.b.attempts) != bindingLimit {
		t.Fatal("capacity discarded recovery state")
	}
}

func FuzzAlpha7Response(f *testing.F) {
	raw, err := os.ReadFile("testdata/alpha7-task.json")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(string(raw))
	f.Add(`{"id":"` + testTask + `","contextId":"` + testContext + `","status":{"state":"TASK_STATE_WORKING"}}`)
	f.Add(`{"id":"` + testTask + `","contextId":"` + testContext + `","status":{"state":"TASK_STATE_COMPLETED"},"metadata":{},"history":[]}`)
	f.Add(`{"id":null,"status":{}}`)
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > responseLimit {
			return
		}
		m, err := parseObject([]byte(raw))
		if err != nil {
			return
		}
		if validTask(m) {
			if reflects(m, []string{"gateway-credential", "caller-credential"}) {
				return
			}
			out, err := json.Marshal(m)
			if err != nil || len(out) > responseLimit {
				t.Fatal("invalid reconstructed task")
			}
			if _, exists := m["history"]; exists {
				t.Fatal("history reflected")
			}
			if _, exists := m["metadata"]; exists {
				t.Fatal("metadata reflected")
			}
		}
	})
}

func TestConcurrentMessageReplayDispatchesOnce(t *testing.T) {
	f := setup(t)
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	f.upstream = func(r *http.Request) (*http.Response, error) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return rpcReply(r, loadAlpha7(t)), nil
	}
	first := make(chan int, 1)
	go func() { first <- serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil).Code }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("send never dispatched")
	}
	var jobs sync.WaitGroup
	for range 16 {
		jobs.Add(1)
		go func() {
			defer jobs.Done()
			if w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil); w.Code != 409 {
				t.Error("duplicate dispatched", w.Code)
			}
		}()
	}
	jobs.Wait()
	release <- struct{}{}
	select {
	case status := <-first:
		if status != 200 {
			t.Fatal(status)
		}
	case <-time.After(time.Second):
		t.Fatal("send stuck")
	}
	if f.a2a != 1 {
		t.Fatal("duplicate dispatch count", f.a2a)
	}
}

func TestCanceledBeforeDispatchDoesNotConsumeAttempt(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), func(r *http.Request) { *r = *r.WithContext(ctx) })
	if w.Code < 400 || f.a2a != 0 || len(f.b.attempts) != 0 {
		t.Fatal("canceled request dispatched or consumed capacity", w.Code)
	}
	if w := serve(f.b, "POST", f.b.policy.a2a(), sendBody(), nil); w.Code != 200 {
		t.Fatal("predispatch cancellation poisoned send", w.Code)
	}
}

// The broker intentionally has no collection route. Feed its one exact-resource
// observation into the real reconciler's List boundary to test readiness logic.
type observedStore struct {
	*kagent.KubernetesStore
	name string
}

func (s observedStore) List(ctx context.Context, ns string) ([]*kagent.Agent, error) {
	a, err := s.Get(ctx, ns, s.name)
	if err != nil {
		return nil, err
	}
	return []*kagent.Agent{a}, nil
}

func TestReconcilerReadinessUsesCurrentGeneration(t *testing.T) {
	f := setup(t)
	_, store := realClients(t, f.b)
	p := f.b.policy
	reconciler, err := kagent.New(observedStore{store, p.name()}, kagent.Config{AllowedNamespaces: []string{p.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		state    string
		observed string
		want     bool
	}{{"True", "7", true}, {"True", "6", false}, {"False", "7", false}, {"Unknown", "7", false}} {
		object(f.observed["metadata"])["generation"] = json.Number("7")
		f.observed["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": tc.state, "observedGeneration": json.Number(tc.observed)}}}
		states, err := reconciler.Recover(context.Background(), []kagent.EnrollmentRecord{{AgentID: p.AgentID, Namespace: p.Namespace, OwnerToken: p.OwnerToken, ExpectedUID: p.ExpectedUID}})
		if err != nil || len(states) != 1 || states[0].Ready != tc.want {
			t.Fatalf("Ready=%s observed=%s: %+v %v", tc.state, tc.observed, states, err)
		}
	}
}
