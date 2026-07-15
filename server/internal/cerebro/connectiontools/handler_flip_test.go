package connectiontools

// FIR-2441 (the Flip, slice 3): the local connection-tools handler moved off the
// api-only APIConnectionResolver.ListForAgent onto the unified
// ConnectionToolResolver.Resolve(...).APITools. The invariant this slice must
// preserve is "listed == callable": List and Call read the SAME resolver output,
// so a tool that shows up in the list is exactly the one Call can dispatch, and a
// listed-but-Ask tool is refused at Call time (there is no approval inbox on the
// local surface, and this fronts the secrets box).
//
// This test wires a REAL *runtime.ConnectionToolResolver from store-free fakes
// (its api sub-component is the real APIConnectionResolver, same as production)
// and drives the handler end to end:
//
//   - an Allow endpoint is listed AND dispatches server-side (200 with the body),
//   - an Ask endpoint is listed (with the approval note) but Call returns 403,
//   - a name absent from the resolved set is a 403 at Call.
//
// Because both List and Call go through allowedTools → Resolve(...).APITools, the
// three assertions together prove the two surfaces agree by construction.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	cerebroruntime "github.com/multica-ai/multica/server/internal/cerebro/runtime"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// --- store-free fakes satisfying the resolver's (unexported) seams -----------
// Go interface satisfaction is structural, so these concrete types can be passed
// where the runtime package expects its own unexported interfaces.

type flipConnLister struct{ conns []connections.Connection }

func (f flipConnLister) ListEnabled(ctx context.Context, workspaceID pgtype.UUID) ([]connections.Connection, error) {
	return f.conns, nil
}

type flipEndpointPolicy struct {
	// verdicts keyed "<conn> <METHOD> <path>"; a missing key resolves to Deny.
	verdicts map[string]toolpolicy.Setting
}

func (f flipEndpointPolicy) ConnectionToolEffective(_ context.Context, _, _, _, _ pgtype.UUID, _ string) (toolpolicy.Setting, string, error) {
	return toolpolicy.SettingDeny, "", nil
}

func (f flipEndpointPolicy) ConnectionEndpointEffective(ctx context.Context, workspaceID, runtimeID, agentID, userID, onBehalfOfID pgtype.UUID, connName, method, path string) (toolpolicy.Setting, string, error) {
	if s, ok := f.verdicts[connName+" "+method+" "+path]; ok {
		return s, connName, nil
	}
	return toolpolicy.SettingDeny, connName, nil
}

type flipFlag struct{ on bool }

func (f flipFlag) GetCerebroFeatureFlag(ctx context.Context, params cerebrodb.GetCerebroFeatureFlagParams) (bool, error) {
	return f.on, nil
}

// flipResolver builds a real unified resolver whose api half admits one Allow and
// one Ask endpoint on a connection pointed at srvURL (so the Allow tool can be
// dispatched for real). The mcp_http half is inert (nil verdicts) — the local
// handler only ever reads APITools.
func flipResolver(t *testing.T, srvURL string) *cerebroruntime.ConnectionToolResolver {
	t.Helper()
	conns := flipConnLister{conns: []connections.Connection{{
		Name: "c", Type: connections.TypeAPI, URL: srvURL, Enabled: true,
		EndpointPermissions: []connections.EndpointPermission{
			{Path: "/allow", Methods: []string{"GET"}},
			{Path: "/ask", Methods: []string{"GET"}},
		},
	}}}
	policy := flipEndpointPolicy{verdicts: map[string]toolpolicy.Setting{
		"c GET /allow": toolpolicy.SettingAllow,
		"c GET /ask":   toolpolicy.SettingAsk,
	}}
	flag := flipFlag{on: true}
	api := cerebroruntime.NewAPIConnectionResolver(conns, policy, flag, nil)
	return cerebroruntime.NewConnectionToolResolver(api, conns, nil, flag, nil, nil)
}

func flipHandler(t *testing.T, srvURL string) *Handler {
	t.Helper()
	res := &fakeResolver{
		wsID:    mustUUID(t, testWsID),
		runID:   mustUUID(t, testRunID),
		ownerID: mustUUID(t, testOwnerID),
	}
	return NewHandler(res, flipResolver(t, srvURL))
}

// listTools drives GET and returns the descriptors as a name→description map.
func listTools(t *testing.T, h *Handler) map[string]string {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/cerebro/connection-tools", nil)
	agentToken(r, testAgentID, testWsID)
	w := httptest.NewRecorder()
	h.List(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("List want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	out := map[string]string{}
	for _, d := range resp.Tools {
		out[d.Name] = d.Description
	}
	return out
}

// callTool drives POST /call for one tool name and returns (status, body).
func callTool(t *testing.T, h *Handler, tool string) (int, string) {
	t.Helper()
	body, _ := json.Marshal(callRequest{Tool: tool})
	r := httptest.NewRequest(http.MethodPost, "/api/cerebro/connection-tools/call", strings.NewReader(string(body)))
	agentToken(r, testAgentID, testWsID)
	w := httptest.NewRecorder()
	h.Call(w, r)
	return w.Code, w.Body.String()
}

func TestFlipSlice3_ListedEqualsCallable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/allow" {
			_, _ = w.Write([]byte("dispatched-ok"))
			return
		}
		// The Ask endpoint must never be reached — Call fails closed before dispatch.
		t.Errorf("upstream reached for path %q — an Ask tool must not dispatch", r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	h := flipHandler(t, srv.URL)

	// --- List: both endpoints surfaced through Resolve(...).APITools ----------
	tools := listTools(t, h)
	if len(tools) != 2 {
		t.Fatalf("want 2 listed tools (Allow + Ask), got %d: %v", len(tools), tools)
	}
	var allowName, askName string
	for name, desc := range tools {
		if strings.Contains(desc, "Requires human approval") {
			askName = name
		} else {
			allowName = name
		}
	}
	if allowName == "" || askName == "" {
		t.Fatalf("could not identify Allow/Ask tools from list: %v", tools)
	}

	// --- Call the Allow tool: it is the SAME tool listed, and it dispatches ----
	code, body := callTool(t, h, allowName)
	if code != http.StatusOK {
		t.Fatalf("Allow tool Call want 200, got %d (%s)", code, body)
	}
	var cr callResponse
	if err := json.Unmarshal([]byte(body), &cr); err != nil {
		t.Fatalf("decode call response: %v", err)
	}
	if cr.Result != "dispatched-ok" {
		t.Fatalf("Allow tool did not dispatch to upstream: result=%q", cr.Result)
	}

	// --- Call the Ask tool: listed, but refused at call (no local approval) ----
	code, body = callTool(t, h, askName)
	if code != http.StatusForbidden {
		t.Fatalf("Ask tool Call want 403 (fail closed), got %d (%s)", code, body)
	}
	if msg := decodeErr(t, body); !strings.Contains(msg, "human approval") {
		t.Fatalf("Ask tool 403 should cite human approval, got %q", msg)
	}

	// --- Call an unknown tool: not in the resolved set → 403 -------------------
	code, body = callTool(t, h, "c__get_nonexistent")
	if code != http.StatusForbidden {
		t.Fatalf("unknown tool Call want 403, got %d (%s)", code, body)
	}
}
