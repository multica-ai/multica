package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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
		{"zero size gets fixed budget", 0, skillBundleResolveFixedBudget},
		{"negative size gets fixed budget", -5, skillBundleResolveFixedBudget},
		{"one byte rounds up transfer time", 1, skillBundleResolveFixedBudget + time.Second},
		{"exact throughput second", 10 * 1024, skillBundleResolveFixedBudget + time.Second},
		{"partial throughput second rounds up", 10*1024 + 1, skillBundleResolveFixedBudget + 2*time.Second},
		{"scales at conservative throughput", 2 * 1024 * 1024, 3*time.Minute + 55*time.Second},
		{"huge bundle caps at max", 100 * 1024 * 1024, skillBundleResolveMaxTimeout},
		{"max int does not overflow", int64(^uint64(0) >> 1), skillBundleResolveMaxTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := skillBundleResolveTimeout(tc.size); got != tc.want {
				t.Fatalf("skillBundleResolveTimeout(%d) = %s, want %s", tc.size, got, tc.want)
			}
		})
	}
}

func TestSkillBundleDownloadWaitContext_ReservesPrepareTime(t *testing.T) {
	parentDeadline := time.Now().Add(2 * time.Minute)
	parent, cancelParent := context.WithDeadline(context.Background(), parentDeadline)
	defer cancelParent()

	ctx, cancel := skillBundleDownloadWaitContext(parent)
	defer cancel()
	got, ok := ctx.Deadline()
	if !ok {
		t.Fatal("download wait context has no deadline")
	}
	want := parentDeadline.Add(-skillBundlePrepareReserve)
	if !got.Equal(want) {
		t.Fatalf("download deadline = %s, want %s", got, want)
	}
}

func TestSkillBundleDownloadWaitContext_NoParentDeadline(t *testing.T) {
	ctx, cancel := skillBundleDownloadWaitContext(context.Background())
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("background download wait context unexpectedly has a deadline")
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
	if len(task.Agent.Skills) != 0 {
		t.Fatalf("dispatch 1: task received %d skills after a missing bundle; want none", len(task.Agent.Skills))
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

func TestEnsureTaskSkillBundles_SingleflightAcrossTasks(t *testing.T) {
	bundle := makeResolvableSkillBundle("shared-skill")
	ref := skillRefFromBundle(bundle)

	var calls atomic.Int32
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var releaseOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(requestStarted)
		}
		<-releaseRequest
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"bundles": []SkillData{bundle}})
	}))
	defer srv.Close()
	defer releaseOnce.Do(func() { close(releaseRequest) })

	coordinator := newSkillBundleDownloadCoordinator(defaultMaxConcurrentSkillDownloads)
	d := &Daemon{
		client:         NewClient(srv.URL),
		skillCache:     NewSkillBundleCache(t.TempDir()),
		skillDownloads: coordinator,
	}

	const taskCount = 20
	start := make(chan struct{})
	errCh := make(chan error, taskCount)
	for i := 0; i < taskCount; i++ {
		task := testTaskWithSkillRefs(fmt.Sprintf("task-%d", i), "ws-1", []SkillRefData{ref})
		go func() {
			<-start
			errCh <- d.ensureTaskSkillBundles(context.Background(), task)
		}()
	}
	close(start)

	waitForSignal(t, requestStarted, "shared bundle request")
	waitForSkillFlightWaiters(t, coordinator, skillBundleCacheKey("ws-1", ref), taskCount)
	releaseOnce.Do(func() { close(releaseRequest) })

	for i := 0; i < taskCount; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("task %d: ensure skill bundle: %v", i, err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("resolve requests = %d, want 1", got)
	}
}

func TestEnsureTaskSkillBundles_DownloadsMissesConcurrently(t *testing.T) {
	const skillCount = defaultMaxConcurrentSkillDownloads
	bundles := make(map[string]SkillData, skillCount)
	refs := make([]SkillRefData, 0, skillCount)
	for i := 0; i < skillCount; i++ {
		bundle := makeResolvableSkillBundle(fmt.Sprintf("skill-%d", i))
		bundles[bundle.ID] = bundle
		refs = append(refs, skillRefFromBundle(bundle))
	}

	entered := make(chan struct{}, skillCount)
	releaseRequests := make(chan struct{})
	var releaseOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Skills []SkillRefData `json:"skills"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Skills) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		entered <- struct{}{}
		<-releaseRequests
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"bundles": []SkillData{bundles[req.Skills[0].ID]}})
	}))
	defer srv.Close()
	defer releaseOnce.Do(func() { close(releaseRequests) })

	d := &Daemon{
		client:         NewClient(srv.URL),
		skillCache:     NewSkillBundleCache(t.TempDir()),
		skillDownloads: newSkillBundleDownloadCoordinator(defaultMaxConcurrentSkillDownloads),
	}
	task := testTaskWithSkillRefs("task-1", "ws-1", refs)
	errCh := make(chan error, 1)
	go func() { errCh <- d.ensureTaskSkillBundles(context.Background(), task) }()
	for i := 0; i < skillCount; i++ {
		waitForSignal(t, entered, "concurrent bundle request")
	}
	releaseOnce.Do(func() { close(releaseRequests) })
	if err := waitForError(t, errCh, "concurrent bundle resolution"); err != nil {
		t.Fatalf("ensure skill bundles: %v", err)
	}
	if got := len(task.Agent.Skills); got != skillCount {
		t.Fatalf("resolved skills = %d, want %d", got, skillCount)
	}
}

func TestEnsureTaskSkillBundles_WaiterCancellationDoesNotCancelSharedDownload(t *testing.T) {
	bundle := makeResolvableSkillBundle("shared-skill")
	ref := skillRefFromBundle(bundle)

	requestStarted := make(chan struct{})
	probeRequest := make(chan struct{})
	requestAlive := make(chan struct{})
	requestCancelled := make(chan struct{})
	releaseRequest := make(chan struct{})
	var releaseOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-probeRequest
		select {
		case <-r.Context().Done():
			close(requestCancelled)
			return
		default:
			close(requestAlive)
		}
		select {
		case <-r.Context().Done():
			close(requestCancelled)
			return
		case <-releaseRequest:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"bundles": []SkillData{bundle}})
		}
	}))
	defer srv.Close()
	defer releaseOnce.Do(func() { close(releaseRequest) })

	coordinator := newSkillBundleDownloadCoordinator(defaultMaxConcurrentSkillDownloads)
	d := &Daemon{
		client:         NewClient(srv.URL),
		skillCache:     NewSkillBundleCache(t.TempDir()),
		skillDownloads: coordinator,
	}
	task1 := testTaskWithSkillRefs("task-1", "ws-1", []SkillRefData{ref})
	task2 := testTaskWithSkillRefs("task-2", "ws-1", []SkillRefData{ref})

	ctx1, cancel1 := context.WithCancel(context.Background())
	err1 := make(chan error, 1)
	go func() { err1 <- d.ensureTaskSkillBundles(ctx1, task1) }()
	waitForSignal(t, requestStarted, "shared bundle request")

	err2 := make(chan error, 1)
	go func() { err2 <- d.ensureTaskSkillBundles(context.Background(), task2) }()
	waitForSkillFlightWaiters(t, coordinator, skillBundleCacheKey("ws-1", ref), 2)

	cancel1()
	if err := waitForError(t, err1, "cancelled waiter"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter error = %v, want context.Canceled", err)
	}
	close(probeRequest)
	select {
	case <-requestAlive:
	case <-requestCancelled:
		t.Fatal("cancelling one waiter cancelled the shared HTTP request")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out probing shared HTTP request")
	}

	releaseOnce.Do(func() { close(releaseRequest) })
	if err := waitForError(t, err2, "remaining waiter"); err != nil {
		t.Fatalf("remaining waiter failed: %v", err)
	}
}

func TestSkillBundleDownloadCoordinator_CancelsWhenAllWaitersLeave(t *testing.T) {
	coordinator := newSkillBundleDownloadCoordinator(defaultMaxConcurrentSkillDownloads)
	started := make(chan struct{})
	operationCancelled := make(chan struct{})
	var calls atomic.Int32
	fn := func(ctx context.Context) (SkillData, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-ctx.Done()
		close(operationCancelled)
		return SkillData{}, context.Cause(ctx)
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	err1 := make(chan error, 1)
	err2 := make(chan error, 1)
	go func() {
		_, err := coordinator.do(ctx1, "shared", fn)
		err1 <- err
	}()
	waitForSignal(t, started, "shared operation")
	go func() {
		_, err := coordinator.do(ctx2, "shared", fn)
		err2 <- err
	}()
	waitForSkillFlightWaiters(t, coordinator, "shared", 2)

	cancel1()
	if err := waitForError(t, err1, "first cancelled waiter"); !errors.Is(err, context.Canceled) {
		t.Fatalf("first waiter error = %v, want context.Canceled", err)
	}
	select {
	case <-operationCancelled:
		t.Fatal("shared operation cancelled while one waiter remained")
	default:
	}

	cancel2()
	if err := waitForError(t, err2, "last cancelled waiter"); !errors.Is(err, context.Canceled) {
		t.Fatalf("last waiter error = %v, want context.Canceled", err)
	}
	waitForSignal(t, operationCancelled, "shared operation cancellation")
	if got := calls.Load(); got != 1 {
		t.Fatalf("shared operation calls = %d, want 1", got)
	}
}

func TestEnsureTaskSkillBundles_GlobalDownloadLimit(t *testing.T) {
	const (
		downloadLimit = defaultMaxConcurrentSkillDownloads
		taskCount     = 8
	)

	bundles := make(map[string]SkillData, taskCount)
	refs := make([]SkillRefData, 0, taskCount)
	for i := 0; i < taskCount; i++ {
		bundle := makeResolvableSkillBundle(fmt.Sprintf("skill-%d", i))
		bundles[bundle.ID] = bundle
		refs = append(refs, skillRefFromBundle(bundle))
	}

	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	entered := make(chan struct{}, taskCount)
	releaseRequests := make(chan struct{})
	var releaseOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Skills []SkillRefData `json:"skills"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Skills) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		current := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			peak := maxInFlight.Load()
			if current <= peak || maxInFlight.CompareAndSwap(peak, current) {
				break
			}
		}
		entered <- struct{}{}
		<-releaseRequests
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"bundles": []SkillData{bundles[req.Skills[0].ID]}})
	}))
	defer srv.Close()
	defer releaseOnce.Do(func() { close(releaseRequests) })

	d := &Daemon{
		client:         NewClient(srv.URL),
		skillCache:     NewSkillBundleCache(t.TempDir()),
		skillDownloads: newSkillBundleDownloadCoordinator(downloadLimit),
	}
	errCh := make(chan error, taskCount)
	for i, ref := range refs {
		task := testTaskWithSkillRefs(fmt.Sprintf("task-%d", i), "ws-1", []SkillRefData{ref})
		go func() { errCh <- d.ensureTaskSkillBundles(context.Background(), task) }()
	}
	for i := 0; i < downloadLimit; i++ {
		waitForSignal(t, entered, "bounded bundle request")
	}
	select {
	case <-entered:
		t.Fatalf("more than %d bundle requests entered concurrently", downloadLimit)
	case <-time.After(50 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(releaseRequests) })

	for i := 0; i < taskCount; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("task %d: ensure skill bundle: %v", i, err)
		}
	}
	if got := maxInFlight.Load(); got > downloadLimit {
		t.Fatalf("max concurrent downloads = %d, want <= %d", got, downloadLimit)
	}
}

func testTaskWithSkillRefs(taskID, workspaceID string, refs []SkillRefData) *Task {
	return &Task{
		ID:          taskID,
		RuntimeID:   "rt-1",
		WorkspaceID: workspaceID,
		Agent:       &AgentData{ID: "agent-1", SkillRefs: refs},
	}
}

func waitForSkillFlightWaiters(t *testing.T, coordinator *skillBundleDownloadCoordinator, key string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		coordinator.mu.Lock()
		flight := coordinator.flights[key]
		got := 0
		if flight != nil {
			got = flight.waiters
		}
		coordinator.mu.Unlock()
		if got >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("flight %q did not reach %d waiters", key, want)
}

func waitForSignal(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func waitForError(t *testing.T, ch <-chan error, name string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
		return nil
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
	if _, ok := d.skillCache.Load("ws-1", ref); ok {
		t.Error("deadline failure must not leave a cache entry")
	}
	if len(task.Agent.Skills) != 0 {
		t.Fatalf("deadline failure injected %d incomplete skills into task", len(task.Agent.Skills))
	}
}
