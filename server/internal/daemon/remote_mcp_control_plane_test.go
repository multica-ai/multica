package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTaskManagedMCPControlPlaneCommitsMetadataOnlyLifecycleInOrder(t *testing.T) {
	const (
		invocationID = "018f0000-0000-7000-8000-000000000001"
		approvalID   = "018f0000-0000-7000-8000-000000000002"
		digest       = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	var stages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer task-token" {
			t.Fatalf("Authorization = %q", got)
		}
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "secret-canary") || strings.Contains(string(raw), `"arguments"`) || strings.Contains(string(raw), `"output"`) {
			t.Fatalf("control-plane payload contained raw values: %s", raw)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/api/daemon/tasks/task/tool-invocations":
			stages = append(stages, "approval")
			_, _ = w.Write([]byte(`{"invocation_id":"` + invocationID + `","approval_request_id":"` + approvalID + `","status":"approved"}`))
		case request.URL.Path == "/api/daemon/tasks/task/tool-approvals/"+approvalID+"/consume":
			stages = append(stages, "consume")
			_, _ = w.Write([]byte(`{"authorized":true}`))
		case request.URL.Path == "/api/daemon/tasks/task/tool-invocations/"+invocationID+"/events":
			message, _ := body["task_message"].(map[string]any)
			if message["invocation_id"] != invocationID {
				t.Fatalf("task message invocation_id = %#v", message["invocation_id"])
			}
			if body["event_type"] == "started" {
				stages = append(stages, "started:"+message["type"].(string))
			} else {
				if body["policy_revision"] != float64(7) {
					t.Fatalf("terminal policy_revision = %#v", body["policy_revision"])
				}
				stages = append(stages, "terminal:"+message["type"].(string))
			}
			_, _ = w.Write([]byte(`{"committed":true}`))
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	task := Task{
		ID: "task", RemoteMCPDaemonToken: "task-token",
		ManagedMCPPolicy: &ManagedMCPPolicyData{
			Capability: managedMCPPreTransportCapability("claude"),
			Revision:   7,
			Rules: []ManagedMCPPolicyRuleData{{
				TransportKind: managedMCPTransportKind, ServerKey: "fixture",
				ToolName: "allowed", SchemaDigest: digest, Effect: "require_approval",
			}},
		},
	}
	gate := newTaskManagedMCPInvocationGate(client, task, "claude")
	grant, err := gate.Begin(context.Background(), remoteMCPInvocation{
		TaskID: "task", ProviderFamily: "claude", TransportKind: managedMCPTransportKind,
		ServerKey: "fixture", ToolName: "allowed", SchemaDigest: digest,
		ArgumentBytes: 31, IdempotencyKey: "mcp:fixed",
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := gate.Finish(context.Background(), grant, remoteMCPInvocationResult{
		OutcomeCode: "succeeded", ResultBytes: 42, DurationMS: 3,
	}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if got, want := strings.Join(stages, ","), "approval,consume,started:tool_use,terminal:tool_result"; got != want {
		t.Fatalf("control-plane order = %s, want %s", got, want)
	}
}

func TestTaskManagedMCPControlPlaneRejectsUnexpectedAllowResponseBeforeStartedCommit(t *testing.T) {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	startedCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(request.URL.Path, "/tool-invocations") {
			_, _ = w.Write([]byte(`{"invocation_id":"018f0000-0000-7000-8000-000000000001","status":"denied"}`))
			return
		}
		startedCalls++
		_, _ = w.Write([]byte(`{"committed":true}`))
	}))
	defer server.Close()

	gate := newTaskManagedMCPInvocationGate(NewClient(server.URL), Task{
		ID: "task", RemoteMCPDaemonToken: "task-token",
		ManagedMCPPolicy: &ManagedMCPPolicyData{
			Capability: managedMCPPreTransportCapability("claude"), Revision: 1,
			Rules: []ManagedMCPPolicyRuleData{{
				TransportKind: managedMCPTransportKind, ServerKey: "fixture", ToolName: "allowed",
				SchemaDigest: digest, Effect: "allow",
			}},
		},
	}, "claude")
	_, err := gate.Begin(context.Background(), remoteMCPInvocation{
		TaskID: "task", ProviderFamily: "claude", TransportKind: managedMCPTransportKind,
		ServerKey: "fixture", ToolName: "allowed", SchemaDigest: digest, IdempotencyKey: "mcp:fixed",
	})
	if !errors.Is(err, errRemoteMCPAuditFailure) {
		t.Fatalf("Begin error = %v, want audit failure", err)
	}
	if startedCalls != 0 {
		t.Fatalf("started commits = %d, want 0", startedCalls)
	}
}
