package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
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
