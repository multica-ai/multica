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

// makeResolvableSkillBundle builds a self-consistent bundle whose hash/size
// match its content, so validateSkillBundle accepts it and skillRefFromBundle
// yields the ref the agent would carry for it.
func makeResolvableSkillBundle(id string) SkillData {
	b := SkillData{
		ID:      id,
		Source:  "workspace",
		Name:    id,
		Content: "content-of-" + id,
		Files:   []SkillFileData{{Path: "rules.md", Content: "rules-" + id}},
	}
	ref := skillRefFromBundle(b)
	b.Hash = ref.Hash
	b.SizeBytes = ref.SizeBytes
	b.Files[0].SHA256 = ref.Files[0].SHA256
	b.Files[0].SizeBytes = ref.Files[0].SizeBytes
	return b
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
