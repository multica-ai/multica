//go:build !windows

package kagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRereviewEnrollmentIdentityAtEveryLoad(t *testing.T) {
	for _, corruption := range []string{"copied", "namespace", "owner", "version", "state"} {
		t.Run(corruption, func(t *testing.T) {
			r, store := newTestReconciler(t)
			gets := 0
			r.store = &deletionStore{Store: store, get: func(ctx context.Context, ns, name string) (*Agent, error) {
				gets++
				return store.Get(ctx, ns, name)
			}}
			l := lifecycleForTest(t, r, t.TempDir())
			d := testDesired()
			if err := l.Enroll(d); err != nil {
				t.Fatal(err)
			}
			rec, state, err := l.record(d.AgentID)
			if err != nil {
				t.Fatal(err)
			}
			switch corruption {
			case "copied":
				state.Desired.AgentID = NewID()
			case "namespace":
				state.Desired.Namespace = "outside-allowlist"
			case "owner":
				state.Desired.OwnerToken = "broken"
			case "version":
				state.Version = 0
			case "state":
				state.DeletionComplete = true
			}
			if err := rec.Save(state); err != nil {
				t.Fatal(err)
			}
			rec.Close()
			if _, err := l.Acquire(context.Background(), d.AgentID); err == nil {
				t.Fatal("corrupt enrollment acquired")
			}
			if err := l.Enroll(d); err == nil {
				t.Fatal("corrupt enrollment edited")
			}
			if err := l.Delete(context.Background(), d.AgentID, true); err == nil {
				t.Fatal("corrupt enrollment deleted")
			}
			if gets != 0 || len(store.ops) != 0 || len(store.objs) != 0 {
				t.Fatal("external operation before validation")
			}
		})
	}
}

func TestRereviewCopiedEnrollmentCannotUseAnotherAgentsLock(t *testing.T) {
	r, _ := newTestReconciler(t)
	l := lifecycleForTest(t, r, t.TempDir())
	a, b := testDesired(), testDesired()
	b.AgentID = NewID()
	if err := l.Enroll(a); err != nil {
		t.Fatal(err)
	}
	if err := l.Enroll(b); err != nil {
		t.Fatal(err)
	}
	lease, err := l.Acquire(context.Background(), b.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	// Copy B's fully valid, bound enrollment byte-for-byte over A's filename.
	raw, err := os.ReadFile(lease.record.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.journal.dir, recordFile("agent/"+a.AgentID)), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Acquire(context.Background(), a.AgentID); err == nil {
		t.Fatal("B reconciled while holding only A's lock")
	}
	records, err := l.journal.records()
	if err != nil || len(records) != 1 || records[0].AgentID != b.AgentID {
		t.Fatalf("recovery trusted copied record: %+v, %v", records, err)
	}
}

func TestRereviewLegacySemanticFences(t *testing.T) {
	for _, mode := range []string{"missing-active", "owner", "other-owner", "version", "settled-active", "missing-attempt", "unrelated-malformed"} {
		t.Run(mode, func(t *testing.T) {
			j, err := NewJournal(filepath.Join(t.TempDir(), "journal"))
			if err != nil {
				t.Fatal(err)
			}
			owner := NewID()
			a := map[string]any{"MessageID": NewID(), "InputHash": strings.Repeat("a", 64), "Generation": NewID(), "TaskID": "task", "Settled": false}
			s := map[string]any{"Version": 2, "AgentUID": "uid", "OwnerToken": owner, "Endpoint": "endpoint", "SessionID": NewID(), "Generation": a["Generation"], "ActiveAttempt": "attempt", "Attempts": map[string]any{"attempt": a}}
			switch mode {
			case "missing-active":
				delete(s, "ActiveAttempt")
			case "owner":
				s["OwnerToken"] = "corrupt"
			case "other-owner":
				s["OwnerToken"] = NewID()
			case "version":
				s["Version"] = 3
			case "settled-active":
				a["Settled"], a["TerminalState"] = true, StateCompleted
			case "missing-attempt":
				s["ActiveAttempt"] = "absent"
			case "unrelated-malformed":
				s["AgentUID"], s["OwnerToken"] = "another", "corrupt"
			}
			rec, err := j.Lock("legacy")
			if err != nil {
				t.Fatal(err)
			}
			if err := rec.Save(s); err != nil {
				t.Fatal(err)
			}
			rec.Close()
			if err := j.CheckConversationFences("uid", owner, "legacy", "attempt"); err == nil {
				t.Fatal("semantic corruption bypassed fence")
			}
		})
	}
}

func TestRereviewDuplicateJSONNeverProvesNonDispatch(t *testing.T) {
	const proof = `[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","domain":"a2a-protocol.org","reason":"UNSUPPORTED_OPERATION","metadata":{"reason":"KAGENT_SEND_NOT_ACCEPTED"}}]`
	for _, mode := range []string{"result", "error", "code", "nested", "case", "trailing", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			s := newContractServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var req struct{ ID string }
				json.NewDecoder(r.Body).Decode(&req)
				errBody := `{"code":-32004,"data":` + proof + `}`
				members := `"error":` + errBody
				switch mode {
				case "result":
					members = `"result":{"task":{"id":"sentinel"}},"result":null,` + members
				case "error":
					members += `,"error":` + errBody
				case "case":
					members += `,"Error":` + errBody
				case "code":
					members = `"error":{"code":-1,"code":-32004,"data":` + proof + `}`
				case "nested":
					members = `"error":{"code":-32004,"data":` + strings.Replace(proof, `"metadata":{`, `"metadata":{"reason":"sentinel",`, 1) + `}`
				case "conflict":
					members = `"result":{"task":{}},` + members
				}
				body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%q,%s}`, req.ID, members)
				if mode == "trailing" {
					body += `{}`
				}
				fmt.Fprint(w, body)
			}))
			c, err := NewA2AClient(HTTPConfig{Endpoint: s.URL, Client: s.Client(), AllowInsecureLoopback: true})
			if err != nil {
				t.Fatal(err)
			}
			d := testDesired()
			_, err = c.Send(context.Background(), d.Namespace, mustResourceName(d.AgentID), NewID(), "", "question")
			if err == nil || calls != 1 || strings.Contains(err.Error(), "sentinel") {
				t.Fatalf("unsafe response/retry: %v calls=%d", err, calls)
			}
		})
	}
}

func TestRereviewDuplicateJournalRejected(t *testing.T) {
	j, err := NewJournal(filepath.Join(t.TempDir(), "journal"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := j.Lock("duplicate")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := os.WriteFile(r.path, []byte(`{"Version":2,"Attempts":{"x":{"Settled":false,"Settled":true}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	var v any
	if _, err := r.Load(&v); err == nil {
		t.Fatal("duplicate nested journal member accepted")
	}
}
