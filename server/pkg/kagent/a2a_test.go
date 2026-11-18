package kagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Golden JSON shapes are from alpha7 gateway/http.go and its pinned
// a2a-go/v2 v2.6.0 internal/jsonrpc/jsonrpc.go and a2a/core.go.
func TestA2AAlpha7WireAndIdentity(t *testing.T) {
	const session = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	d := testDesired()
	name, _ := ResourceName(d.AgentID)
	calls := 0
	server := newContractServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/agents/"+d.Namespace+"/"+name {
			t.Error("actor or wrong gateway URL used")
		}
		var req struct {
			JSONRPC, ID, Method string
			Params              json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		var result any
		if calls == 1 {
			if req.Method != "SendMessage" {
				t.Fatal("used pre-1.0 method name")
			}
			var params map[string]any
			json.Unmarshal(req.Params, &params)
			message := params["message"].(map[string]any)
			if message["role"] != "ROLE_USER" || message["messageId"] != d.OwnerToken {
				t.Fatal("wrong A2A 1.0 message")
			}
			if _, ok := message["kind"]; ok {
				t.Fatal("invented 0.3 discriminator")
			}
			if message["parts"].([]any)[0].(map[string]any)["text"] != "brief\nquestion" {
				t.Fatal("lost input")
			}
			if params["configuration"].(map[string]any)["returnImmediately"] != true {
				t.Fatal("send would block for turn")
			}
			result = json.RawMessage(`{"task":{"id":"task-1","contextId":"` + session + `","status":{"state":"TASK_STATE_WORKING"}}}`)
		} else {
			if req.Method != "GetTask" {
				t.Fatal("recovery did not observe task")
			}
			result = json.RawMessage(`{"id":"task-1","contextId":"` + session + `","status":{"state":"TASK_STATE_COMPLETED"},"artifacts":[{"artifactId":"a","parts":[{"text":"answer"}]}]}`)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	client, _ := NewA2AClient(HTTPConfig{AllowInsecureLoopback: true, Endpoint: server.URL, Client: server.Client()})
	task, err := client.Send(context.Background(), d.Namespace, name, d.OwnerToken, "", "brief\nquestion")
	if err != nil {
		t.Fatal(err)
	}
	if task.Terminal() {
		t.Fatal("working is not terminal")
	}
	task, err = client.Get(context.Background(), d.Namespace, name, task.ID, task.ContextID)
	if err != nil {
		t.Fatal(err)
	}
	if !task.Terminal() || task.Text() != "answer" {
		t.Fatal("lost terminal artifact")
	}
}

func TestA2ARejectsUncertainResponsesWithoutRetry(t *testing.T) {
	d := testDesired()
	name, _ := ResourceName(d.AgentID)
	for _, kind := range []string{"http-auth", "rpc-auth", "wrong-id", "unknown-state", "missing-task", "wrong-context", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			server := newContractServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if kind == "http-auth" {
					w.WriteHeader(401)
					w.Write([]byte("secret remote diagnostic"))
					return
				}
				var req struct{ ID string }
				json.NewDecoder(r.Body).Decode(&req)
				if kind == "malformed" {
					w.Write([]byte(`{`))
					return
				}
				if kind == "rpc-auth" {
					json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32001, "message": "secret remote diagnostic"}})
					return
				}
				id := req.ID
				if kind == "wrong-id" {
					id = "other-response"
				}
				state := StateCompleted
				if kind == "unknown-state" {
					state = "future-state"
				}
				contextID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
				if kind == "wrong-context" {
					contextID = "bbbbbbbb-bbbb-cccc-dddd-eeeeeeeeeeee"
				}
				var task any = map[string]any{"id": "task", "contextId": contextID, "status": map[string]string{"state": state}}
				if kind == "missing-task" {
					task = nil
				}
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"task": task}})
			}))
			c, _ := NewA2AClient(HTTPConfig{AllowInsecureLoopback: true, Endpoint: server.URL, Client: server.Client()})
			_, err := c.Send(context.Background(), d.Namespace, name, d.OwnerToken, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "question")
			if err == nil {
				t.Fatal("unsafe response accepted")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("remote body leaked")
			}
			if calls != 1 {
				t.Fatal("side-effecting send retried")
			}
			if kind == "rpc-auth" {
				var rpc *RPCError
				if !errors.As(err, &rpc) || rpc.Code != -32001 {
					t.Fatal("lost typed error")
				}
			}
		})
	}
}

func TestTaskStateMatrix(t *testing.T) {
	for _, tc := range []struct {
		state    string
		terminal bool
	}{
		{StateSubmitted, false}, {StateWorking, false}, {StateInputRequired, false}, {StateAuthRequired, false},
		{StateCompleted, true}, {StateFailed, true}, {StateRejected, true}, {StateCanceled, true},
	} {
		task := &RemoteTask{ID: "task", ContextID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}
		task.Status.State = tc.state
		if err := validateTask(task); err != nil {
			t.Fatal(err)
		}
		if task.Terminal() != tc.terminal {
			t.Fatalf("terminal(%s)=%v", tc.state, task.Terminal())
		}
	}
}

func TestA2ASendRetriesOnlyStructuredNonDispatchProof(t *testing.T) {
	for _, mode := range []string{"exact", "generic-code", "wrong-code", "wrong-domain", "wrong-type", "wrong-reason", "plain-message", "mixed-result", "exhausted", "context"} {
		t.Run(mode, func(t *testing.T) {
			const sentinel = "opaque-provider-diagnostic-j8P2n7"
			calls := 0
			var first RemoteMessage
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			server := newContractServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var req struct {
					ID     string
					Params struct{ Message RemoteMessage }
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatal(err)
				}
				if calls == 1 {
					first = req.Params.Message
				} else if fmt.Sprint(req.Params.Message) != fmt.Sprint(first) {
					t.Error("retry changed input identity")
				}
				if calls > 1 && mode != "exhausted" {
					json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"task": map[string]any{"id": "task", "contextId": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "status": map[string]any{"state": StateCompleted}}}})
					return
				}
				detail := map[string]any{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "domain": "a2a-protocol.org", "reason": "UNSUPPORTED_OPERATION", "metadata": map[string]any{"reason": "KAGENT_SEND_NOT_ACCEPTED", "exception": sentinel}}
				code := -32004
				switch mode {
				case "wrong-code":
					code = -32603
				case "wrong-domain":
					detail["domain"] = "other"
				case "wrong-type":
					detail["@type"] = "other"
				case "wrong-reason":
					detail["reason"] = "OTHER"
				case "plain-message":
					detail["metadata"] = nil
				}
				rpc := map[string]any{"code": code, "message": sentinel + " KAGENT_SEND_NOT_ACCEPTED", "data": []any{detail}}
				if mode == "generic-code" {
					delete(rpc, "data")
				}
				body := map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": rpc}
				if mode == "mixed-result" {
					body["result"] = map[string]any{"task": map[string]string{"id": "possibly-dispatched"}}
				}
				json.NewEncoder(w).Encode(body)
				if mode == "context" {
					cancel()
				}
			}))
			c, err := NewA2AClient(HTTPConfig{AllowInsecureLoopback: true, Endpoint: server.URL, Client: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			d := testDesired()
			name, _ := ResourceName(d.AgentID)
			task, err := c.Send(ctx, d.Namespace, name, d.OwnerToken, "", "question")
			want := 1
			if mode == "exact" {
				want = 2
				if err != nil || task == nil {
					t.Fatalf("proven rejection did not retry: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected rejection")
				}
				if strings.Contains(fmt.Sprintf("%+v", err), sentinel) {
					t.Fatal("raw diagnostics retained in error")
				}
			}
			if mode == "exhausted" {
				want = 3
			}
			if calls != want {
				t.Fatalf("calls=%d want=%d", calls, want)
			}
			if mode == "context" && !errors.Is(err, context.Canceled) {
				t.Fatalf("context ignored: %v", err)
			}
		})
	}
}
