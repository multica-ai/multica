package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestClaimTaskByRuntime_OnlyCurrentBuiltinSkills verifies that both inline
// and bundle-ref claims ship the platform skill without the retired redirect,
// regardless of whether the daemon advertises platform-skill-v1.
func TestClaimTaskByRuntime_OnlyCurrentBuiltinSkills(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	const legacy = "multica-working-on-issues"

	tests := []struct {
		name         string
		fixture      string
		capabilities string
	}{
		{
			name:         "inline claim without the capability",
			fixture:      "builtininlineold",
			capabilities: "",
		},
		{
			name:         "skill-refs claim without the capability",
			fixture:      "builtinrefsold",
			capabilities: protocol.DaemonCapabilitySkillBundlesV1,
		},
		{
			name:         "inline claim with the capability",
			fixture:      "builtininlinenew",
			capabilities: protocol.DaemonCapabilityPlatformSkillV1,
		},
		{
			name:         "skill-refs claim with the capability",
			fixture:      "builtinrefsnew",
			capabilities: protocol.DaemonCapabilitySkillBundlesV1 + "," + protocol.DaemonCapabilityPlatformSkillV1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			runtimeID, _, _ := seedSkillLoadFixture(t, ctx, tc.fixture)

			req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, tc.fixture+"-daemon")
			if tc.capabilities != "" {
				req.Header.Set("X-Client-Capabilities", tc.capabilities)
			}
			req = withURLParam(req, "runtimeId", runtimeID)

			var resp struct {
				Task struct {
					Agent *struct {
						Skills    []service.AgentSkillData    `json:"skills"`
						SkillRefs []service.AgentSkillRefData `json:"skill_refs"`
					} `json:"agent"`
				} `json:"task"`
			}
			testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&resp)
			agent := resp.Task.Agent
			if agent == nil {
				t.Fatal("claim returned no agent payload")
			}
			if len(agent.Skills) == 0 && len(agent.SkillRefs) == 0 {
				t.Fatal("claim delivered neither inline skills nor skill refs")
			}

			names := map[string]bool{}
			for _, s := range agent.Skills {
				names[s.Name] = true
			}
			for _, r := range agent.SkillRefs {
				names[r.Name] = true
			}

			if !names[service.PlatformSkillName] {
				t.Errorf("claim payload is missing %q; names=%v", service.PlatformSkillName, names)
			}
			if names[legacy] {
				t.Errorf("claim payload still includes retired redirect %q", legacy)
			}
		})
	}
}
