package note

// FIR-1317 — the AI merge endpoint shipped in #1924 with no test coverage at
// all. Conflict DETECTION is pinned by update_conflict_db_test.go; what was
// never pinned is the half that actually combines the two versions once a 409
// has fired. These tests cover the gateway contract (both response shapes the
// gateway can return, auth headers, both versions reaching the prompt, error
// handling) and the handler shortcuts that must never spend a model call.
//
// The gateway tests use a local httptest server, so they run in CI without a
// model, a network, or FIRTAL_REGISTRY_* being set.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// --- extractMergeText -------------------------------------------------------

// The gateway fronts several providers. OpenAI-compatible responses put a plain
// string in message.content; Anthropic-compatible ones put an array of typed
// blocks. Both must yield the merged text, and anything else must degrade to ""
// rather than panicking or leaking raw JSON into the user's note.
func TestExtractMergeText(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"openai plain string", `"merged text"`, "merged text"},
		{"anthropic text blocks", `[{"type":"text","text":"one "},{"type":"text","text":"two"}]`, "one two"},
		{"block without type still counts", `[{"text":"bare"}]`, "bare"},
		{"non-text blocks are skipped", `[{"type":"thinking","text":"ignore"},{"type":"text","text":"keep"}]`, "keep"},
		{"empty array", `[]`, ""},
		{"empty raw", ``, ""},
		{"unexpected object shape", `{"foo":"bar"}`, ""},
		{"number", `42`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractMergeText(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Fatalf("extractMergeText(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// --- callMergeGateway -------------------------------------------------------

// fakeGateway stands in for the Firtal AI gateway. It records the request it
// received so the test can assert on what we actually sent.
type fakeGateway struct {
	srv        *httptest.Server
	gotPath    string
	gotAuth    string
	gotTrace   string
	gotPayload map[string]any
}

func newFakeGateway(t *testing.T, status int, respBody string) *fakeGateway {
	t.Helper()
	fg := &fakeGateway{}
	fg.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fg.gotPath = r.URL.Path
		fg.gotAuth = r.Header.Get("Authorization")
		fg.gotTrace = r.Header.Get("x-trace-name")
		_ = json.NewDecoder(r.Body).Decode(&fg.gotPayload)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(fg.srv.Close)
	return fg
}

// The happy path: an OpenAI-shaped answer comes back as the merged string, and
// the request carried the bearer token, the right path, and BOTH versions —
// a merge that only sees one side silently discards someone's edit.
func TestCallMergeGatewayHappyPath(t *testing.T) {
	fg := newFakeGateway(t, http.StatusOK,
		`{"choices":[{"message":{"content":"line one\nline two\nline three"}}]}`)

	got, err := callMergeGateway(context.Background(), fg.srv.URL, "test-key",
		"line one\nline two", "line one\nline three")
	if err != nil {
		t.Fatalf("callMergeGateway: %v", err)
	}
	if got != "line one\nline two\nline three" {
		t.Fatalf("merged = %q, want the gateway's content verbatim", got)
	}

	if fg.gotPath != mergeGatewayPath {
		t.Fatalf("path = %q, want %q", fg.gotPath, mergeGatewayPath)
	}
	if fg.gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization = %q, want bearer token", fg.gotAuth)
	}
	if fg.gotTrace != "cerebro-note-merge" {
		t.Fatalf("x-trace-name = %q — gateway cost attribution would be wrong", fg.gotTrace)
	}
	if model, _ := fg.gotPayload["model"].(string); model != mergeModel {
		t.Fatalf("model = %q, want %q", model, mergeModel)
	}

	msgs, _ := fg.gotPayload["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want system + user", len(msgs))
	}
	user, _ := msgs[1].(map[string]any)
	userContent, _ := user["content"].(string)
	if !strings.Contains(userContent, "line one\nline two") {
		t.Fatalf("prompt is missing YOUR version — that edit would be dropped:\n%s", userContent)
	}
	if !strings.Contains(userContent, "line one\nline three") {
		t.Fatalf("prompt is missing THEIR version — that edit would be dropped:\n%s", userContent)
	}
}

// The gateway may answer in Anthropic block form depending on which provider
// it routes to. That must merge identically, not come back blank.
func TestCallMergeGatewayAnthropicBlockShape(t *testing.T) {
	fg := newFakeGateway(t, http.StatusOK,
		`{"choices":[{"message":{"content":[{"type":"text","text":"merged body"}]}}]}`)

	got, err := callMergeGateway(context.Background(), fg.srv.URL, "k", "mine", "theirs")
	if err != nil {
		t.Fatalf("callMergeGateway: %v", err)
	}
	if got != "merged body" {
		t.Fatalf("merged = %q, want %q", got, "merged body")
	}
}

// A gateway error must surface as an error so the handler can answer 503 and
// the dialog falls back to "pick one version" — never as an empty merge that
// would blank the note.
func TestCallMergeGatewayNon2xxIsAnError(t *testing.T) {
	fg := newFakeGateway(t, http.StatusInternalServerError, `{"error":"boom"}`)

	got, err := callMergeGateway(context.Background(), fg.srv.URL, "k", "mine", "theirs")
	if err == nil {
		t.Fatalf("gateway 500 returned no error (merged=%q) — the dialog would show an empty merge", got)
	}
	if got != "" {
		t.Fatalf("merged = %q on error, want empty", got)
	}
}

// A 200 with no choices must not be treated as a successful empty merge.
func TestCallMergeGatewayEmptyChoicesYieldsNoMergedText(t *testing.T) {
	fg := newFakeGateway(t, http.StatusOK, `{"choices":[]}`)

	got, _ := callMergeGateway(context.Background(), fg.srv.URL, "k", "mine", "theirs")
	if got != "" {
		t.Fatalf("merged = %q, want empty when the gateway returned no choices", got)
	}
}

// --- MergeNote handler shortcuts -------------------------------------------

// mergeRequestFor drives MergeNote as userA against the given note.
func mergeRequestFor(t *testing.T, noteID pgtype.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/notes/"+uuidStr(noteID)+"/merge", strings.NewReader(body))
	r.Header.Set("X-User-ID", uuidStr(w3UserA))
	ctx := middleware.SetMemberContext(r.Context(), uuidStr(w3WsID), db.Member{})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuidStr(noteID))
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	w := httptest.NewRecorder()
	w3H.MergeNote(w, r.WithContext(ctx))
	return w
}

// Two shortcuts must answer without ever reaching the gateway: identical
// versions (nothing to merge) and two empty versions. Both are cheap wins —
// if they regressed we would pay for a model call on every no-op conflict.
// The third case pins that a REAL merge with no gateway configured degrades to
// 503 (dialog falls back to manual pick) rather than silently returning "".
func TestMergeNoteShortcutsAvoidTheGateway(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "Merge shortcuts", "base")

	// Guarantee that any gateway call in this test would fail loudly rather
	// than hitting a real endpoint.
	t.Setenv("FIRTAL_REGISTRY_URL", "")
	t.Setenv("FIRTAL_REGISTRY_KEY", "")

	// Identical versions → echo the body back, no model call.
	payload, _ := json.Marshal(mergeRequest{YourBody: "same text", ServerBody: "same text"})
	w := mergeRequestFor(t, noteID, string(payload))
	if w.Code != http.StatusOK {
		t.Fatalf("identical bodies: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp mergeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Merged != "same text" {
		t.Fatalf("identical bodies merged to %q, want the original text", resp.Merged)
	}

	// Both empty → empty merge, no model call.
	payload, _ = json.Marshal(mergeRequest{YourBody: "", ServerBody: ""})
	w = mergeRequestFor(t, noteID, string(payload))
	if w.Code != http.StatusOK {
		t.Fatalf("empty bodies: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	// A real merge with no gateway configured → 503, so the dialog can offer
	// the manual "keep mine / keep theirs" fallback.
	payload, _ = json.Marshal(mergeRequest{YourBody: "mine", ServerBody: "theirs"})
	w = mergeRequestFor(t, noteID, string(payload))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured gateway: got %d, want 503 (body: %s)", w.Code, w.Body.String())
	}
}
