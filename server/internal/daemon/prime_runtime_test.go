//go:build !windows

package daemon

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestPrimeBuiltinAdmissionAndExactVersion(t *testing.T) {
	fx := newBatchFixture(t)
	checkAgentMinVersion = agent.CheckMinVersion
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"prime": {Path: "/fake/prime-agent"}}

	t.Setenv("MULTICA_PRIME_AGENT_ISOLATED", "")
	runtimes, demotable, _ := d.detectBuiltinRuntimes(context.Background())
	if len(runtimes) != 0 || !strings.Contains(demotable["prime"].reason, "MULTICA_PRIME_AGENT_ISOLATED=1") {
		t.Fatalf("without attestation runtimes=%v demotable=%v", runtimes, demotable)
	}

	if os.Geteuid() == 0 {
		t.Skip("root is intentionally ineligible")
	}
	t.Setenv("MULTICA_PRIME_AGENT_ISOLATED", "1")
	fx.setProbeVersion("0.7.2")
	runtimes, demotable, _ = d.detectBuiltinRuntimes(context.Background())
	if len(runtimes) != 0 || !strings.Contains(demotable["prime"].reason, "no startup hard-disable") {
		t.Fatalf("upstream-blocked runtimes=%v demotable=%v", runtimes, demotable)
	}
}

func TestPrimeCustomProfileAdmission(t *testing.T) {
	fx := newBatchFixture(t)
	stubLookPath(t, map[string]string{"company-prime": "/opt/bin/company-prime"})
	fx.profiles["ws-1"] = []RuntimeProfile{{
		ID: "prime-profile", WorkspaceID: "ws-1", DisplayName: "Company Prime",
		ProtocolFamily: "prime", CommandName: "company-prime", Visibility: "workspace", Enabled: true,
	}}
	t.Setenv("MULTICA_PRIME_AGENT_ISOLATED", "")
	var runtimes, failed []map[string]string
	fx.daemon.appendProfileRuntimes(context.Background(), "ws-1", &runtimes, &failed)
	if len(runtimes) != 0 || len(failed) != 1 || !strings.Contains(failed[0]["reason"], "MULTICA_PRIME_AGENT_ISOLATED=1") {
		t.Fatalf("runtimes=%v failed=%v", runtimes, failed)
	}
	if os.Geteuid() != 0 {
		t.Setenv("MULTICA_PRIME_AGENT_ISOLATED", "1")
		runtimes, failed = nil, nil
		fx.daemon.appendProfileRuntimes(context.Background(), "ws-1", &runtimes, &failed)
		if len(runtimes) != 0 || len(failed) != 1 || !strings.Contains(failed[0]["reason"], "no startup hard-disable") {
			t.Fatalf("upstream-blocked custom profile runtimes=%v failed=%v", runtimes, failed)
		}
	}
}
