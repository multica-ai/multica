//go:build !windows

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/kagent"
)

type periodicKagentFixture struct {
	mu                                                  sync.Mutex
	row                                                 kagentRuntimeEnrollment
	remote                                              *kagent.Agent
	reports                                             []map[string]json.RawMessage
	absent, unavailable, remoteFailure, conflict, block bool
	abaConflict                                         bool
	gets, writes, deletes, creates                      int
	entered                                             chan struct{}
	d                                                   *Daemon
}

func newPeriodicKagentFixture(t *testing.T) *periodicKagentFixture {
	t.Helper()
	configureWiringKagent(t)
	f := &periodicKagentFixture{row: kagentRuntimeEnrollment{AgentID: wiringAgentID, Name: "Native DEV", WorkspaceID: "workspace", RuntimeID: "runtime", Revision: 1, Generation: 1, DesiredState: "present", Phase: "pending"}, entered: make(chan struct{}, 1)}
	previous := http.DefaultTransport
	http.DefaultTransport = kagentWiringTransport(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		f.serve(t, rec, r)
		response := rec.Result()
		response.Request = r
		return response, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	f.d = &Daemon{client: NewClient("https://multica.test"), logger: slog.New(slog.NewTextHandler(io.Discard, nil)), workspaces: map[string]*workspaceState{"workspace": {workspaceID: "workspace", runtimeIDs: []string{"runtime"}}}, runtimeIndex: map[string]Runtime{"runtime": {ID: "runtime", Provider: "kagent"}}}
	f.d.client.token = "daemon-credential"
	return f
}

func (f *periodicKagentFixture) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Host == "multica.test" {
		if r.Header.Get("Authorization") != "Bearer daemon-credential" {
			t.Error("daemon authentication missing")
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/daemon/runtimes/runtime/kagent/agents" {
			f.gets++
			if f.block {
				f.mu.Unlock()
				select {
				case f.entered <- struct{}{}:
				default:
				}
				<-r.Context().Done()
				f.mu.Lock()
				w.WriteHeader(503)
				return
			}
			if f.unavailable {
				http.Error(w, "secret upstream failure", 503)
				return
			}
			rows := []kagentRuntimeEnrollment{}
			if !f.absent {
				rows = append(rows, f.row)
			}
			_ = json.NewEncoder(w).Encode(rows)
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/report") {
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			f.reports = append(f.reports, body)
			if len(body) != 4 || body["expected_revision"] == nil || body["expected_generation"] == nil || body["expected_phase"] == nil || body["phase"] == nil {
				t.Error("report contains fields beyond phase CAS")
			}
			raw, _ := json.Marshal(body)
			for _, secret := range []string{"test-caller", "daemon-credential", "secret upstream"} {
				if strings.Contains(string(raw), secret) {
					t.Error("secret in report")
				}
			}
			var gen, revision int64
			var expected, phase string
			_ = json.Unmarshal(body["expected_generation"], &gen)
			_ = json.Unmarshal(body["expected_revision"], &revision)
			_ = json.Unmarshal(body["expected_phase"], &expected)
			_ = json.Unmarshal(body["phase"], &phase)
			if f.conflict {
				f.conflict = false
				f.row.Generation++
				f.row.Revision++
				http.Error(w, "stale secret", 409)
				return
			}
			if f.abaConflict {
				f.abaConflict = false
				// Another reporter completed pending->blocked->pending (or
				// blocked->pending->blocked) before this request arrived.
				f.row.Revision += 2
				http.Error(w, "stale report", 409)
				return
			}
			if revision != f.row.Revision || gen != f.row.Generation || expected != f.row.Phase {
				t.Errorf("stale CAS %s", raw)
				w.WriteHeader(409)
				return
			}
			allowed := expected == "pending" && (phase == "ready" || phase == "blocked") || expected == "ready" && phase == "blocked" || expected == "blocked" && phase == "pending"
			if !allowed {
				t.Errorf("invalid transition %s", raw)
				w.WriteHeader(400)
				return
			}
			f.row.Phase = phase
			f.row.Revision++
			_ = json.NewEncoder(w).Encode(f.row)
			return
		}
		t.Errorf("unexpected control request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(404)
		return
	}
	if r.URL.Host != "management.test" {
		t.Errorf("unexpected endpoint %s", r.URL.Host)
		w.WriteHeader(400)
		return
	}
	if r.Header.Get("Authorization") != "Bearer test-caller" {
		t.Error("management identity missing")
	}
	if f.remoteFailure {
		http.Error(w, "secret upstream credentials", 500)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if f.remote == nil {
			w.WriteHeader(404)
			return
		}
	case http.MethodPost, http.MethodPatch:
		f.writes++
		if r.Method == http.MethodPost {
			f.creates++
		}
		var a kagent.Agent
		if f.remote != nil {
			a = *f.remote
		}
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			t.Error(err)
		}
		if a.Spec.HarnessRef == nil || a.Spec.HarnessRef.Name != "dedicated-readonly" || a.Spec.Template == nil || a.Spec.Template.ModelConfig.Name != "test-model" {
			t.Error("operator desired state missing")
		}
		a.Metadata.UID = "pinned-uid"
		a.Metadata.ResourceVersion = "1"
		if f.remote == nil {
			a.Metadata.Generation = 1
		} else {
			a.Metadata.Generation = f.remote.Metadata.Generation + 1
		}
		if r.Method == http.MethodPost {
			a.Status.Conditions = []kagent.Condition{{Type: kagent.ConditionReady, Status: "True", ObservedGeneration: 1}}
		}
		f.remote = &a
	case http.MethodDelete:
		f.deletes++
		t.Error("remote deletion forbidden")
		w.WriteHeader(403)
		return
	default:
		t.Errorf("unexpected management method %s", r.Method)
		w.WriteHeader(400)
		return
	}
	_ = json.NewEncoder(w).Encode(f.remote)
}
func (f *periodicKagentFixture) round(t *testing.T) {
	t.Helper()
	if err := f.d.kagentReconcileRound(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestKagentPeriodicCreatesBeforeTaskAndRepairsAfterRestart(t *testing.T) {
	f := newPeriodicKagentFixture(t)
	f.round(t)
	if f.creates != 1 || f.row.Phase != "ready" {
		t.Fatalf("creates=%d phase=%s", f.creates, f.row.Phase)
	}
	token := f.remote.Metadata.Annotations[kagent.AnnotationOwnerToken]
	f.d = &Daemon{client: f.d.client, workspaces: f.d.workspaces, runtimeIndex: f.d.runtimeIndex}
	f.remote.Spec.HarnessRef.Name = "drift"
	if err := f.d.kagentReconcileRound(context.Background()); err == nil {
		t.Fatal("unobserved generation reported ready")
	}
	if f.row.Phase != "blocked" || f.remote.Spec.HarnessRef.Name != "dedicated-readonly" {
		t.Fatal("drift was not repaired and blocked")
	}
	f.remote.Status.Conditions = []kagent.Condition{{Type: kagent.ConditionReady, Status: "True", ObservedGeneration: f.remote.Metadata.Generation}}
	f.round(t)
	if f.row.Phase != "ready" || f.creates != 1 || f.remote.Metadata.Annotations[kagent.AnnotationOwnerToken] != token {
		t.Fatal("restart changed ownership")
	}
	if err := f.d.authorizeKagentTask(context.Background(), kagentClaim()); err != nil {
		t.Fatal(err)
	}
}
func TestKagentPeriodicRejectsUnauthorizedEnrollmentsAndTasks(t *testing.T) {
	for _, name := range []string{"workspace", "runtime", "allowlist", "absent", "generation", "revision", "phase", "missing", "unregistered"} {
		t.Run(name, func(t *testing.T) {
			f := newPeriodicKagentFixture(t)
			switch name {
			case "workspace":
				f.row.WorkspaceID = "wrong"
			case "runtime":
				f.row.RuntimeID = "wrong"
			case "allowlist":
				f.row.AgentID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
			case "absent":
				f.row.DesiredState = "absent"
			case "revision":
				f.row.Revision = 0
			case "generation":
				f.row.Generation = 0
			case "phase":
				f.row.Phase = "deleted"
			case "missing":
				f.absent = true
			case "unregistered":
				f.d.runtimeIndex["runtime"] = Runtime{ID: "runtime", Provider: "other"}
			}
			f.round(t)
			if f.writes != 0 || len(f.reports) != 0 {
				t.Fatal("unauthorized enrollment touched")
			}
			if _, err := f.d.runTask(context.Background(), kagentClaim(), "kagent", 0, slog.Default()); err == nil {
				t.Fatal("task bypassed enrollment")
			}
		})
	}
}
func TestKagentPeriodicStaleCASReloadsBeforeRetry(t *testing.T) {
	f := newPeriodicKagentFixture(t)
	f.row.Phase = "blocked"
	f.conflict = true
	if err := f.d.kagentReconcileRound(context.Background()); err == nil {
		t.Fatal("stale CAS accepted")
	}
	if f.writes != 0 {
		t.Fatal("reconciled after stale authorization")
	}
	f.round(t)
	if f.gets != 2 || f.row.Generation != 2 || f.row.Phase != "ready" {
		t.Fatal("did not reload current generation")
	}
	f.row.Phase = "pending"
	f.conflict = true
	if err := f.d.kagentReconcileRound(context.Background()); err == nil {
		t.Fatal("stale completion accepted")
	}
	f.round(t)
	if f.row.Generation != 3 || f.creates != 1 {
		t.Fatal("stale completion reset ownership")
	}
}

func TestKagentPeriodicABACASReloadsBeforeRetry(t *testing.T) {
	for _, phase := range []string{"pending", "blocked"} {
		t.Run(phase, func(t *testing.T) {
			f := newPeriodicKagentFixture(t)
			f.row.Phase = phase
			f.abaConflict = true
			if err := f.d.kagentReconcileRound(t.Context()); err == nil {
				t.Fatal("ABA conflict accepted")
			}
			if f.row.Generation != 1 || f.row.Phase != phase || f.row.Revision != 3 || len(f.reports) != 1 {
				t.Fatalf("continued after ABA conflict: %+v reports=%d", f.row, len(f.reports))
			}
			if phase == "blocked" && f.writes != 0 {
				t.Fatal("reconciled after stale retry authorization")
			}
			f.round(t)
			if f.gets != 2 || f.row.Generation != 1 || f.row.Phase != "ready" {
				t.Fatalf("did not reload after ABA: %+v gets=%d", f.row, f.gets)
			}
			wantRevision := int64(4)
			if phase == "blocked" {
				wantRevision = 5
			}
			if f.row.Revision != wantRevision {
				t.Fatalf("retry used incorrect revisions: %+v", f.row)
			}
		})
	}
}
func TestKagentPeriodicUnavailableOrAbsentNeverDeletes(t *testing.T) {
	f := newPeriodicKagentFixture(t)
	f.round(t)
	f.unavailable = true
	if err := f.d.kagentReconcileRound(context.Background()); err == nil {
		t.Fatal("API failure accepted")
	}
	if err := f.d.authorizeKagentTask(context.Background(), kagentClaim()); err == nil {
		t.Fatal("offline approval cached")
	}
	f.unavailable = false
	f.absent = true
	f.round(t)
	if f.deletes != 0 || f.writes != 1 {
		t.Fatal("absence changed remote state")
	}
	f.absent = false
	f.round(t)
	if f.creates != 1 || f.row.Phase != "ready" {
		t.Fatal("absence tombstoned enrollment")
	}
}
func TestKagentPeriodicRemoteErrorsOnlyReportPhase(t *testing.T) {
	f := newPeriodicKagentFixture(t)
	f.round(t)
	f.remoteFailure = true
	err := f.d.kagentReconcileRound(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret") || f.row.Phase != "blocked" {
		t.Fatalf("phase=%s err=%v", f.row.Phase, err)
	}
	f.remoteFailure = false
	f.round(t)
	if f.row.Phase != "ready" {
		t.Fatal("blocked agent did not retry")
	}
}
func TestKagentPeriodicExecutionLeaseAndUnknownFenceSurvive(t *testing.T) {
	f := newPeriodicKagentFixture(t)
	f.round(t)
	cfg, err := loadKagentDaemonConfig()
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, journal, err := cfg.lifecycle()
	if err != nil {
		t.Fatal(err)
	}
	lease, err := lifecycle.Acquire(context.Background(), wiringAgentID)
	if err != nil {
		t.Fatal(err)
	}
	before := len(f.reports)
	if err := f.d.kagentReconcileRound(context.Background()); err == nil {
		lease.Close()
		t.Fatal("execution lock ignored")
	}
	lease.Close()
	if len(f.reports) != before || f.row.Phase != "ready" {
		t.Fatal("active work reported blocked")
	}
	key := kagent.ConversationKey("workspace", wiringAgentID, "chat/chat")
	rec, err := journal.Lock(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Save(map[string]string{"ActiveAttempt": "unknown-outcome"}); err != nil {
		t.Fatal(err)
	}
	rec.Close()
	f.round(t)
	rec, err = journal.Lock(key)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	var after map[string]string
	if _, err := rec.Load(&after); err != nil {
		t.Fatal(err)
	}
	if after["ActiveAttempt"] != "unknown-outcome" {
		t.Fatal("erased unknown outcome fence")
	}
}
func TestKagentPeriodicCancellationAndDefaultDisabled(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		f := newPeriodicKagentFixture(t)
		t.Setenv("MULTICA_KAGENT_ENABLED", "")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
		defer cancel()
		f.d.kagentReconcileLoop(ctx, time.Millisecond, 4*time.Millisecond)
		if f.gets != 0 || f.writes != 0 {
			t.Fatal("disabled loop made requests")
		}
	})
	t.Run("inflight", func(t *testing.T) {
		f := newPeriodicKagentFixture(t)
		f.block = true
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() { f.d.kagentReconcileLoop(ctx, time.Millisecond, 4*time.Millisecond); close(done) }()
		select {
		case <-f.entered:
		case <-time.After(time.Second):
			t.Fatal("startup did not discover")
		}
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("cancellation blocked")
		}
	})
	t.Run("retry", func(t *testing.T) {
		f := newPeriodicKagentFixture(t)
		f.unavailable = true
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() { f.d.kagentReconcileLoop(ctx, time.Millisecond, 4*time.Millisecond); close(done) }()
		deadline := time.After(time.Second)
		for {
			f.mu.Lock()
			if f.gets >= 2 {
				f.unavailable = false
			}
			ready := f.row.Phase == "ready"
			f.mu.Unlock()
			if ready {
				break
			}
			select {
			case <-deadline:
				t.Fatal("periodic retry did not recover")
			case <-time.After(time.Millisecond):
			}
		}
		cancel()
		<-done
	})
}
func TestKagentPeriodicClaimCannotEnrollFromLocalAllowlist(t *testing.T) {
	f := newPeriodicKagentFixture(t)
	cfg, err := loadKagentDaemonConfig()
	if err != nil {
		t.Fatal(err)
	}
	remote, err := cfg.backendConfig(kagentClaim())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Lifecycle.Reconcile(context.Background(), wiringAgentID); err == nil {
		t.Fatal("task configuration enrolled resource")
	}
	if f.writes != 0 {
		t.Fatal("task created resource")
	}
}

func TestKagentPeriodicRefusesReplacedResourceIdentity(t *testing.T) {
	f := newPeriodicKagentFixture(t)
	f.round(t)
	writes := f.writes
	f.remote.Metadata.UID = "replacement-uid"
	if err := f.d.kagentReconcileRound(context.Background()); err == nil {
		t.Fatal("replacement resource adopted")
	}
	if f.row.Phase != "blocked" || f.writes != writes || f.deletes != 0 {
		t.Fatal("ownership mismatch mutated remote state")
	}
}

func TestKagentReadinessReportResponseRevision(t *testing.T) {
	for _, revision := range []int64{0, 41, 42, 43, 44} {
		t.Run(fmt.Sprint(revision), func(t *testing.T) {
			f := newPeriodicKagentFixture(t)
			row := f.row
			row.Revision = 42
			before := row
			f.d.client.client.Transport = kagentWiringTransport(func(r *http.Request) (*http.Response, error) {
				var request struct {
					ExpectedRevision int64 `json:"expected_revision"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if request.ExpectedRevision != before.Revision {
					t.Fatalf("report did not use observed revision: %+v", request)
				}
				result := before
				result.Phase = "ready"
				result.Revision = revision
				rec := httptest.NewRecorder()
				_ = json.NewEncoder(rec).Encode(result)
				return rec.Result(), nil
			})
			err := f.d.client.reportKagentPhase(t.Context(), &row, "ready")
			if revision == 43 {
				if err != nil || row.Revision != 43 || row.Phase != "ready" {
					t.Fatalf("valid server revision not adopted: %+v %v", row, err)
				}
			} else if err == nil || row != before {
				t.Fatalf("invalid response advanced local snapshot: %+v %v", row, err)
			}
		})
	}
}

func TestKagentReadinessReportInvalidRevision(t *testing.T) {
	f := newPeriodicKagentFixture(t)
	for _, revision := range []int64{-1, 0, math.MaxInt64} {
		row := f.row
		row.Revision = revision
		before := row
		if err := f.d.client.reportKagentPhase(t.Context(), &row, "ready"); err == nil || row != before {
			t.Fatalf("invalid revision accepted: %+v %v", row, err)
		}
	}
	if len(f.reports) != 0 {
		t.Fatal("invalid revision sent to server")
	}
}
