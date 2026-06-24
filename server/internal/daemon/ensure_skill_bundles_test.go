package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// indexedSkillBundle builds a valid SkillData bundle distinguished by idx, with
// its ref hash/size filled in so it passes validateSkillBundle.
func indexedSkillBundle(idx string) SkillData {
	bundle := SkillData{
		ID:      "skill-" + idx,
		Source:  "workspace",
		Name:    "skill-" + idx,
		Content: "content-" + idx,
		Files:   []SkillFileData{{Path: "rules.md", Content: "rules-" + idx}},
	}
	ref := skillRefFromBundle(bundle)
	bundle.Hash = ref.Hash
	bundle.SizeBytes = ref.SizeBytes
	bundle.Files[0].SHA256 = ref.Files[0].SHA256
	bundle.Files[0].SizeBytes = ref.Files[0].SizeBytes
	return bundle
}

// skillResolveFixture stands up a daemon wired to a fake resolve endpoint and a
// real on-disk skill cache. failAfter, if >0, makes the endpoint return 500 on
// the failAfter-th request onward, simulating a slow-link body-read timeout that
// only some batches survive.
type skillResolveFixture struct {
	daemon       *Daemon
	cache        *SkillBundleCache
	refs         []SkillRefData
	mu           sync.Mutex
	requestSizes []int
}

func newSkillResolveFixture(t *testing.T, count, failAfter int) *skillResolveFixture {
	t.Helper()
	fx := &skillResolveFixture{}
	byID := make(map[string]SkillData, count)
	for i := 0; i < count; i++ {
		b := indexedSkillBundle(string(rune('a' + i)))
		byID[b.ID] = b
		fx.refs = append(fx.refs, skillRefFromBundle(b))
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Skills []SkillRefData `json:"skills"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		fx.mu.Lock()
		fx.requestSizes = append(fx.requestSizes, len(body.Skills))
		reqNum := len(fx.requestSizes)
		fx.mu.Unlock()

		if failAfter > 0 && reqNum >= failAfter {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		out := struct {
			Bundles []SkillData `json:"bundles"`
		}{}
		for _, ref := range body.Skills {
			if b, ok := byID[ref.ID]; ok {
				out.Bundles = append(out.Bundles, b)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	d.skillCache = NewSkillBundleCache(t.TempDir())
	fx.daemon = d
	fx.cache = d.skillCache
	return fx
}

func (fx *skillResolveFixture) task() *Task {
	return &Task{ID: "task-1", RuntimeID: "rt-1", WorkspaceID: "ws-1", Agent: &AgentData{SkillRefs: fx.refs}}
}

func (fx *skillResolveFixture) maxRequestSize() int {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	max := 0
	for _, n := range fx.requestSizes {
		if n > max {
			max = n
		}
	}
	return max
}

func (fx *skillResolveFixture) cachedCount() int {
	n := 0
	for _, ref := range fx.refs {
		if _, ok := fx.cache.Load("ws-1", ref); ok {
			n++
		}
	}
	return n
}

// TestEnsureTaskSkillBundlesChunksMisses verifies the daemon resolves a large
// set of skill-bundle cache misses in bounded batches rather than one atomic
// request. Resolving every miss in a single request means a slow link times out
// reading the whole body and the task never starts (#4505).
func TestEnsureTaskSkillBundlesChunksMisses(t *testing.T) {
	fx := newSkillResolveFixture(t, 12, 0)

	if err := fx.daemon.ensureTaskSkillBundles(context.Background(), fx.task()); err != nil {
		t.Fatalf("ensureTaskSkillBundles: %v", err)
	}

	fx.mu.Lock()
	reqCount := len(fx.requestSizes)
	fx.mu.Unlock()
	if reqCount < 2 {
		t.Fatalf("expected misses to be resolved in multiple batched requests, got %d request(s)", reqCount)
	}
	if max := fx.maxRequestSize(); max >= len(fx.refs) {
		t.Fatalf("a single resolve request carried %d/%d skills; expected bounded batches", max, len(fx.refs))
	}
	if got := fx.cachedCount(); got != len(fx.refs) {
		t.Fatalf("expected all %d bundles cached after a clean resolve, got %d", len(fx.refs), got)
	}
}

// TestEnsureTaskSkillBundlesPersistsCompletedBatchesOnFailure verifies that when
// a later batch fails (the slow-link timeout case), the batches that already
// succeeded are persisted to the cache. Without this, every dispatch re-fetches
// the full bundle and fails identically; with it, progress accumulates and a
// later dispatch only fetches the remaining misses (#4505).
func TestEnsureTaskSkillBundlesPersistsCompletedBatchesOnFailure(t *testing.T) {
	fx := newSkillResolveFixture(t, 12, 2) // first batch succeeds, second batch onward fails

	err := fx.daemon.ensureTaskSkillBundles(context.Background(), fx.task())
	if err == nil {
		t.Fatal("expected ensureTaskSkillBundles to fail when a batch errors")
	}

	cached := fx.cachedCount()
	if cached == 0 {
		t.Fatal("expected the successfully-resolved batch to be cached despite a later batch failing")
	}
	if cached == len(fx.refs) {
		t.Fatalf("did not expect all bundles cached when a batch failed, got %d", cached)
	}
}
