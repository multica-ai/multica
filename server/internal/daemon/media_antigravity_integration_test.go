package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/daemon/media"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/testutil/agyimage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The caller runs the actual CLI in an empty home with no provider credentials.
// Only the host executor can access the test subscription; all job scheduling,
// attachment transfers and receipts pass through real Server handlers and DB.
func TestMediaAntigravityCredentialSeparatedCLI(t *testing.T) {
	database := os.Getenv("DATABASE_URL")
	if database == "" {
		t.Skip("requires migrated DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	fx := testutil.New(pool, "", "")
	userID := fx.User(t, "agy media caller", uuid.NewString()+"@example.test")
	workspaceID := fx.Workspace(t, "agy media", uuid.NewString())
	fx.Member(t, workspaceID, userID, "owner")
	fx = testutil.New(pool, workspaceID, userID)
	runtimeCols := testutil.Cols{"provider": "antigravity", "daemon_id": uuid.NewString(), "metadata": `{"capabilities":["media-image-agy-v1"]}`}
	runtimeID := fx.Runtime(t, "agy host", runtimeCols)
	wrongRuntime := fx.Runtime(t, "unready host", testutil.Cols{"provider": "antigravity", "daemon_id": uuid.NewString()})
	fx.Cleanup(t, "DELETE FROM attachment WHERE workspace_id=$1", workspaceID)
	fx.Cleanup(t, "DELETE FROM media_job WHERE workspace_id=$1", workspaceID)
	t.Setenv("LOCAL_UPLOAD_DIR", t.TempDir())
	h := &handler.Handler{Queries: db.New(pool), DB: pool, TxStarter: pool, Storage: storage.NewLocalStorageFromEnv()}
	var uploads, downloads atomic.Int32
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			if token != "Bearer fixture-member" && token != "Bearer fixture-daemon" && !strings.HasPrefix(token, "Bearer mjt_") {
				t.Error("missing caller, daemon or job credential")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			// Model the authenticated member/daemon identity. Job-token handlers
			// additionally verify their scoped token using the real signing path.
			r.Header.Set("X-User-ID", userID)
			r.Header.Set("X-Workspace-ID", workspaceID)
			next.ServeHTTP(w, r)
		})
	})
	router.Post("/api/media/jobs", h.CreateMediaJob)
	router.Get("/api/media/jobs/{id}", h.GetMediaJob)
	router.Post("/api/daemon/media/claim", h.ClaimMediaJob)
	router.Post("/api/daemon/media/result", h.ReportMediaJob)
	router.Get("/api/media/jobs/{id}/status", h.MediaAttemptStatus)
	router.Get("/api/media/jobs/{id}/inputs/{attachmentID}", h.DownloadMediaInput)
	router.Post("/api/upload-file", h.UploadFile)
	var uploaded []byte
	router.Post("/api/media/jobs/{id}/output", func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(data))
		attempt := uploads.Add(1)
		if attempt == 1 {
			uploaded = data
			http.Error(w, "transient upload failure", 503)
			return
		}
		if !bytes.Equal(uploaded, data) {
			t.Error("upload retry changed the artifact")
		}
		if attempt == 2 {
			rec := httptest.NewRecorder()
			h.UploadMediaOutput(rec, r)
			if rec.Code != http.StatusOK {
				t.Errorf("commit output: %d %s", rec.Code, rec.Body.String())
			}
			http.Error(w, "lost committed-upload receipt", 503)
			return
		}
		h.UploadMediaOutput(w, r)
	})
	router.Get("/api/attachments/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		h.DownloadAttachment(w, r)
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	binary := filepath.Join(t.TempDir(), "multica")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/multica")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	callerHome := t.TempDir()
	runCLI := func(t *testing.T, args ...string) protocol.MediaJob {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, append(args, "--output", "json")...)
		cmd.Dir = callerHome
		cmd.Env = []string{"HOME=" + callerHome, "USERPROFILE=" + callerHome, "MULTICA_SERVER_URL=" + server.URL, "MULTICA_WORKSPACE_ID=" + workspaceID, "MULTICA_TOKEN=fixture-member"}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("CLI %v: %v %s", args, err, stderr.String())
		}
		if bytes.Contains(output, []byte("test-host-only")) || bytes.Contains(output, []byte("mjt_")) {
			t.Fatal("credential leaked to caller receipt")
		}
		var job protocol.MediaJob
		if err := json.Unmarshal(output, &job); err != nil {
			t.Fatalf("receipt: %v %s", err, output)
		}
		return job
	}
	executor, counter := antigravityNativeFixture(t, "ready")
	d := &Daemon{client: NewClient(server.URL), wsRPC: newWSRPCClient(time.Second), nativeMediaExecutors: map[string]media.Executor{"antigravity": executor}}
	d.client.SetToken("fixture-daemon")
	claim := func(t *testing.T, id string) *protocol.MediaAssignment {
		t.Helper()
		var result protocol.MediaClaimResponse
		if err := d.mediaRPC(ctx, "media.claim", protocol.MediaClaimRequest{RuntimeID: id}, &result); err != nil {
			t.Fatal(err)
		}
		return result.Assignment
	}
	run := func(t *testing.T, id string, assignment *protocol.MediaAssignment) protocol.MediaResult {
		t.Helper()
		if assignment == nil {
			t.Fatal("missing assignment")
		}
		result := d.executeMedia(ctx, Runtime{ID: id, Provider: "antigravity"}, *assignment)
		if err := d.mediaRPC(ctx, "media.result", result, nil); err != nil {
			t.Fatal(err)
		}
		return result
	}
	for _, provider := range []string{"agy", "auto"} {
		for _, capability := range []string{"image-generate", "image-edit"} {
			t.Run(provider+"/"+capability, func(t *testing.T) {
				mode := "ready"
				args := []string{"media", "generate", "--provider", provider, "--capability", capability, "--prompt", "A blue circle", "--async"}
				if capability == "image-edit" {
					mode = "edit"
					input := filepath.Join(callerHome, "input.png")
					if err := os.WriteFile(input, uploaded, 0600); err != nil {
						t.Fatal(err)
					}
					args = append(args, "--input", "subject="+input)
				}
				t.Setenv(agyimage.ModeEnv, mode)
				job := runCLI(t, args...)
				if job.State != "queued" || claim(t, wrongRuntime) != nil {
					t.Fatal("job not queued or unready runtime claimed it")
				}
				assignment := claim(t, runtimeID)
				if assignment == nil || assignment.Job.ID != job.ID || assignment.Job.RuntimeID != runtimeID {
					t.Fatalf("wrong assignment: %+v", assignment)
				}
				if result := run(t, runtimeID, assignment); result.Error != "" {
					t.Fatal(result.Error)
				}
				completed := runCLI(t, "media", "job", "wait", job.ID)
				if completed.State != "completed" || completed.AttachmentID == "" || completed.MarkdownURL == "" || !strings.HasPrefix(completed.Markdown, "![") {
					t.Fatalf("missing image receipt: %+v", completed)
				}
			})
		}
	}
	if uploads.Load() != 6 || downloads.Load() != 0 {
		t.Fatalf("uploads=%d downloads=%d", uploads.Load(), downloads.Load())
	}
	// A native quota receipt cools the host and requeues without an upload.
	job := runCLI(t, "media", "generate", "--provider", "auto", "--prompt", "A green square", "--async")
	t.Setenv(agyimage.ModeEnv, "quota-transcript")
	if result := run(t, runtimeID, claim(t, runtimeID)); !result.RateLimited {
		t.Fatalf("quota not propagated: %+v", result)
	}
	if queued := runCLI(t, "media", "job", "get", job.ID); queued.State != "queued" || claim(t, runtimeID) != nil {
		t.Fatal("quota did not cool and requeue")
	}
	fallback := fx.Runtime(t, "second agy host", runtimeCols, testutil.Cols{"daemon_id": uuid.NewString()})
	t.Setenv(agyimage.ModeEnv, "ready")
	if result := run(t, fallback, claim(t, fallback)); result.Error != "" {
		t.Fatal(result.Error)
	}
	if completed := runCLI(t, "media", "job", "wait", job.ID); completed.State != "completed" || completed.RuntimeID != fallback {
		t.Fatalf("reassignment failed: %+v", completed)
	}
	calls, err := os.ReadFile(counter)
	if err != nil || string(calls) != strings.Repeat("generate\n", 6) || uploads.Load() != 7 {
		t.Fatalf("model calls=%q uploads=%d err=%v", calls, uploads.Load(), err)
	}
	var attachments int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM attachment WHERE workspace_id=$1", workspaceID).Scan(&attachments); err != nil || attachments != 7 {
		t.Fatalf("expected five outputs and two inputs; attachments=%d err=%v", attachments, err)
	}
	for _, path := range []string{agyimage.TokenPath, ".codex/auth.json"} {
		if _, err := os.Stat(filepath.Join(callerHome, path)); !os.IsNotExist(err) {
			t.Fatal("caller unexpectedly has provider credentials")
		}
	}
	hostAuth, err := os.ReadFile(filepath.Join(executor.Home, agyimage.TokenPath))
	if err != nil || string(hostAuth) != agyimage.Token {
		t.Fatal("host authentication changed")
	}
}
