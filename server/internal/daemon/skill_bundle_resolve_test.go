package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/skillbundle"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestSkillBundleResolveTimeout(t *testing.T) {
	cases := []struct {
		name string
		size int64
		want time.Duration
	}{
		{"zero size floors to min", 0, skillBundleResolveMinTimeout},
		{"negative size floors to min", -5, skillBundleResolveMinTimeout},
		{"tiny bundle floors to min", 1024, skillBundleResolveMinTimeout},
		{"scales with size above the floor", 2 * 1024 * 1024, 40 * time.Second},
		{"huge bundle caps at max", 100 * 1024 * 1024, skillBundleResolveMaxTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := skillBundleResolveTimeout(tc.size); got != tc.want {
				t.Fatalf("skillBundleResolveTimeout(%d) = %s, want %s", tc.size, got, tc.want)
			}
		})
	}
}

// makeResolvableSkillBundleWith builds a self-consistent bundle from explicit
// content, so validateSkillBundle accepts it and skillRefFromBundle yields the
// ref the agent would carry. Varying content changes the hash, which lets tests
// model a skill edited between claim and prepare.
func makeResolvableSkillBundleWith(id, content, fileContent string) SkillData {
	b := SkillData{
		ID:      id,
		Source:  "workspace",
		Name:    id,
		Content: content,
		Files:   []SkillFileData{{Path: "rules.md", Content: fileContent}},
	}
	ref := skillRefFromBundle(b)
	b.Hash = ref.Hash
	b.SizeBytes = ref.SizeBytes
	b.Files[0].SHA256 = ref.Files[0].SHA256
	b.Files[0].SizeBytes = ref.Files[0].SizeBytes
	return b
}

// makeResolvableSkillBundle is makeResolvableSkillBundleWith with default
// content derived from the id.
func makeResolvableSkillBundle(id string) SkillData {
	return makeResolvableSkillBundleWith(id, "content-of-"+id, "rules-"+id)
}

// TestEnsureTaskSkillBundles_CachesEachSuccessAcrossDispatches is the core
// regression for GitHub #4505: when one skill's download fails, the skills that
// did resolve must still be cached, and the next dispatch must re-fetch only
// the still-missing one — never the whole bundle. The pre-fix code resolved the
// whole set in one atomic request and cached nothing on failure, so a large
// bundle that could not finish in the fixed 30s timeout was re-downloaded in
// full on every dispatch and never converged.
func TestEnsureTaskSkillBundles_CachesEachSuccessAcrossDispatches(t *testing.T) {
	defer noSleepRetry(t)()

	var mu sync.Mutex
	requested := map[string]int{}
	failIDs := map[string]bool{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Skills []SkillRefData `json:"skills"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// Each request must carry exactly one skill — the fix resolves
		// per-skill so each download fits its own deadline and caches alone.
		if len(req.Skills) != 1 {
			t.Errorf("expected exactly 1 skill per request, got %d", len(req.Skills))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id := req.Skills[0].ID
		mu.Lock()
		requested[id]++
		fail := failIDs[id]
		mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"bundles": []SkillData{makeResolvableSkillBundle(id)}})
	}))
	defer srv.Close()

	ids := []string{"skill-1", "skill-2", "skill-3"}
	refs := make([]SkillRefData, len(ids))
	for i, id := range ids {
		refs[i] = skillRefFromBundle(makeResolvableSkillBundle(id))
	}

	d := &Daemon{
		client:     NewClient(srv.URL),
		skillCache: NewSkillBundleCache(t.TempDir()),
	}
	task := &Task{
		ID:          "task-1",
		RuntimeID:   "rt-1",
		WorkspaceID: "ws-1",
		Agent:       &AgentData{ID: "agent-1", SkillRefs: refs},
	}

	// Dispatch 1: the last skill fails. The first two must still be cached.
	mu.Lock()
	failIDs["skill-3"] = true
	mu.Unlock()

	if err := d.ensureTaskSkillBundles(context.Background(), task); err == nil {
		t.Fatal("dispatch 1: expected error because skill-3 fails, got nil")
	}
	if _, ok := d.skillCache.Load("ws-1", refs[0]); !ok {
		t.Error("dispatch 1: skill-1 should be cached despite skill-3 failing")
	}
	if _, ok := d.skillCache.Load("ws-1", refs[1]); !ok {
		t.Error("dispatch 1: skill-2 should be cached despite skill-3 failing")
	}
	if _, ok := d.skillCache.Load("ws-1", refs[2]); ok {
		t.Error("dispatch 1: skill-3 must not be cached after a failed download")
	}
	// A 500 is transient, so skill-3 is retried over the full schedule.
	mu.Lock()
	wantSkill3 := len(skillBundleResolveRetrySchedule) + 1
	if got := requested["skill-3"]; got != wantSkill3 {
		t.Errorf("dispatch 1: skill-3 attempts = %d, want %d (initial + retries)", got, wantSkill3)
	}
	requested = map[string]int{}
	failIDs = map[string]bool{}
	mu.Unlock()

	// Dispatch 2: everything succeeds. Only the previously-missing skill-3 may
	// be re-fetched; the two cached skills must not hit the network again.
	if err := d.ensureTaskSkillBundles(context.Background(), task); err != nil {
		t.Fatalf("dispatch 2: expected success, got %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := requested["skill-1"]; got != 0 {
		t.Errorf("dispatch 2: skill-1 was re-fetched %d times, want 0 (served from cache)", got)
	}
	if got := requested["skill-2"]; got != 0 {
		t.Errorf("dispatch 2: skill-2 was re-fetched %d times, want 0 (served from cache)", got)
	}
	if got := requested["skill-3"]; got != 1 {
		t.Errorf("dispatch 2: skill-3 fetched %d times, want exactly 1", got)
	}
	if len(task.Agent.Skills) != len(ids) {
		t.Fatalf("dispatch 2: resolved %d skills, want %d", len(task.Agent.Skills), len(ids))
	}
	for i, id := range ids {
		if task.Agent.Skills[i].ID != id {
			t.Errorf("dispatch 2: skill[%d].ID = %q, want %q", i, task.Agent.Skills[i].ID, id)
		}
	}
}

// TestEnsureTaskSkillBundles_AcceptsServerSideSkillUpdate guards the resolve
// endpoint's contract: when a skill is edited between claim and prepare, the
// server returns the *current* bundle and hash even though the daemon asked
// with the stale claim-time hash (see ResolveTaskSkillBundles). The daemon must
// accept it — validating the bundle for self-consistency, not against the
// requested hash — and cache it under its new hash. Pinning to the requested
// hash would reject a legitimate update and fail the task.
func TestEnsureTaskSkillBundles_AcceptsServerSideSkillUpdate(t *testing.T) {
	defer noSleepRetry(t)()

	current := makeResolvableSkillBundleWith("skill-1", "v2-content", "v2-rules")
	currentRef := skillRefFromBundle(current)
	staleRef := skillRefFromBundle(makeResolvableSkillBundleWith("skill-1", "v1-content", "v1-rules"))
	if staleRef.Hash == currentRef.Hash {
		t.Fatal("test setup: stale and current hash must differ")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Skills []SkillRefData `json:"skills"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(req.Skills) != 1 || req.Skills[0].Hash != staleRef.Hash {
			t.Errorf("expected the stale ref to be sent, got %+v", req.Skills)
		}
		// Server ignores the requested (stale) hash and returns the current bundle.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"bundles": []SkillData{current}})
	}))
	defer srv.Close()

	d := &Daemon{
		client:     NewClient(srv.URL),
		skillCache: NewSkillBundleCache(t.TempDir()),
	}
	task := &Task{
		ID:          "task-1",
		RuntimeID:   "rt-1",
		WorkspaceID: "ws-1",
		Agent:       &AgentData{ID: "agent-1", SkillRefs: []SkillRefData{staleRef}},
	}

	if err := d.ensureTaskSkillBundles(context.Background(), task); err != nil {
		t.Fatalf("expected success when server returns an updated bundle, got %v", err)
	}
	if len(task.Agent.Skills) != 1 || task.Agent.Skills[0].Hash != currentRef.Hash {
		t.Fatalf("expected the resolved skill to be the updated bundle (hash %s), got %+v", currentRef.Hash, task.Agent.Skills)
	}
	if _, ok := d.skillCache.Load("ws-1", currentRef); !ok {
		t.Error("updated bundle should be cached under its own (new) hash")
	}
}

func TestEnsureTaskSkillBundles_CacheRemovalFailureDoesNotDiscardDownloadedBundle(t *testing.T) {
	bundle := makeResolvableSkillBundle("skill-1")
	ref := skillRefFromBundle(bundle)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"bundles": []SkillData{bundle}})
	}))
	defer server.Close()

	cache := NewSkillBundleCache(t.TempDir())
	cache.removeAll = func(string) error { return fs.ErrPermission }
	daemon := &Daemon{client: NewClient(server.URL), skillCache: cache}
	task := &Task{
		ID:          "task-1",
		RuntimeID:   "rt-1",
		WorkspaceID: "ws-1",
		Agent:       &AgentData{ID: "agent-1", SkillRefs: []SkillRefData{ref}},
	}

	if err := daemon.ensureTaskSkillBundles(context.Background(), task); err != nil {
		t.Fatalf("cache removal failure must not discard a downloaded bundle: %v", err)
	}
	if len(task.Agent.Skills) != 1 || task.Agent.Skills[0].Hash != bundle.Hash {
		t.Fatalf("downloaded bundle was not attached to the task: %+v", task.Agent.Skills)
	}
	if _, ok := cache.Load(task.WorkspaceID, ref); ok {
		t.Fatal("bundle must not appear cached after the removal failure")
	}
}

func TestEnsureTaskSkillBundles_RejectsPluginHashDrift(t *testing.T) {
	defer noSleepRetry(t)()

	makePluginBundle := func(content string) SkillData {
		bundle := SkillData{ID: "plugin:review-readiness", Source: skillbundle.SourcePlugin, Name: "review-readiness", Content: content}
		ref := skillRefFromBundle(bundle)
		bundle.Hash = ref.Hash
		bundle.SizeBytes = ref.SizeBytes
		return bundle
	}
	pinned := makePluginBundle("pinned-content")
	mutated := makePluginBundle("mutated-content")
	pinnedRef := skillRefFromBundle(pinned)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"bundles": []SkillData{mutated}})
	}))
	defer server.Close()

	daemon := &Daemon{client: NewClient(server.URL), skillCache: NewSkillBundleCache(t.TempDir())}
	task := &Task{
		ID:          "task-plugin-pin",
		RuntimeID:   "rt-1",
		WorkspaceID: "ws-1",
		Agent:       &AgentData{ID: "agent-1", SkillRefs: []SkillRefData{pinnedRef}},
	}
	if err := daemon.ensureTaskSkillBundles(context.Background(), task); err == nil {
		t.Fatal("expected plugin bundle hash drift to fail closed")
	}
	if _, ok := daemon.skillCache.Load(task.WorkspaceID, pinnedRef); ok {
		t.Fatal("mutated plugin bundle must not be cached under the pinned ref")
	}
}

// TestEnsureTaskSkillBundles_DeadlineIsLabelledStructurally is the MUL-5370
// regression. A stalled bundle download used to surface as the bare string
// "resolve skill bundles: context deadline exceeded", which taskfailure.Classify
// could only file under agent_error.unknown — a bucket that is NOT on the
// server's retry allowlist. So a transient stall became a terminal chat failure
// carrying a label nobody could act on, and the user was told only "something
// went wrong". The wrap must now (a) name the skill and how long we waited,
// (b) preserve the transport cause, and (c) carry a sentinel that
// taskRunFailureReason maps to the retryable platform-side reason.
func TestEnsureTaskSkillBundles_DeadlineIsLabelledStructurally(t *testing.T) {
	defer noSleepRetry(t)()

	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		// Accept the connection and never answer — the shape of a link that
		// is up but cannot carry the response (blocked route, missing proxy).
		<-block
	}))
	// LIFO: release the handler before tearing the server down, so Close
	// doesn't block on an in-flight request.
	defer srv.Close()
	defer close(block)

	ref := skillRefFromBundle(makeResolvableSkillBundle("frontend-review"))
	d := &Daemon{
		client:     NewClient(srv.URL),
		skillCache: NewSkillBundleCache(t.TempDir()),
	}
	task := &Task{
		ID:          "task-1",
		RuntimeID:   "rt-1",
		WorkspaceID: "ws-1",
		Agent:       &AgentData{ID: "agent-1", SkillRefs: []SkillRefData{ref}},
	}

	// Squeeze the parent below the per-skill floor so the deadline fires
	// without the test waiting skillBundleResolveMinTimeout for it.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := d.ensureTaskSkillBundles(ctx, task)
	if err == nil {
		t.Fatal("expected an error when the bundle download never completes")
	}
	if !errors.Is(err, errSkillBundleUnavailable) {
		t.Errorf("error must carry the skill-bundle sentinel, got %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error must preserve the transport cause, got %v", err)
	}
	if !strings.Contains(err.Error(), "frontend-review") {
		t.Errorf("error must name the skill that failed, got %v", err)
	}
	want := taskfailure.ReasonSkillBundleUnavailable.String()
	if got := taskRunFailureReason(err); got != want {
		t.Errorf("taskRunFailureReason = %q, want %q (retryable platform-side reason)", got, want)
	}
}

// clearProxyEnv unsets every variable proxyEnvSummary consults, so a developer
// machine that happens to run behind a proxy does not flip these assertions.
func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		t.Setenv(key, "")
	}
}

// TestDescribeSkillBundleFailure is the GitHub #7386 regression. The reporter
// spent a day re-importing xlsx because the message named the skill and said
// nothing about the wire; the whole point of this text is that a reader can
// tell, without a packet capture, whether the link was dead or merely slow.
func TestDescribeSkillBundleFailure(t *testing.T) {
	clearProxyEnv(t)
	ref := SkillRefData{Name: "xlsx", ID: "c5034ed6", SizeBytes: 1101426}

	t.Run("dead link reports no response and separates declared content size", func(t *testing.T) {
		got := describeSkillBundleFailure(ref, TransferStats{}, 30*time.Second)
		for _, want := range []string{
			"network error downloading skill \"xlsx\"",
			"no successful response from server after 30s overall",
			"declared skill content size 1.05 MB",
			"no proxy configured",
			"the skill content is not at fault",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in:\n%s", want, got)
			}
		}
	})

	t.Run("partial response reports response bytes without an inferred rate", func(t *testing.T) {
		stats := TransferStats{ResponseStarted: true, BytesRead: 552960}
		got := describeSkillBundleFailure(ref, stats, 30*time.Second)
		for _, want := range []string{
			"received up to 540 KB of response body data in one attempt",
			"failed after 30s overall",
			"declared skill content size 1.05 MB",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in:\n%s", want, got)
			}
		}
		if strings.Contains(got, "KB/s") || strings.Contains(got, "MB/s") {
			t.Errorf("cross-retry elapsed time must not be presented as a transfer rate:\n%s", got)
		}
		if strings.Contains(got, "no response from server") {
			t.Errorf("a link that delivered bytes must not read as no-response:\n%s", got)
		}
	})

	t.Run("wire bytes may exceed declared content size", func(t *testing.T) {
		stats := TransferStats{ResponseStarted: true, BytesRead: 1388598}
		got := describeSkillBundleFailure(ref, stats, 30*time.Second)
		for _, want := range []string{
			"received up to 1.32 MB of response body data",
			"declared skill content size 1.05 MB",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in:\n%s", want, got)
			}
		}
		if strings.Contains(got, "1.32 MB of 1.05 MB") {
			t.Errorf("wire and decoded content sizes must not be shown as one progress ratio:\n%s", got)
		}
	})

	t.Run("configured proxy is named but never valued", func(t *testing.T) {
		clearProxyEnv(t)
		t.Setenv("HTTPS_PROXY", "http://user:secret@127.0.0.1:7890")
		got := describeSkillBundleFailure(ref, TransferStats{}, time.Second)
		if !strings.Contains(got, "proxy configured (HTTPS_PROXY)") {
			t.Errorf("expected the proxy to be reported as configured:\n%s", got)
		}
		// Proxy URLs routinely carry credentials and this string is shown to
		// the whole workspace.
		if strings.Contains(got, "secret") || strings.Contains(got, "7890") {
			t.Errorf("proxy value must never be echoed into failure text:\n%s", got)
		}
	})
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1 KB"},
		{552960, "540 KB"},
		{1101426, "1.05 MB"},
	}
	for _, tc := range cases {
		if got := formatBytes(tc.n); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestEnsureTaskSkillBundles_ServerErrorsKeepServerSemantics(t *testing.T) {
	defer noSleepRetry(t)()

	for _, status := range []int{
		http.StatusMultipleChoices,
		http.StatusUnauthorized,
		http.StatusNotFound,
		http.StatusConflict,
		http.StatusInternalServerError,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"task is not preparing"}`)
			}))
			defer srv.Close()

			ref := skillRefFromBundle(makeResolvableSkillBundle("frontend-review"))
			d := &Daemon{client: NewClient(srv.URL), skillCache: NewSkillBundleCache(t.TempDir())}
			task := &Task{
				ID: "task-1", RuntimeID: "rt-1", WorkspaceID: "ws-1",
				Agent: &AgentData{ID: "agent-1", SkillRefs: []SkillRefData{ref}},
			}

			err := d.ensureTaskSkillBundles(context.Background(), task)
			if err == nil {
				t.Fatal("expected server error")
			}
			if !errors.Is(err, errSkillBundleUnavailable) {
				t.Errorf("sentinel must survive, got %v", err)
			}
			var reqErr *requestError
			if !errors.As(err, &reqErr) || reqErr.StatusCode != status {
				t.Errorf("requestError status = %v, want %d; err=%v", reqErr, status, err)
			}
			if got := err.Error(); strings.Contains(got, "network error") || strings.Contains(got, "skill content is not at fault") {
				t.Errorf("HTTP %d must keep server semantics instead of being relabelled as a network failure:\n%s", status, got)
			}
		})
	}
}

func TestResolveSkillBundle_ServerErrorBodyIsNotCountedAsBundleData(t *testing.T) {
	defer noSleepRetry(t)()

	for _, status := range []int{http.StatusMultipleChoices, http.StatusConflict, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"task is not preparing"}`)
			}))
			defer srv.Close()

			client := NewClient(srv.URL)
			_, stats, err := client.ResolveSkillBundle(context.Background(), "rt-1", "task-1", SkillRefData{ID: "skill-1"})
			if err == nil {
				t.Fatal("expected server error")
			}
			if stats.ResponseStarted || stats.BytesRead != 0 {
				t.Errorf("HTTP %d error body was counted as bundle data: %+v", status, stats)
			}
		})
	}
}

func TestEnsureTaskSkillBundles_MalformedSuccessfulResponseIsNotNetworkError(t *testing.T) {
	defer noSleepRetry(t)()

	for _, tc := range []struct {
		name              string
		body              string
		wantUnexpectedEOF bool
	}{
		{name: "invalid token", body: `{"bundles": definitely-not-json}`},
		{name: "clean EOF after truncated JSON", body: `{"bundles":`, wantUnexpectedEOF: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			ref := skillRefFromBundle(makeResolvableSkillBundle("frontend-review"))
			d := &Daemon{client: NewClient(srv.URL), skillCache: NewSkillBundleCache(t.TempDir())}
			task := &Task{
				ID: "task-1", RuntimeID: "rt-1", WorkspaceID: "ws-1",
				Agent: &AgentData{ID: "agent-1", SkillRefs: []SkillRefData{ref}},
			}

			err := d.ensureTaskSkillBundles(context.Background(), task)
			if err == nil {
				t.Fatal("expected malformed response error")
			}
			if tc.wantUnexpectedEOF && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("truncated JSON must exercise json.Decoder's io.ErrUnexpectedEOF path, got %v", err)
			}
			if got := err.Error(); strings.Contains(got, "network error") || strings.Contains(got, "skill content is not at fault") {
				t.Errorf("malformed JSON ending at a clean EOF is a server payload error, not a network failure:\n%s", got)
			}
		})
	}
}

// TestTransferStatsKeepsHighWaterMark pins the across-retries semantics: the
// question the counter answers is "did this link ever get anywhere", so an
// attempt that reached the body must not be erased by a later one that died
// during connect.
func TestTransferStatsKeepsHighWaterMark(t *testing.T) {
	var stats TransferStats
	first := stats.wrap(strings.NewReader("0123456789"))
	if _, err := io.ReadAll(first); err != nil {
		t.Fatalf("read: %v", err)
	}
	if stats.BytesRead != 10 {
		t.Fatalf("BytesRead = %d, want 10", stats.BytesRead)
	}
	// A second, shorter attempt must not lower the mark.
	second := stats.wrap(strings.NewReader("abc"))
	if _, err := io.ReadAll(second); err != nil {
		t.Fatalf("read: %v", err)
	}
	if stats.BytesRead != 10 {
		t.Errorf("a shorter retry lowered the high-water mark to %d, want 10", stats.BytesRead)
	}
	// nil must stay usable so callers can opt out of the accounting.
	var nilStats *TransferStats
	if got := nilStats.wrap(strings.NewReader("x")); got == nil {
		t.Error("nil TransferStats must still return a usable reader")
	}
	nilStats.observeResponseStarted()
}

// TestEnsureTaskSkillBundles_SlowLinkReportsPartialTransfer is the other half
// of #7386. A link that accepts the connection and then trickles must be
// reported as a partial transfer, not as "no response" — that is exactly the
// distinction that decides whether raising the deadline would have helped.
func TestEnsureTaskSkillBundles_SlowLinkReportsPartialTransfer(t *testing.T) {
	defer noSleepRetry(t)()
	clearProxyEnv(t)

	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Emit a valid prefix of the response, flush it so it really lands on
		// the wire, then stall — a body that started but cannot finish.
		io.WriteString(w, `{"bundles":[{"id":"x","content":"`+strings.Repeat("a", 4096))
		w.(http.Flusher).Flush()
		<-block
	}))
	defer srv.Close()
	defer close(block)

	ref := skillRefFromBundle(makeResolvableSkillBundle("frontend-review"))
	d := &Daemon{client: NewClient(srv.URL), skillCache: NewSkillBundleCache(t.TempDir())}
	task := &Task{
		ID: "task-1", RuntimeID: "rt-1", WorkspaceID: "ws-1",
		Agent: &AgentData{ID: "agent-1", SkillRefs: []SkillRefData{ref}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := d.ensureTaskSkillBundles(ctx, task)
	if err == nil {
		t.Fatal("expected an error when the body never completes")
	}
	if !errors.Is(err, errSkillBundleUnavailable) {
		t.Errorf("sentinel must survive, got %v", err)
	}
	if got := err.Error(); !strings.Contains(got, "received") || strings.Contains(got, "no response from server") {
		t.Errorf("a stalled body must report a partial transfer, got:\n%s", got)
	}
}
