package driftwatch

import (
	"strings"
	"testing"
)

func TestUnpermittedCapabilities_DropsGovernedOnes(t *testing.T) {
	observed := []observedCapability{
		{Key: "bash", Name: "Bash", Status: statusAllowed, Uses: 30},
		{Key: "read", Name: "Read", Status: statusNeedsApproval, Uses: 12},
		{Key: "webfetch", Name: "WebFetch", Status: statusUnmapped, Uses: 9},
		{Key: "droptable", Name: "DropTable", Status: statusBlocked, Uses: 3},
	}

	got := unpermittedCapabilities(observed)

	if len(got) != 2 {
		t.Fatalf("want 2 unpermitted capabilities, got %d (%+v)", len(got), got)
	}
	if got[0].Name != "WebFetch" || got[1].Name != "DropTable" {
		t.Fatalf("unexpected selection: %+v", got)
	}
}

func TestUnpermittedCapabilities_KeepsOldFindings(t *testing.T) {
	// An ungoverned capability nobody actioned last night must stay in the
	// digest even though it is no longer new — otherwise the finding retires
	// itself without anyone deciding anything.
	observed := []observedCapability{
		{Key: "webfetch", Name: "WebFetch", Status: statusUnmapped, Uses: 4, IsNew: false},
	}
	if got := unpermittedCapabilities(observed); len(got) != 1 {
		t.Fatalf("a known-but-ungoverned capability must stay listed, got %+v", got)
	}
}

func TestBuildDigest_GroupsAgentsPerCapability(t *testing.T) {
	findings := []agentFindings{
		{
			AgentID:   "agent-1",
			AgentName: "Mia",
			Caps: []observedCapability{
				{Key: "webfetch", Name: "WebFetch", Status: statusUnmapped, Uses: 5, IsNew: true},
				{Key: "droptable", Name: "DropTable", Status: statusBlocked, Uses: 2},
			},
		},
		{
			AgentID:   "agent-2",
			AgentName: "Rasp",
			Caps: []observedCapability{
				{Key: "webfetch", Name: "WebFetch", Status: statusUnmapped, Uses: 7},
			},
		},
	}

	rows := buildDigest(findings)

	if len(rows) != 2 {
		t.Fatalf("want 2 digest rows, got %d (%+v)", len(rows), rows)
	}
	// Blocked sorts first even though it has fewer uses.
	if rows[0].Name != "DropTable" || rows[0].Status != statusBlocked {
		t.Fatalf("blocked row must sort first, got %+v", rows[0])
	}
	web := rows[1]
	if web.Name != "WebFetch" || len(web.Agents) != 2 {
		t.Fatalf("WebFetch must fold both agents into one row: %+v", web)
	}
	if web.Uses != 12 {
		t.Fatalf("uses must sum across agents, got %d", web.Uses)
	}
	if !web.New {
		t.Fatalf("row must be marked new when any agent hit it for the first time")
	}
	// Agents sort by uses desc.
	if web.Agents[0].Name != "Rasp" || web.Agents[1].Name != "Mia" {
		t.Fatalf("agents must sort by uses desc: %+v", web.Agents)
	}
}

func TestBuildDigest_KeepsBlockedAndUnmappedApart(t *testing.T) {
	// The same tool can be unmapped for one agent and denied-but-used for
	// another. Collapsing them would hide the enforcement failure.
	findings := []agentFindings{
		{AgentID: "a", AgentName: "A", Caps: []observedCapability{
			{Key: "bash", Name: "Bash", Status: statusUnmapped, Uses: 4},
		}},
		{AgentID: "b", AgentName: "B", Caps: []observedCapability{
			{Key: "bash", Name: "Bash", Status: statusBlocked, Uses: 1},
		}},
	}

	rows := buildDigest(findings)

	if len(rows) != 2 {
		t.Fatalf("unmapped and blocked must stay separate rows, got %+v", rows)
	}
	if rows[0].Status != statusBlocked || rows[1].Status != statusUnmapped {
		t.Fatalf("unexpected row order/status: %+v", rows)
	}
}

func TestBuildDigest_EmptyInput(t *testing.T) {
	if rows := buildDigest(nil); len(rows) != 0 {
		t.Fatalf("empty input must produce no rows, got %+v", rows)
	}
}

func TestDigestSignature_ChangesWithAgentSetAndIsOrderInsensitive(t *testing.T) {
	base := []DigestRow{
		{Key: "webfetch", Status: statusUnmapped, Agents: []DigestAgent{{ID: "a"}, {ID: "b"}}},
		{Key: "droptable", Status: statusBlocked, Agents: []DigestAgent{{ID: "a"}}},
	}
	reordered := []DigestRow{
		{Key: "droptable", Status: statusBlocked, Agents: []DigestAgent{{ID: "a"}}},
		{Key: "webfetch", Status: statusUnmapped, Agents: []DigestAgent{{ID: "b"}, {ID: "a"}}},
	}
	if digestSignature(base) != digestSignature(reordered) {
		t.Fatalf("signature must be order-insensitive")
	}

	extraAgent := []DigestRow{
		{Key: "webfetch", Status: statusUnmapped, Agents: []DigestAgent{{ID: "a"}, {ID: "b"}, {ID: "c"}}},
		{Key: "droptable", Status: statusBlocked, Agents: []DigestAgent{{ID: "a"}}},
	}
	if digestSignature(base) == digestSignature(extraAgent) {
		t.Fatalf("a new agent on an existing capability must change the signature")
	}
}

func TestSignatureRoundTrip(t *testing.T) {
	body := embedDigestSignature("## Body\n\ntext\n", "abc123")
	if got := extractDigestSignature(body); got != "abc123" {
		t.Fatalf("signature round-trip failed, got %q", got)
	}
	if got := extractDigestSignature("a human rewrote this body"); got != "" {
		t.Fatalf("missing marker must read as empty (treat as changed), got %q", got)
	}
}

func TestRecommendedAction(t *testing.T) {
	blocked := DigestRow{Status: statusBlocked}
	if !strings.Contains(recommendedAction(blocked, false), "enforcement") {
		t.Fatalf("blocked action must point at enforcement: %q", recommendedAction(blocked, false))
	}
	unmapped := DigestRow{Status: statusUnmapped}
	if !strings.Contains(recommendedAction(unmapped, false), "No rule exists") {
		t.Fatalf("unmapped without auto-permission: %q", recommendedAction(unmapped, false))
	}
	if !strings.Contains(recommendedAction(unmapped, true), "Allow") {
		t.Fatalf("unmapped with auto-permission must say a rule was created: %q", recommendedAction(unmapped, true))
	}
}

func TestFormatDigestAgents_CapsAndDedupes(t *testing.T) {
	agents := []DigestAgent{
		{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D"}, {Name: "E"}, {Name: "A"},
	}
	got := formatDigestAgents(agents)
	if !strings.HasPrefix(got, "A, B, C, D") || !strings.Contains(got, "+1 more") {
		t.Fatalf("unexpected agents cell: %q", got)
	}

	if got := formatDigestAgents([]DigestAgent{{Name: ""}}); got != "Unnamed agent" {
		t.Fatalf("blank agent name must not render empty, got %q", got)
	}
}

func TestRenderDigestBody_HasThreeRequestedColumns(t *testing.T) {
	rows := []DigestRow{
		{Key: "webfetch", Name: "WebFetch", Status: statusUnmapped, Uses: 12, New: true,
			Agents: []DigestAgent{{ID: "a", Name: "Mia", Uses: 7}, {ID: "b", Name: "Rasp", Uses: 5}}},
	}

	body := renderDigestBody(rows, "2026-07-28T00:00:00Z", "2026-07-29T00:00:00Z", false)

	for _, want := range []string{"| Capability |", "| Agents |", "| Recommended action |", "WebFetch", "Mia, Rasp", "No rule exists"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "ⁿᵉʷ") {
		t.Fatalf("new capabilities must be marked in the table:\n%s", body)
	}
}

func TestRenderDigestBody_ReportsTruncation(t *testing.T) {
	rows := make([]DigestRow, digestTableLimit+3)
	for i := range rows {
		rows[i] = DigestRow{Key: "t", Name: "Tool", Status: statusUnmapped, Uses: 1}
	}

	body := renderDigestBody(rows, "a", "b", false)

	if !strings.Contains(body, "And 3 more not shown") {
		t.Fatalf("truncation must be stated, never silent:\n%s", body)
	}
}

func TestRenderDigestNotificationBody_StatesAutoCreatedRules(t *testing.T) {
	rows := []DigestRow{
		{Key: "webfetch", Name: "WebFetch", Status: statusUnmapped, Uses: 12,
			Agents: []DigestAgent{{ID: "a", Name: "Mia", Uses: 12}}},
		{Key: "bash", Name: "Bash", Status: statusUnmapped, Uses: 3,
			Agents: []DigestAgent{{ID: "a", Name: "Mia", Uses: 3}}},
	}

	// A write done on the reader's behalf must be on the card itself, not only
	// inside the issue the reader has to open first.
	withRules := renderDigestNotificationBody(rows, 1, 2)
	for _, want := range []string{"2 capabilities", "1 agent ", "2 rules were created automatically", "set to allow", "nothing changed about what the agents can do"} {
		if !strings.Contains(withRules, want) {
			t.Fatalf("notification body missing %q:\n%s", want, withRules)
		}
	}

	if got := renderDigestNotificationBody(rows, 1, 1); !strings.Contains(got, "1 rule was created automatically") {
		t.Fatalf("single rule must read in the singular:\n%s", got)
	}

	// Auto-permission off writes nothing, so there is nothing to disclose.
	withoutRules := renderDigestNotificationBody(rows, 1, 0)
	if strings.Contains(withoutRules, "created automatically") {
		t.Fatalf("no rules written means no rule sentence:\n%s", withoutRules)
	}
}

func TestUnmappedCapabilityKeys_ExcludesBlocked(t *testing.T) {
	caps := []observedCapability{
		{Key: "webfetch", Status: statusUnmapped},
		{Key: "webfetch", Status: statusUnmapped}, // duplicate
		{Key: "droptable", Status: statusBlocked}, // already has a deny row
		{Key: "", Status: statusUnmapped},         // unusable key
	}

	got := unmappedCapabilityKeys(caps)

	if len(got) != 1 || got[0] != "webfetch" {
		t.Fatalf("want only webfetch, got %+v", got)
	}
}

func TestDigestTitle_CarriesCount(t *testing.T) {
	if got := digestTitle([]DigestRow{{}, {}}); !strings.Contains(got, "(2)") {
		t.Fatalf("title must carry the count, got %q", got)
	}
}
