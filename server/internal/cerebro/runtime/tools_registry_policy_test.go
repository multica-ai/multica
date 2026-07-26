package runtime

// FIR-1512 Step 1 — unit tests for Registry.GetToolPolicyEnabledToolsForAgent,
// the engine-flip that builds an agent's tool list from the unified per-tool
// permission chain (Allow/Ask/Deny) instead of the legacy grant cascade.
//
// These are pure tests: the chain resolution is exercised through a fake
// toolPolicyResolver so the build path is verified without a database. The
// real DB-backed resolver (*toolpolicy.Store) is covered by the toolpolicy
// package's own tests; here we only assert the registry's list-building rules:
// Allow and Ask surface a tool, Deny hides it, and a resolve error fails closed
// (excludes that one tool rather than leaking it).

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// fakeToolPolicyResolver resolves each tool key from a static map. A key absent
// from verdicts resolves to Allow (mirroring the chain's default-Allow base);
// a key present in errs returns an error instead.
type fakeToolPolicyResolver struct {
	verdicts map[string]toolpolicy.Setting
	errs     map[string]error
}

func (f fakeToolPolicyResolver) Resolve(_ context.Context, q toolpolicy.Query) (toolpolicy.Effective, error) {
	if err, ok := f.errs[q.ToolKey]; ok {
		return toolpolicy.Effective{}, err
	}
	s, ok := f.verdicts[q.ToolKey]
	if !ok {
		s = toolpolicy.SettingAllow
	}
	return toolpolicy.Effective{Setting: s}, nil
}

func sortedToolNames(tools []Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name())
	}
	sort.Strings(out)
	return out
}

func newPolicyTestRegistry(names ...string) *Registry {
	r := NewRegistry(nil)
	for _, n := range names {
		r.Register(stubTool{name: n})
	}
	return r
}

func TestGetToolPolicyEnabledToolsForAgent_AllowAndAskSurface_DenyHidden(t *testing.T) {
	reg := newPolicyTestRegistry("lookup_order", "draft_reply", "issue_refund")
	resolver := fakeToolPolicyResolver{
		verdicts: map[string]toolpolicy.Setting{
			"lookup_order": toolpolicy.SettingAllow,
			"draft_reply":  toolpolicy.SettingAsk,  // Ask still surfaces — it only pauses at call time.
			"issue_refund": toolpolicy.SettingDeny, // Deny is the only verdict that hides a tool.
		},
	}

	got := reg.GetToolPolicyEnabledToolsForAgent(context.Background(), resolver,
		pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{})

	want := []string{"draft_reply", "lookup_order"}
	if names := sortedToolNames(got); !equalStrings(names, want) {
		t.Fatalf("expected %v, got %v", want, names)
	}
}

func TestGetToolPolicyEnabledToolsForAgent_NoRowsDefaultsAllow(t *testing.T) {
	// An empty verdict map means no explicit policy rows; the chain's default
	// Allow base surfaces every registered tool. The default-deny posture is
	// authored as rows, not assumed by the engine.
	reg := newPolicyTestRegistry("a", "b", "c")
	resolver := fakeToolPolicyResolver{}

	got := reg.GetToolPolicyEnabledToolsForAgent(context.Background(), resolver,
		pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{})

	if names := sortedToolNames(got); !equalStrings(names, []string{"a", "b", "c"}) {
		t.Fatalf("expected all tools, got %v", names)
	}
}

func TestGetToolPolicyEnabledToolsForAgent_ResolveErrorFailsClosed(t *testing.T) {
	// A resolve error for one tool excludes only that tool — the rest still
	// resolve normally. The failing tool must not leak into the toolset.
	reg := newPolicyTestRegistry("safe", "broken")
	resolver := fakeToolPolicyResolver{
		errs: map[string]error{"broken": errors.New("db down")},
	}

	got := reg.GetToolPolicyEnabledToolsForAgent(context.Background(), resolver,
		pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{})

	if names := sortedToolNames(got); !equalStrings(names, []string{"safe"}) {
		t.Fatalf("expected only [safe] (broken excluded), got %v", names)
	}
}

func TestGetToolPolicyEnabledToolsForAgent_NilResolver(t *testing.T) {
	reg := newPolicyTestRegistry("a")
	if got := reg.GetToolPolicyEnabledToolsForAgent(context.Background(), nil,
		pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}); got != nil {
		t.Fatalf("nil resolver should return nil, got %v", sortedToolNames(got))
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
