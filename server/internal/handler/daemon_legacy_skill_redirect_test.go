package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestClaimTaskByRuntime_LegacySkillRedirectFollowsTheCapability covers the
// wiring the two end-to-end tests leave out (MUL-6986).
//
// The service test drives the decision by passing a bool straight in, and the
// brief test hand-builds a skill set. Both ends were covered while the middle —
// X-Client-Capabilities on the claim request, through the handler, into the
// payload the daemon actually receives — was not. If that link ever breaks, the
// other two tests stay green: an old daemon would silently lose its redirect
// and its brief would point at a skill nobody shipped, or every current daemon
// would start paying for a stub it does not need.
//
// Both claim branches are covered because they compute the built-in set
// separately: the slim skill-refs claim and the older inline-skills claim.
func TestClaimTaskByRuntime_LegacySkillRedirectFollowsTheCapability(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	const legacy = "multica-working-on-issues"

	tests := []struct {
		name         string
		fixture      string
		capabilities string
		wantRedirect bool
	}{
		{
			// A daemon released before the merge: its brief still names the old
			// skill, so the server has to ship something under that name.
			name:         "inline claim without the capability",
			fixture:      "legacyredirinline",
			capabilities: "",
			wantRedirect: true,
		},
		{
			name:         "skill-refs claim without the capability",
			fixture:      "legacyredirrefs",
			capabilities: protocol.DaemonCapabilitySkillBundlesV1,
			wantRedirect: true,
		},
		{
			// A current daemon names multica-platform itself and must not be
			// charged for the stub.
			name:         "inline claim with the capability",
			fixture:      "legacyredirinlinenew",
			capabilities: protocol.DaemonCapabilityPlatformSkillV1,
			wantRedirect: false,
		},
		{
			name:         "skill-refs claim with the capability",
			fixture:      "legacyredirrefsnew",
			capabilities: protocol.DaemonCapabilitySkillBundlesV1 + "," + protocol.DaemonCapabilityPlatformSkillV1,
			wantRedirect: false,
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

			// Whichever branch ran must have delivered the merged skill; the
			// redirect only ever adds to that, it never stands in for it.
			if !names[service.PlatformSkillName] {
				t.Errorf("claim payload is missing %q; names=%v", service.PlatformSkillName, names)
			}
			if got := names[legacy]; got != tc.wantRedirect {
				t.Errorf("legacy redirect %q present = %v, want %v; names=%v", legacy, got, tc.wantRedirect, names)
			}
		})
	}
}
