//go:build !windows

package kagent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestHTTPRequiresHTTPSOrExplicitLiteralLoopback(t *testing.T) {
	for _, endpoint := range []string{
		"https://cluster.example", "http://cluster.example", "http://localhost",
		"http://LOCALHOST:8080", "http://127.0.0.1:8080", "http://127.42.0.1",
		"http://[::1]:8080", "http://[::ffff:127.0.0.1]", "http://localhost.example",
		"http://localhost.", "http://127.0.0.1.example", "http://10.0.0.1",
		"http://0.0.0.0", "http://[::]", "http://127.1", "http://2130706433",
	} {
		for _, allow := range []bool{false, true} {
			t.Run(endpoint+map[bool]string{false: "/default", true: "/opt-in"}[allow], func(t *testing.T) {
				loopback := endpoint == "http://localhost" || endpoint == "http://LOCALHOST:8080" ||
					endpoint == "http://127.0.0.1:8080" || endpoint == "http://127.42.0.1" ||
					endpoint == "http://[::1]:8080" || endpoint == "http://[::ffff:127.0.0.1]"
				wantOK := endpoint == "https://cluster.example" || (allow && loopback)
				c := HTTPConfig{Endpoint: endpoint, AllowInsecureLoopback: allow, Token: func(context.Context) (string, error) {
					t.Fatal("endpoint validation requested credentials")
					return "", nil
				}}
				if _, err := NewKubernetesStore(c, []string{"dev"}); (err == nil) != wantOK {
					t.Fatalf("Kubernetes endpoint: %v, want accepted=%v", err, wantOK)
				}
				if _, err := NewA2AClient(c); (err == nil) != wantOK {
					t.Fatalf("A2A endpoint: %v, want accepted=%v", err, wantOK)
				}
			})
		}
	}
}

type lostCreateAckStore struct{ *fakeStore }

func (s lostCreateAckStore) Create(ctx context.Context, a *Agent) error {
	if err := s.fakeStore.Create(ctx, a); err != nil {
		return err
	}
	return errors.New("lost create acknowledgement")
}

type unsupportedCreateStore struct{ *fakeStore }

func (s unsupportedCreateStore) Create(ctx context.Context, a *Agent) error {
	a.Spec.Template.ExtraFields = map[string]json.RawMessage{"tools": json.RawMessage(`[]`)}
	return s.fakeStore.Create(ctx, a)
}

func TestLifecyclePinsConfirmedCreateBeforeRejectingUnsupportedResponse(t *testing.T) {
	ctx := context.Background()
	s := unsupportedCreateStore{newFakeStore()}
	r, err := New(s, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	l := lifecycleForTest(t, r, t.TempDir())
	d := testDesired()
	if err := l.Enroll(d); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Acquire(ctx, d.AgentID); !errors.Is(err, ErrUnsupportedField) {
		t.Fatalf("unsupported create response: %v", err)
	}
	states, err := l.Recover(ctx)
	if err != nil || len(states) != 1 || states[0].Ready || states[0].UID == "" {
		t.Fatalf("confirmed binding: %+v %v", states, err)
	}
	if err := l.Delete(ctx, d.AgentID, true); err != nil {
		t.Fatalf("confirmed create cleanup: %v", err)
	}
}

func TestLifecycleLostCreateAckCannotAdoptOrDelete(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "original", true: "replacement"}[replacement], func(t *testing.T) {
			ctx := context.Background()
			s := lostCreateAckStore{newFakeStore()}
			r, err := New(s, testConfig())
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			l := lifecycleForTest(t, r, dir)
			d := testDesired()
			if err := l.Enroll(d); err != nil {
				t.Fatal(err)
			}
			if _, err := l.Acquire(ctx, d.AgentID); err == nil {
				t.Fatal("lost ack granted a lease")
			}
			name, _ := ResourceName(d.AgentID)
			obj := s.objs[key(d.Namespace, name)]
			if replacement {
				obj.Metadata.UID = "replacement-uid"
			}
			// Restart with the same durable enrollment; annotations/token are identical.
			l = lifecycleForTest(t, r, dir)
			if _, err := l.Acquire(ctx, d.AgentID); !errors.Is(err, ErrNotOwned) {
				t.Fatalf("unbound adoption: %v", err)
			}
			if err := l.Delete(ctx, d.AgentID, true); !errors.Is(err, ErrNotOwned) {
				t.Fatalf("unbound delete: %v", err)
			}
			if s.count("update") != 0 || s.count("delete") != 0 || s.objs[key(d.Namespace, name)] != obj {
				t.Fatal("ambiguous resource was mutated")
			}
			rec, state, err := l.record(d.AgentID)
			if err != nil {
				t.Fatal(err)
			}
			defer rec.Close()
			if state.Desired.ExpectedUID != "" || !state.Deleted {
				t.Fatalf("unexpected enrollment: %+v", state)
			}
		})
	}
}

func TestLifecycleConfirmedUIDFencesReplacementDeletion(t *testing.T) {
	ctx := context.Background()
	r, s := newTestReconciler(t)
	dir := t.TempDir()
	l := lifecycleForTest(t, r, dir)
	d := testDesired()
	if err := l.Enroll(d); err != nil {
		t.Fatal(err)
	}
	b, err := l.Reconcile(ctx, d.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	name, _ := ResourceName(d.AgentID)
	obj := s.objs[key(d.Namespace, name)]
	if b.Desired.ExpectedUID != obj.Metadata.UID {
		t.Fatal("confirmed UID was not pinned")
	}
	obj.Metadata.UID = "replacement-uid"
	l = lifecycleForTest(t, r, dir)
	if err := l.Delete(ctx, d.AgentID, true); !errors.Is(err, ErrNotOwned) {
		t.Fatalf("replacement deletion: %v", err)
	}
	if s.count("delete") != 0 {
		t.Fatal("replacement reached deletion transport")
	}
	// Retry tombstoned cleanup against the originally confirmed object.
	obj.Metadata.UID = b.Desired.ExpectedUID
	if err := l.Delete(ctx, d.AgentID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, d.Namespace, name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("original cleanup: %v", err)
	}
}

func TestTerminatingAgentCannotBecomeReadyOrAcquireExecution(t *testing.T) {
	ctx := context.Background()
	r, s := newTestReconciler(t)
	l := lifecycleForTest(t, r, t.TempDir())
	d := testDesired()
	if err := l.Enroll(d); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Reconcile(ctx, d.AgentID); err != nil {
		t.Fatal(err)
	}
	name, _ := ResourceName(d.AgentID)
	obj := s.objs[key(d.Namespace, name)]
	// Decode metadata as the REST transport does.
	if err := json.Unmarshal([]byte(`{"deletionTimestamp":"2026-11-18T00:00:00Z","generation":1}`), &obj.Metadata); err != nil {
		t.Fatal(err)
	}
	obj.Status.Conditions = []Condition{{Type: ConditionReady, Status: "True", ObservedGeneration: 1}}
	if ready, summary := readiness(obj); ready {
		t.Fatalf("terminating ready: %s", summary)
	}
	if _, err := l.Acquire(ctx, d.AgentID); !errors.Is(err, ErrTerminating) {
		t.Fatalf("terminating execution: %v", err)
	}
	states, err := l.Recover(ctx)
	if err != nil || len(states) != 1 || states[0].Ready {
		t.Fatalf("terminating recovery: %+v %v", states, err)
	}
	if s.count("update") != 0 {
		t.Fatal("terminating resource was updated")
	}
	if err := l.Delete(ctx, d.AgentID, true); !errors.Is(err, ErrDeletionPending) {
		t.Fatalf("safe terminating cleanup: %v", err)
	}
	if s.count("delete") != 0 {
		t.Fatal("terminating resource must only be observed")
	}
	delete(s.objs, key(d.Namespace, name))
	if _, err := l.Recover(ctx); err != nil {
		t.Fatalf("cleanup after observed absence: %v", err)
	}
}

func TestUnsupportedTemplateFieldsFailClosedAndRoundTrip(t *testing.T) {
	// Verified against alpha7 AgentTemplateSpec at 542e0a7: these are all
	// fields outside the managed subset. Future fields also fail closed.
	for _, field := range []string{"tools", "skills", "plugins", "promptTemplate", "outputSchema", "outputSchemaFrom", "futureCapability"} {
		for _, value := range []string{`null`, `[]`, `{"nested":{"preserve":true}}`} {
			t.Run(field+"/"+value, func(t *testing.T) {
				r, s := newTestReconciler(t)
				d := testDesired()
				name := mustCreate(t, r, &d)
				obj := s.objs[key(d.Namespace, name)]
				raw := []byte(`{"description":"drift","` + field + `":` + value + `}`)
				if err := json.Unmarshal(raw, &obj.Spec.Template); err != nil {
					t.Fatal(err)
				}
				before := cloneTemplate(obj.Spec.Template)
				for _, desired := range []*AgentTemplateSpec{d.Inline, cloneTemplate(obj.Spec.Template)} {
					attempt := d
					attempt.Inline = desired
					if _, err := r.reconcile(context.Background(), attempt); !errors.Is(err, ErrUnsupportedField) {
						t.Fatalf("unsupported field accepted: %v", err)
					}
				}
				encoded, err := json.Marshal(obj.Spec.Template)
				if err != nil {
					t.Fatal(err)
				}
				var after AgentTemplateSpec
				if err := json.Unmarshal(encoded, &after); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, &after) || s.count("update") != 0 {
					t.Fatalf("unsupported fields changed: %s", encoded)
				}
				// Ownership and drain still authorize cleanup despite capabilities.
				if err := r.delete(context.Background(), d, true); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
