package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func TestConversationClaimOmitsIntegrationCapabilities(t *testing.T) {
	// Both feature flags must be ON: their default-off state must not mask a leak.
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.PluginsV1, featureflag.Rule{Default: true})
	provider.Set(featureflags.ComposioMCPApps, featureflag.Rule{Default: true})
	dbfx.Insert(t, "plugin_installation", testutil.Cols{
		"workspace_id": testWorkspaceID, "plugin_key": "com.example.toolbox", "package_version_id": testutil.Raw("gen_random_uuid()"), "version": "1.0.0",
		"manifest": mcpToolboxManifest, "granted_scopes": `["issues:read","net:tools.example.com"]`,
		"mcp_approvals": `{"toolbox":{"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}}`,
	})
	for _, lineage := range []string{"root", "delegation", "retry", "first-party"} {
		t.Run(lineage, func(t *testing.T) {
			f := newConversationFixture(t)
			f.msg.ReplyTo = nil
			f.ingest(t)
			f.h.FeatureFlags = featureflag.NewService(provider)
			root := f.task(t)
			task := root
			if lineage != "root" {
				cols := testutil.Cols{"runtime_id": f.runtime, "issue_id": f.issue, "originator_source": "delegation", "delegated_from_task_id": root.ID,
					"originator_user_id": testUserID, "accountable_user_id": testUserID}
				if lineage == "retry" {
					cols["originator_source"], cols["retry_of_task_id"], cols["delegated_from_task_id"] = "retry", root.ID, nil
				} else if lineage == "first-party" {
					cols["originator_source"], cols["delegated_from_task_id"] = "direct_human", nil
				}
				id := dbfx.Task(t, f.agent, cols)
				var err error
				task, err = f.h.Queries.GetAgentTask(context.Background(), parseUUID(id))
				if err != nil {
					t.Fatal(err)
				}
			}
			// Persisted overlays from older servers/retries must also be ignored.
			task.RuntimeMcpOverlay = []byte(`{"mcpServers":{"composio":{"url":"https://mcp.example/private-session"}}}`)
			task.RuntimeConnectedApps = []byte(`[{"provider":"composio","server_name":"composio","toolkit_slug":"notion","toolkit_name":"Notion"}]`)
			dbfx.Exec(t, `UPDATE agent SET mcp_config='{"mcpServers":{"base":{"command":"test-base-tool"}}}' WHERE id=$1`, f.agent)
			runtime, err := f.h.Queries.GetAgentRuntime(context.Background(), task.RuntimeID)
			if err != nil {
				t.Fatal(err)
			}
			resp, _, _, _, _, failure := f.h.buildClaimedTaskResponse(httptest.NewRequest("POST", "/claim", nil), &task, runtime, f.runtime, testWorkspaceID)
			if failure != nil || resp.Agent == nil {
				t.Fatalf("build claim failed: %+v", failure)
			}
			servers := decodeServers(t, resp.Agent.McpConfig)
			if servers["base"] == nil {
				t.Fatal("claim lost the agent's configured base MCP server")
			}
			if lineage == "first-party" {
				if len(resp.PluginHookTools) == 0 || len(resp.RemoteMCPConnections) == 0 || len(resp.ConnectedApps) == 0 || servers["composio"] == nil {
					t.Fatalf("first-party capabilities missing: %+v", resp)
				}
				if strings.Contains(resp.Agent.Instructions, conversationInstructions) {
					t.Fatal("ordinary task received external restrictions")
				}
			} else {
				if len(resp.PluginHookTools) != 0 || len(resp.RemoteMCPConnections) != 0 || len(resp.ConnectedApps) != 0 || servers["composio"] != nil {
					t.Fatalf("external task received integrations: %+v", resp)
				}
				if strings.Count(resp.Agent.Instructions, conversationInstructions) != 1 {
					t.Fatal("external root or descendant did not receive restrictions exactly once")
				}
			}
		})
	}
}

func TestConversationClaimOmitsFileDeliveryCapability(t *testing.T) {
	for _, external := range []bool{true, false} {
		name := "external"
		if !external {
			name = "first-party"
		}
		t.Run(name, func(t *testing.T) {
			f := newConversationFixture(t)
			f.msg.ReplyTo = nil
			f.ingest(t)
			task := f.task(t)
			if !external {
				id := dbfx.Task(t, f.agent, testutil.Cols{"runtime_id": f.runtime, "chat_session_id": task.ChatSessionID, "status": "running",
					"originator_source": "direct_human", "originator_user_id": testUserID, "accountable_user_id": testUserID})
				dbfx.InsertNoID(t, "channel_task_delivery", testutil.Cols{"task_id": id, "binding_id": f.binding, "installation_id": f.install,
					"channel_type": "feishu", "channel_chat_id": "oc_feedback", "chat_type": "p2p", "route_revision": 1, "config": `{}`}, "task_id=$1", id)
				var err error
				task, err = f.h.Queries.GetAgentTask(context.Background(), parseUUID(id))
				if err != nil {
					t.Fatal(err)
				}
			}
			// A default-off adapter must not mask a missing external-task guard.
			f.h.channelFileDelivery = nil
			f.h.DeclareChannelFileDelivery("feishu")
			runtime, err := f.h.Queries.GetAgentRuntime(context.Background(), task.RuntimeID)
			if err != nil {
				t.Fatal(err)
			}
			resp, _, _, _, _, failure := f.h.buildClaimedTaskResponse(httptest.NewRequest("POST", "/claim", nil), &task, runtime, f.runtime, testWorkspaceID)
			if failure != nil || resp.ChatChannelType != "feishu" || resp.ChatChannelDeliversFiles == external {
				t.Fatalf("claim file capability: external=%t type=%s files=%t failure=%+v", external, resp.ChatChannelType, resp.ChatChannelDeliversFiles, failure)
			}
		})
	}
}

func TestConversationDaemonPluginRoutesDenyExternalTasks(t *testing.T) {
	f := newConversationFixture(t)
	dbfx.Exec(t, "UPDATE agent_runtime SET daemon_id='conversation-capabilities' WHERE id=$1", f.runtime)
	f.msg.ReplyTo = nil
	f.ingest(t)
	withPluginsV1Flag(t, f.h, true)
	root := f.task(t)
	for _, lineage := range []string{"root", "delegation", "retry", "missing-root", "first-party"} {
		t.Run(lineage, func(t *testing.T) {
			id := uuidToString(root.ID)
			if lineage != "root" {
				cols := testutil.Cols{"runtime_id": f.runtime, "issue_id": f.issue, "originator_source": "delegation", "delegated_from_task_id": root.ID,
					"originator_user_id": testUserID, "accountable_user_id": testUserID}
				if lineage == "retry" {
					cols["originator_source"], cols["retry_of_task_id"], cols["delegated_from_task_id"] = "retry", root.ID, nil
				}
				if lineage == "first-party" || lineage == "missing-root" {
					cols["originator_source"], cols["delegated_from_task_id"] = "direct_human", nil
				}
				id = dbfx.Task(t, f.agent, cols)
				if lineage == "missing-root" {
					// An origin-only malformed row must never become first-party.
					// Stamp the source first, then remove only the root: the lineage
					// trigger runs on source changes and would otherwise repair it.
					dbfx.Exec(t, "UPDATE agent_task_queue SET originator_source='channel_integration' WHERE id=$1", id)
					dbfx.Exec(t, "UPDATE agent_task_queue SET conversation_root_task_id=NULL WHERE id=$1", id)
				}
			}
			want := http.StatusForbidden
			if lineage == "first-party" {
				want = http.StatusBadRequest // reaches the existing request validation
			}
			for name, handler := range map[string]http.HandlerFunc{"plugin-hooks": f.h.InvokeAgentPluginHook, "plugin-mcp": f.h.ResolvePluginMCPCredential} {
				req := newBoundDaemonTokenRequest(t, "POST", "/api/daemon/tasks/"+id+"/"+name, map[string]any{}, f.runtime)
				req = withURLParam(req, "id", id)
				testutil.Call(t, handler, req).Want(want)
			}
		})
	}
}
