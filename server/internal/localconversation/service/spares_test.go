package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
	"github.com/multica-ai/multica/server/internal/localconversation/provider/codex"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
	"github.com/multica-ai/multica/server/pkg/codexappserver"
)

// spareAdapter is an adapter that announces spare support unless told not to.
type spareAdapter struct {
	*fakeAdapter
	unsupported bool
}

func (a *spareAdapter) Supports(capability string) bool {
	return !a.unsupported && (capability == claude.CapabilityPrewarm || capability == claude.CapabilityClaim)
}

// spareWorld is a service with fake Claude and Codex spares.
type spareWorld struct {
	*focusFixture
	mu          sync.Mutex
	conditions  SpareConditions
	claudeReply func(a *spareAdapter, typ string) (json.RawMessage, error)
	claudeStart func(ctx context.Context) error
	unsupported bool
	spareClaude []*spareAdapter

	codexStart func() error
	codexSetup func(i int, s *fakeCodex)
	spareCodex []*fakeCodex
	codexEnv   [][]string
	coldCodex  *codexProcesses
	timing     string
}

func newSpareWorld(t *testing.T, providers ...string) *spareWorld {
	t.Helper()
	w := &spareWorld{conditions: SpareConditions{Ready: true, Executable: "/bin/cli", Binary: "/bin/cli\x001.0", Identity: "user"}, coldCodex: &codexProcesses{}}
	w.timing = timingPath(t)
	opts := Options{TimingPath: w.timing, Codex: func(_ context.Context, conv sqlitedb.Conversation) (CodexSession, StartCodexServer, error) {
		return CodexSession{Cwd: conv.Cwd}, w.coldCodex.start, nil
	}}
	read := func(context.Context) SpareConditions {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.conditions
	}
	if slices.Contains(providers, "claude") {
		opts.ClaudeSpare = &ClaudeSpareLauncher{Conditions: read, Fields: func(c SpareConditions) map[string]any {
			return map[string]any{"executable": c.Executable, "allowed_tools": []string{"mcp__multica__*"}}
		}, Start: func(ctx context.Context, h claude.Handler) (ClaudeAdapter, error) {
			w.mu.Lock()
			defer w.mu.Unlock()
			if w.claudeStart != nil {
				if err := w.claudeStart(ctx); err != nil {
					return nil, err
				}
			}
			a := &spareAdapter{fakeAdapter: &fakeAdapter{handler: h, done: make(chan struct{})}, unsupported: w.unsupported}
			if reply := w.claudeReply; reply != nil {
				a.reply = func(typ string) (json.RawMessage, error) { return reply(a, typ) }
			}
			w.spareClaude = append(w.spareClaude, a)
			return a, nil
		}}
	}
	if slices.Contains(providers, "codex") {
		opts.CodexSpare = &CodexSpareLauncher{Conditions: read, Start: func(_ context.Context, _ SpareConditions, h codex.Handler, env []string) (CodexServer, error) {
			w.mu.Lock()
			defer w.mu.Unlock()
			if w.codexStart != nil {
				if err := w.codexStart(); err != nil {
					return nil, err
				}
			}
			s := &fakeCodex{params: map[string]map[string]any{}, done: make(chan struct{}), handler: h}
			if w.codexSetup != nil {
				w.codexSetup(len(w.spareCodex), s)
			}
			w.spareCodex = append(w.spareCodex, s)
			w.codexEnv = append(w.codexEnv, env)
			return s, nil
		}}
	}
	w.focusFixture = newFocusFixture(t, opts)
	return w
}

func (w *spareWorld) claudeCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.spareClaude)
}

func (w *spareWorld) claudeSpare(i int) *spareAdapter {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.spareClaude[i]
}

func (w *spareWorld) codexCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.spareCodex)
}

func (w *spareWorld) codexSpare(i int) *fakeCodex {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.spareCodex[i]
}

func (w *spareWorld) parked(provider string) bool {
	slot := w.svc.spares.slots[provider]
	slot.mu.Lock()
	defer slot.mu.Unlock()
	return slot.spare != nil
}

func (w *spareWorld) setConditions(change func(*SpareConditions)) {
	w.mu.Lock()
	change(&w.conditions)
	w.mu.Unlock()
}

// fill connects a client and waits until the provider's spare is parked.
func (w *spareWorld) fill(t *testing.T, provider string) {
	t.Helper()
	gone := w.svc.ClientConnected("client")
	t.Cleanup(gone)
	w.svc.spares.check()
	eventually(t, "spare of "+provider, func() bool { return w.parked(provider) })
}

func (w *spareWorld) newConversation(t *testing.T, id, provider string) {
	t.Helper()
	if _, err := w.svc.Store().CreateConversation(context.Background(), store.NewConversation{ID: id, ScopeID: w.scopeID, Provider: provider, Cwd: "/repo", CreatedAt: w.clock.Now()}); err != nil {
		t.Fatal(err)
	}
}

func (w *spareWorld) send(t *testing.T, id, request string) {
	t.Helper()
	if _, err := w.svc.Send(context.Background(), SendCommand{ScopeID: w.scopeID, ConversationID: id, RequestID: request, Text: "go"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func fieldsOf(a *fakeAdapter, typ string) map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, sent := range a.requests {
		if sent == typ {
			return a.fields[i]
		}
	}
	return nil
}

func isDone(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func TestSparesOnlyWithAClientAndAReadyProvider(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "claude", "codex")
	w.svc.spares.check()
	if w.claudeCount() != 0 || w.codexCount() != 0 {
		t.Fatal("spares started with nobody connected")
	}
	w.setConditions(func(c *SpareConditions) { c.Ready = false })
	gone := w.svc.ClientConnected("client")
	defer gone()
	w.svc.spares.check()
	time.Sleep(10 * time.Millisecond)
	if w.claudeCount() != 0 || w.codexCount() != 0 {
		t.Fatal("spares started for a provider that is not ready")
	}
	w.setConditions(func(c *SpareConditions) { c.Ready = true })
	w.clock.advance(spareCheckInterval)
	w.svc.spares.check()
	eventually(t, "both spares", func() bool { return w.parked("claude") && w.parked("codex") })
	if got := fmt.Sprint(w.claudeSpare(0).sent()); got != "[prewarm]" {
		t.Fatalf("requests = %s", got)
	}
	fields := fieldsOf(w.claudeSpare(0).fakeAdapter, "prewarm")
	if fields["executable"] != "/bin/cli" || fields["cwd"] != nil || fields["mcp_servers"] != nil {
		t.Fatalf("prewarm fields = %v", fields)
	}
	if env := fmt.Sprint(w.codexEnv[0]); !strings.Contains(env, ContextTokenEnv+"=") {
		t.Fatalf("codex env = %s", env)
	}
	// One each, however often it is checked.
	w.svc.spares.check()
	if w.claudeCount() != 1 || w.codexCount() != 1 {
		t.Fatalf("%d claude, %d codex spares", w.claudeCount(), w.codexCount())
	}
}

func TestClaudeNewConversationClaimsTheSpare(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "claude")
	w.fill(t, "claude")
	w.newConversation(t, "new", "claude")
	model, effort, mode := "claude-opus-5-5", "high", "full-access"
	if err := w.svc.UpdateSettings(context.Background(), w.scopeID, "new", SettingsUpdate{Model: &model, ReasoningEffort: &effort, PermissionMode: &mode}); err != nil {
		t.Fatal(err)
	}
	w.send(t, "new", "r1")
	a := w.claudeSpare(0)
	if got := fmt.Sprint(a.sent()); got != "[prewarm claim set_mcp_servers send]" {
		t.Fatalf("requests = %s", got)
	}
	claim := fieldsOf(a.fakeAdapter, "claim")
	if claim["cwd"] != "/repo" || claim["model"] != model || claim["permission_mode"] != mode || fmt.Sprint(claim["settings"]) != "map[effortLevel:high]" || claim["append_system_prompt"] == "" {
		t.Fatalf("claim = %v", claim)
	}
	if w.adapterCount() != 0 {
		t.Fatal("a cold adapter started too")
	}
	eventually(t, "refill", func() bool { w.svc.spares.check(); return w.parked("claude") })
	records := timingRecords(t, w.svc, w.timing)
	found := false
	for _, r := range records {
		if r.TurnID == "turn-r1" && r.Stage == timing.StageSpareClaimed {
			found = r.PrepareKind == timing.PrepareSpare
		}
	}
	if !found {
		t.Fatalf("no spare.claimed mark with prepareKind spare: %v", stagesOf(records))
	}
}

func TestClaudeSpareWhoseQueryEndsDuringBindFallsBack(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "claude")
	w.claudeReply = func(a *spareAdapter, typ string) (json.RawMessage, error) {
		if typ == "set_mcp_servers" {
			queryEnded(a.fakeAdapter)
		}
		return json.RawMessage(`{}`), nil
	}
	w.fill(t, "claude")
	w.newConversation(t, "new", "claude")
	w.send(t, "new", "r1")
	if slices.Contains(w.claudeSpare(0).sent(), "send") {
		t.Fatal("the prompt went to a process whose Query ended")
	}
	if !isDone(w.claudeSpare(0).Done()) {
		t.Fatal("the spare was left running")
	}
	if w.adapterCount() != 1 || !slices.Contains(w.adapters[0].sent(), "send") {
		t.Fatalf("%d cold adapters", w.adapterCount())
	}
}

func TestClaudeSpareFallsBackBeforeAnyMessageRuns(t *testing.T) {
	t.Parallel()

	refused := func(message string) error { return &claude.AdapterError{Code: claude.CodeNotClaimed, Message: message} }
	cases := []struct {
		name   string
		reply  func(typ string) (json.RawMessage, error)
		reason string
	}{
		{"claim refused", func(typ string) (json.RawMessage, error) {
			if typ == "claim" {
				return nil, refused("project_settings_not_claimable")
			}
			return json.RawMessage(`{}`), nil
		}, timing.ReasonClaimRefused},
		{"option not applied", func(typ string) (json.RawMessage, error) {
			if typ == "claim" {
				return nil, refused("option_not_applied: model")
			}
			return json.RawMessage(`{}`), nil
		}, timing.ReasonOptionNotApplied},
		{"settings not applied", func(typ string) (json.RawMessage, error) {
			if typ == "claim" {
				return nil, refused("settings_not_applied: effortLevel")
			}
			return json.RawMessage(`{}`), nil
		}, timing.ReasonSettingsNotApplied},
		{"MCP request failed", func(typ string) (json.RawMessage, error) {
			if typ == "set_mcp_servers" {
				return nil, errors.New("refused")
			}
			return json.RawMessage(`{}`), nil
		}, timing.ReasonMCPFailed},
		{"MCP server not bound", func(typ string) (json.RawMessage, error) {
			if typ == "set_mcp_servers" {
				return json.RawMessage(`{"added":[],"removed":[],"errors":{"multica":"refused"}}`), nil
			}
			return json.RawMessage(`{}`), nil
		}, timing.ReasonMCPFailed},
		{"process died", func(typ string) (json.RawMessage, error) {
			if typ == "claim" {
				return nil, claude.ErrClosed
			}
			return json.RawMessage(`{}`), nil
		}, timing.ReasonClaimRefused},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := newSpareWorld(t, "claude")
			w.claudeReply = func(a *spareAdapter, typ string) (json.RawMessage, error) {
				if typ == "claim" {
					// A session id seen during a failed bind must not be kept.
					a.emit(t, `{"type":"system","subtype":"init","session_id":"from-the-spare"}`)
				}
				return c.reply(typ)
			}
			w.fill(t, "claude")
			w.newConversation(t, "new", "claude")
			w.send(t, "new", "r1")

			spare := w.claudeSpare(0)
			if !isDone(spare.Done()) {
				t.Fatal("the rejected spare is still running")
			}
			if slices.Contains(spare.sent(), "send") {
				t.Fatalf("the prompt reached the spare: %v", spare.sent())
			}
			if w.adapterCount() != 1 {
				t.Fatalf("%d cold adapters", w.adapterCount())
			}
			w.mu.Lock()
			cold := w.adapters[0]
			w.mu.Unlock()
			if got := fmt.Sprint(cold.sent()); got != "[start send]" {
				t.Fatalf("cold requests = %s", got)
			}
			if _, resumes := fieldsOf(cold, "start")["resume"]; resumes {
				t.Fatalf("fallback resumes: %v", fieldsOf(cold, "start"))
			}
			conv, _ := w.svc.Store().Conversation(context.Background(), "new")
			if conv.ProviderSessionID != "" {
				t.Fatalf("session remembered from a failed bind: %q", conv.ProviderSessionID)
			}
			// The new adapter's output reaches the turn through its own normalizer.
			cold.emit(t, `{"type":"system","subtype":"init","session_id":"real"}`,
				`{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"ok"}]}}`,
				`{"type":"result","subtype":"success","is_error":false,"result":"ok"}`)
			eventually(t, "the new adapter's session and output", func() bool {
				conv, _ = w.svc.Store().Conversation(context.Background(), "new")
				return conv.ProviderSessionID == "real" && conv.LastMessagePreview == "ok"
			})
			rejected := ""
			for _, r := range timingRecords(t, w.svc, w.timing) {
				if r.Stage == timing.StageSpareRejected {
					rejected = r.Reason
				}
			}
			if rejected != c.reason {
				t.Fatalf("rejected reason = %q, want %q", rejected, c.reason)
			}
		})
	}
}

func TestClaudeSpareOutputWaitsForTheBind(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "claude")
	w.claudeReply = func(a *spareAdapter, typ string) (json.RawMessage, error) {
		if typ == "claim" {
			a.emit(t, `{"type":"system","subtype":"init","session_id":"bound"}`)
		}
		return json.RawMessage(`{}`), nil
	}
	w.fill(t, "claude")
	w.newConversation(t, "new", "claude")
	w.send(t, "new", "r1")
	// What the process said before the prompt is no turn's and names no session
	// with a transcript yet; the prompt's own output does.
	conv, _ := w.svc.Store().Conversation(context.Background(), "new")
	if conv.ProviderSessionID != "" {
		t.Fatalf("session before any prompt output = %q", conv.ProviderSessionID)
	}
	w.claudeSpare(0).emit(t, `{"type":"result","subtype":"success","is_error":false,"result":"ok","session_id":"bound"}`)
	eventually(t, "session from the turn's output", func() bool {
		conv, _ := w.svc.Store().Conversation(context.Background(), "new")
		return conv.ProviderSessionID == "bound"
	})
}

func TestClaudeSpareIsLeftForConversationsWithASession(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "claude", "codex")
	w.fill(t, "claude")
	w.fill(t, "codex")
	w.old(t, "old")
	w.send(t, "old", "r1")
	if got := fmt.Sprint(w.claudeSpare(0).sent()); got != "[prewarm]" {
		t.Fatalf("spare requests = %s", got)
	}
	if !w.parked("claude") || w.adapterCount() != 1 {
		t.Fatalf("parked %v, cold adapters %d", w.parked("claude"), w.adapterCount())
	}
}

func TestOldSidecarIsNotAskedToPrewarmUntilTheExecutableChanges(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "claude")
	w.unsupported = true
	gone := w.svc.ClientConnected("client")
	defer gone()
	w.svc.spares.check()
	eventually(t, "attempt", func() bool { return w.claudeCount() == 1 && isDone(w.claudeSpare(0).Done()) })
	if slices.Contains(w.claudeSpare(0).sent(), "prewarm") {
		t.Fatal("a sidecar without the capability was asked to prewarm")
	}
	w.clock.advance(time.Hour)
	w.svc.spares.check()
	time.Sleep(10 * time.Millisecond)
	if w.claudeCount() != 1 {
		t.Fatalf("retried an unsupported executable: %d", w.claudeCount())
	}
	w.mu.Lock()
	w.unsupported = false
	w.conditions.Binary = "/bin/cli\x002.0"
	w.mu.Unlock()
	w.clock.advance(spareCheckInterval)
	w.svc.spares.check()
	eventually(t, "spare after the upgrade", func() bool { return w.parked("claude") })
}

func TestTooOldCLIIsRememberedByExecutable(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "claude")
	w.claudeReply = func(_ *spareAdapter, typ string) (json.RawMessage, error) {
		return nil, &claude.AdapterError{Code: claude.CodePrewarmUnsupported, Message: "unknown option --await-claim"}
	}
	gone := w.svc.ClientConnected("client")
	defer gone()
	w.svc.spares.check()
	eventually(t, "attempt", func() bool { return w.claudeCount() == 1 && isDone(w.claudeSpare(0).Done()) })
	eventually(t, "remembered", func() bool {
		slot := w.svc.spares.slots["claude"]
		slot.mu.Lock()
		defer slot.mu.Unlock()
		return slot.unsupported["/bin/cli\x001.0"]
	})
	w.clock.advance(time.Hour)
	w.svc.spares.check()
	time.Sleep(10 * time.Millisecond)
	if w.claudeCount() != 1 {
		t.Fatalf("retried a CLI that is too old: %d", w.claudeCount())
	}
}

func TestSpareStartBacksOffAndDisablesAfterRepeatedFailures(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "codex")
	attempts := 0
	w.codexStart = func() error { attempts++; return errors.New("crashed") }
	read := func() int {
		w.mu.Lock()
		defer w.mu.Unlock()
		return attempts
	}
	gone := w.svc.ClientConnected("client")
	defer gone()
	slot := w.svc.spares.slots["codex"]
	settled := func() bool {
		slot.mu.Lock()
		defer slot.mu.Unlock()
		return !slot.starting
	}
	check := func() {
		w.svc.spares.check()
		eventually(t, "start settled", settled)
	}
	check()
	if read() != 1 {
		t.Fatalf("attempts = %d", read())
	}
	check()
	if read() != 1 {
		t.Fatal("no backoff after a failure")
	}
	wait := spareBackoffBase
	for i := 2; i <= spareMaxFailures; i++ {
		w.clock.advance(wait - time.Millisecond)
		check()
		if read() != i-1 {
			t.Fatalf("attempt %d came before its backoff of %s", i, wait)
		}
		w.clock.advance(time.Millisecond)
		check()
		if read() != i {
			t.Fatalf("attempts = %d, want %d", read(), i)
		}
		wait *= 2
	}
	w.clock.advance(time.Hour)
	check()
	if read() != spareMaxFailures {
		t.Fatalf("kept trying after being disabled: %d", read())
	}
	// A new executable gets a new chance.
	w.mu.Lock()
	w.codexStart = nil
	w.conditions.Binary = "/bin/cli\x002.0"
	w.mu.Unlock()
	w.clock.advance(spareCheckInterval)
	check()
	if !w.parked("codex") {
		t.Fatal("no spare for a new executable")
	}
}

func TestSpareThatExitsSoonCountsAsAFailedStart(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "codex")
	w.fill(t, "codex")
	w.codexSpare(0).Stop()
	eventually(t, "discarded", func() bool { return !w.parked("codex") })
	w.svc.spares.check()
	time.Sleep(10 * time.Millisecond)
	if w.codexCount() != 1 {
		t.Fatal("restarted a spare that crashed at once, without backoff")
	}
	w.clock.advance(spareBackoffBase)
	w.svc.spares.check()
	eventually(t, "replacement", func() bool { return w.parked("codex") })

	// One that lived is simply replaced.
	w.clock.advance(spareHealthyAfter)
	w.codexSpare(1).Stop()
	eventually(t, "immediate replacement", func() bool { w.svc.spares.check(); return w.codexCount() == 3 && w.parked("codex") })
}

func TestParkedAdapterErrorDiscardsTheSpare(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "claude")
	w.fill(t, "claude")
	spare := w.claudeSpare(0)
	spare.handler(claude.Output{Type: "error", Error: &claude.AdapterError{Code: claude.CodeSpareExited, Message: "gone"}})
	eventually(t, "discarded", func() bool { return !w.parked("claude") && isDone(spare.Done()) })
}

func TestSparesAreDiscardedWhenWhatTheyFrozeChanged(t *testing.T) {
	t.Parallel()

	config := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(config, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(w *spareWorld)
		reason string
	}{
		{"max age", func(w *spareWorld) { w.clock.advance(spareMaxAge) }, timing.ReasonMaxAge},
		{"cli version", func(w *spareWorld) { w.setConditions(func(c *SpareConditions) { c.Binary = "/bin/cli\x002.0" }) }, timing.ReasonCLIChanged},
		{"signed-in user", func(w *spareWorld) { w.setConditions(func(c *SpareConditions) { c.Identity = "other" }) }, timing.ReasonEnvironmentChanged},
		{"signed out", func(w *spareWorld) { w.setConditions(func(c *SpareConditions) { c.Ready = false }) }, timing.ReasonEnvironmentChanged},
		{"user configuration", func(w *spareWorld) {
			later := time.Now().Add(time.Hour)
			if err := os.Chtimes(config, later, later); err != nil {
				panic(err)
			}
		}, timing.ReasonConfigChanged},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newSpareWorld(t, "codex")
			w.setConditions(func(s *SpareConditions) { s.ConfigFiles = []string{config} })
			w.fill(t, "codex")
			c.change(w)
			w.clock.advance(spareCheckInterval)
			w.svc.spares.check()
			eventually(t, "spare stopped", func() bool { return isDone(w.codexSpare(0).Done()) })
			discarded := ""
			for _, r := range timingRecords(t, w.svc, w.timing) {
				if r.Stage == timing.StageSpareDiscarded {
					discarded = r.Reason
				}
			}
			if discarded != c.reason {
				t.Fatalf("reason = %q, want %q", discarded, c.reason)
			}
		})
	}
}

func TestSpareReplacementBacksOffWhenConfigurationKeepsChanging(t *testing.T) {
	t.Parallel()

	config := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(config, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	touched := time.Now()
	churn := func() {
		touched = touched.Add(time.Minute)
		if err := os.Chtimes(config, touched, touched); err != nil {
			t.Fatal(err)
		}
	}
	w := newSpareWorld(t, "codex")
	w.setConditions(func(c *SpareConditions) { c.ConfigFiles = []string{config} })
	w.fill(t, "codex")

	// The first change replaces the spare straight away.
	churn()
	w.clock.advance(spareCheckInterval)
	w.svc.spares.check()
	eventually(t, "first replacement", func() bool { return w.parked("codex") && w.codexCount() == 2 })

	// A replacement that went stale soon after starting waits before the next.
	churn()
	w.clock.advance(spareCheckInterval)
	w.svc.spares.check()
	eventually(t, "second discard", func() bool { return !w.parked("codex") })
	w.clock.advance(spareStaleBackoffBase - time.Second)
	w.svc.spares.check()
	time.Sleep(10 * time.Millisecond)
	if w.codexCount() != 2 {
		t.Fatalf("%d spares started within the backoff", w.codexCount())
	}
	w.clock.advance(time.Second)
	w.svc.spares.check()
	eventually(t, "replacement after the backoff", func() bool { return w.parked("codex") && w.codexCount() == 3 })

	// A spare that stayed valid for a while clears the backoff.
	w.clock.advance(spareStaleSteadyAfter)
	churn()
	w.svc.spares.check()
	eventually(t, "replacement after a steady spare", func() bool { return w.parked("codex") && w.codexCount() == 4 })
}

func TestClaimRefusesASpareThatWentStaleSinceTheLastCheck(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "codex")
	w.fill(t, "codex")
	w.setConditions(func(c *SpareConditions) { c.Identity = "other" })
	w.newConversation(t, "new", "codex")
	w.send(t, "new", "r1")
	if slices.Contains(w.codexSpare(0).sent(), "turn/start") {
		t.Fatal("a stale spare served the turn")
	}
	if w.coldCodex.count() != 1 {
		t.Fatalf("%d cold processes", w.coldCodex.count())
	}
}

func TestSparesGoAwayWithTheLastClient(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "codex")
	gone := w.svc.ClientConnected("client")
	w.svc.spares.check()
	eventually(t, "spare", func() bool { return w.parked("codex") })
	gone()
	w.clock.advance(spareNoClientGrace - time.Second)
	w.svc.spares.check()
	if !w.parked("codex") {
		t.Fatal("discarded within the grace period")
	}
	w.clock.advance(time.Second)
	w.svc.spares.check()
	if w.parked("codex") {
		t.Fatal("kept on a machine nobody uses")
	}
	w.svc.spares.check()
	time.Sleep(10 * time.Millisecond)
	if w.codexCount() != 1 {
		t.Fatal("refilled with nobody connected")
	}
}

func TestDrainStopProvidersAndUndrain(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "codex", "claude")
	ctx := context.Background()
	w.fill(t, "codex")
	eventually(t, "claude spare", func() bool { return w.parked("claude") })
	if _, err := w.svc.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if !w.parked("codex") {
		t.Fatal("draining alone stopped the spare")
	}
	if err := w.svc.StopProviders(ctx); err != nil {
		t.Fatal(err)
	}
	if !isDone(w.codexSpare(0).Done()) || !isDone(w.claudeSpare(0).Done()) {
		t.Fatal("StopProviders left spares running")
	}
	w.clock.advance(time.Hour)
	w.svc.spares.check()
	time.Sleep(10 * time.Millisecond)
	if w.codexCount() != 1 {
		t.Fatal("refilled while draining")
	}
	if err := w.svc.Undrain(ctx); err != nil {
		t.Fatal(err)
	}
	w.svc.spares.check()
	eventually(t, "refill after undrain", func() bool { return w.parked("codex") && w.parked("claude") })
}

func TestCloseStopsSparesAndStartsUnderway(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "codex", "claude")
	entered := make(chan struct{}, 1)
	w.claudeReply = func(a *spareAdapter, typ string) (json.RawMessage, error) {
		if typ == "prewarm" {
			entered <- struct{}{}
			<-a.done
			return nil, claude.ErrClosed
		}
		return json.RawMessage(`{}`), nil
	}
	gone := w.svc.ClientConnected("client")
	defer gone()
	w.svc.spares.check()
	eventually(t, "codex spare", func() bool { return w.parked("codex") })
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- w.svc.Close(context.Background()) }()
	eventually(t, "cancelled", func() bool {
		w.svc.mu.Lock()
		defer w.svc.mu.Unlock()
		return w.svc.closing
	})
	// The fake's prewarm ignores cancellation until its process goes away.
	w.claudeSpare(0).exit(nil)
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if !isDone(w.codexSpare(0).Done()) {
		t.Fatal("Close left a spare running")
	}
}

func TestCodexNewConversationClaimsTheSpare(t *testing.T) {
	t.Parallel()

	w := newSpareWorld(t, "codex")
	// The answer depends on the directory asked about, not where the process runs.
	w.codexSetup = func(_ int, s *fakeCodex) {
		s.reply = func(method string, params map[string]any) (json.RawMessage, bool) {
			if method == "config/read" && params["cwd"] == "/repo" {
				return json.RawMessage(`{"config":{"model_reasoning_effort":"high"}}`), true
			}
			return nil, false
		}
	}
	w.fill(t, "codex")
	w.newConversation(t, "new", "codex")
	w.send(t, "new", "r1")
	s := w.codexSpare(0)
	if got := fmt.Sprint(s.sent()); got != "[config/read thread/start turn/start]" {
		t.Fatalf("requests = %s", got)
	}
	if s.paramsOf("config/read")["cwd"] != "/repo" || s.paramsOf("thread/start")["cwd"] != "/repo" {
		t.Fatalf("cwd = %v, %v", s.paramsOf("config/read"), s.paramsOf("thread/start"))
	}
	if s.paramsOf("turn/start")["effort"] != "high" {
		t.Fatalf("turn/start = %v", s.paramsOf("turn/start"))
	}
	mcp, _ := s.paramsOf("thread/start")["config"].(map[string]any)
	servers, _ := mcp["mcp_servers"].(map[string]any)
	if _, bound := servers[contextmcp.Name]; !bound {
		t.Fatalf("thread/start config = %v", mcp)
	}
	if w.coldCodex.count() != 0 {
		t.Fatal("a cold process started too")
	}
	// The spare's frames now reach this conversation's turn.
	s.handler(codexappserver.Frame{Kind: codexappserver.FrameNotification, Method: "item/completed", Params: json.RawMessage(`{"threadId":"th-1","turnId":"tu-1","item":{"id":"a1","type":"agentMessage","text":"ok"}}`)})
	completeCodexTurn(t, s)
	eventually(t, "session and output", func() bool {
		conv, _ := w.svc.Store().Conversation(context.Background(), "new")
		return conv.ProviderSessionID == "th-1" && conv.LastMessagePreview == "ok"
	})
	eventually(t, "refill", func() bool { w.svc.spares.check(); return w.parked("codex") })
}

func TestCodexSpareFallsBackWithNoMessageSubmitted(t *testing.T) {
	t.Parallel()

	failing := func(method string) func(ctx context.Context, m string) error {
		return func(_ context.Context, m string) error {
			if m == method {
				return errors.New("refused")
			}
			return nil
		}
	}
	cases := []struct {
		name  string
		setup func(s *fakeCodex)
	}{
		{"config read failed", func(s *fakeCodex) { s.intercept = failing("config/read") }},
		{"thread start failed", func(s *fakeCodex) { s.intercept = failing("thread/start") }},
		{"reserved MCP name", func(s *fakeCodex) {
			s.reply = func(method string, _ map[string]any) (json.RawMessage, bool) {
				return json.RawMessage(`{"config":{"mcp_servers":{"` + contextmcp.Name + `":{}}}}`), method == "config/read"
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := newSpareWorld(t, "codex")
			w.codexSetup = func(_ int, s *fakeCodex) { c.setup(s) }
			if c.name == "reserved MCP name" {
				// The native setting also stops the normal start, as it always has.
				w.fill(t, "codex")
				w.newConversation(t, "new", "codex")
				w.coldCodex.setup = func(_ int, s *fakeCodex) { c.setup(s) }
				if _, err := w.svc.Send(context.Background(), SendCommand{ScopeID: w.scopeID, ConversationID: "new", RequestID: "r1", Text: "go"}); err == nil {
					t.Fatal("send started despite the reserved name")
				}
				if !isDone(w.codexSpare(0).Done()) {
					t.Fatal("the spare is still running")
				}
				return
			}
			w.fill(t, "codex")
			w.newConversation(t, "new", "codex")
			w.send(t, "new", "r1")
			if slices.Contains(w.codexSpare(0).sent(), "turn/start") {
				t.Fatal("a prompt reached the rejected spare")
			}
			if !isDone(w.codexSpare(0).Done()) {
				t.Fatal("the rejected spare is still running")
			}
			if got := fmt.Sprint(w.coldCodex.server(0).sent()); got != "[config/read thread/start turn/start]" {
				t.Fatalf("cold requests = %s", got)
			}
		})
	}
}

func TestCodexBindFailureDiscardsTheSpare(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	cold := &codexProcesses{}
	spareServer := &fakeCodex{params: map[string]map[string]any{}, done: make(chan struct{})}
	pool := &sparePool{s: &Service{traces: newTurnTraces(nil, time.Now)}}
	binds := 0
	r := NewCodexRunner(CodexRunnerOptions{
		ConversationID: "conv-1", ScopeID: f.scopeID, Files: f.store, Session: CodexSession{Cwd: "/repo"},
		Batcher: f.batcher, Limiter: f.limiter, ContextMCPName: "multica", Start: cold.start,
		Context: func(context.Context, string, func() map[string]any) (BoundContext, func(), error) {
			binds++
			if binds == 1 {
				return BoundContext{}, nil, errors.New("bind failed")
			}
			return BoundContext{}, func() {}, nil
		},
		ClaimSpare: func(context.Context, string) *parkedSpare {
			return &parkedSpare{pool: pool, provider: "codex", server: spareServer, token: "t", codexFrames: &frameSwitch[codexappserver.Frame]{}, stop: spareServer.Stop}
		},
	})
	codexSend(t, r, "t1")
	if !isDone(spareServer.Done()) || slices.Contains(spareServer.sent(), "turn/start") {
		t.Fatalf("spare requests = %v", spareServer.sent())
	}
	if got := fmt.Sprint(cold.server(0).sent()); got != "[config/read thread/start turn/start]" {
		t.Fatalf("cold requests = %s", got)
	}
}

func TestFrameSwitchReplaysHeldFramesInOrder(t *testing.T) {
	t.Parallel()

	var parked, got []int
	f := &frameSwitch[int]{parked: func(v int) { parked = append(parked, v) }}
	f.deliver(1)
	f.hold()
	f.deliver(2)
	f.deliver(3)
	f.adopt(func(v int) { got = append(got, v) })
	f.deliver(4)
	if fmt.Sprint(parked) != "[1]" || fmt.Sprint(got) != "[2 3 4]" {
		t.Fatalf("parked %v, got %v", parked, got)
	}
}
