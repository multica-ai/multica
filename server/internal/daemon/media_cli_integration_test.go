package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/testutil/agyimage"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The caller runs the actual CLI in an empty home with no provider credentials.
// Only the host executor can access the test subscription; all job scheduling,
// attachment transfers and receipts pass through real Server handlers and DB.
func TestMediaAntigravityCredentialSeparatedCLI(t *testing.T) {
	testMediaCredentialSeparatedCLI(t, "antigravity", "agy")
}

func TestMediaCodexCredentialSeparatedCLI(t *testing.T) {
	testMediaCredentialSeparatedCLI(t, "codex", "codex")
}

func testMediaCredentialSeparatedCLI(t *testing.T, runtimeProvider, mediaProvider string) {
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
	userID := fx.User(t, "image caller", uuid.NewString()+"@example.test")
	workspaceID := fx.Workspace(t, "image studio", uuid.NewString())
	fx.Member(t, workspaceID, userID, "owner")
	fx = testutil.New(pool, workspaceID, userID)
	runtimeCols := testutil.Cols{"provider": runtimeProvider, "daemon_id": uuid.NewString(), "metadata": fmt.Sprintf(`{"capabilities":[%q]}`, protocol.MediaCapabilityTag(mediaProvider, "image-generate"))}
	runtimeID := fx.Runtime(t, "image host", runtimeCols)
	wrongRuntime := fx.Runtime(t, "unready host", testutil.Cols{"provider": runtimeProvider, "daemon_id": uuid.NewString()})
	fx.Cleanup(t, "DELETE FROM attachment WHERE workspace_id=$1", workspaceID)
	fx.Cleanup(t, "DELETE FROM media_job WHERE workspace_id=$1", workspaceID)
	t.Setenv("LOCAL_UPLOAD_DIR", t.TempDir())
	h := &handler.Handler{Queries: db.New(pool), DB: pool, TxStarter: pool, Storage: storage.NewLocalStorageFromEnv(), Bus: events.New()}
	var uploads, downloads, issueEvents atomic.Int32
	h.Bus.Subscribe(protocol.EventIssueAttachmentsChanged, func(events.Event) { issueEvents.Add(1) })
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
	var executor media.Executor
	var counter, authPath, authContent string
	setMode := func(mode string) { t.Setenv(agyimage.ModeEnv, mode) }
	if mediaProvider == "agy" {
		native, calls := antigravityNativeFixture(t, "ready")
		executor, counter = native, calls
		authPath, authContent = filepath.Join(native.Home, agyimage.TokenPath), agyimage.Token
	} else {
		home := t.TempDir()
		authPath, authContent = filepath.Join(home, "auth.json"), `{"fixture":"test-host-only"}`
		if err := os.WriteFile(authPath, []byte(authContent), 0600); err != nil {
			t.Fatal(err)
		}
		command, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		counter = filepath.Join(t.TempDir(), "calls")
		t.Setenv("MULTICA_TEST_MEDIA_COUNTER", counter)
		executor = &media.Codex{Command: agent.NewCommand(command, nil), Home: home}
		setMode = func(mode string) {
			if mode == "quota-transcript" {
				mode = "turn-quota"
			}
			t.Setenv("MULTICA_TEST_MEDIA_PROBE", mode)
		}
	}
	d := &Daemon{client: NewClient(server.URL), wsRPC: newWSRPCClient(time.Second), nativeMediaExecutors: map[string]media.Executor{runtimeProvider: executor}}
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
		result := d.executeMedia(ctx, Runtime{ID: id, Provider: runtimeProvider}, *assignment)
		if err := d.mediaRPC(ctx, "media.result", result, nil); err != nil {
			t.Fatal(err)
		}
		return result
	}
	issueID := fx.Issue(t, "Image studio output", nil)
	promptFile := filepath.Join(callerHome, "prompt.txt")
	if err := os.WriteFile(promptFile, []byte("A blue circle"), 0600); err != nil {
		t.Fatal(err)
	}
	var referenceID string
	for _, provider := range []string{mediaProvider, "auto"} {
		for _, capability := range []string{"image-generate", "image-edit"} {
			t.Run(provider+"/"+capability, func(t *testing.T) {
				mode := "ready"
				args := []string{"media", "generate", "--provider", provider, "--capability", capability, "--prompt-file", promptFile, "--issue", issueID, "--async"}
				if capability == "image-edit" {
					mode = "edit"
					input := filepath.Join(callerHome, "input.png")
					if err := os.WriteFile(input, uploaded, 0600); err != nil {
						t.Fatal(err)
					}
					if provider == "auto" {
						args = append(args, "--input-attachment", "subject="+referenceID)
					} else {
						args = append(args, "--input", "subject="+input)
					}
				}
				setMode(mode)
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
				referenceID = completed.AttachmentID
				var boundIssue string
				if err := pool.QueryRow(ctx, "SELECT issue_id::text FROM attachment WHERE id=$1", completed.AttachmentID).Scan(&boundIssue); err != nil || boundIssue != issueID {
					t.Fatalf("output not bound to issue: %s %v", boundIssue, err)
				}
			})
		}
	}
	if uploads.Load() != 6 || downloads.Load() != 0 || issueEvents.Load() != 4 {
		t.Fatalf("uploads=%d downloads=%d issue events=%d", uploads.Load(), downloads.Load(), issueEvents.Load())
	}
	out := filepath.Join(callerHome, "result.png")
	// Download a completed job only when the caller explicitly requests a file.
	var lastJobID string
	if err := pool.QueryRow(ctx, "SELECT id::text FROM media_job WHERE attachment_id=$1", referenceID).Scan(&lastJobID); err != nil {
		t.Fatal(err)
	}
	runCLI(t, "media", "job", "wait", lastJobID, "--out", out)
	if data, err := os.ReadFile(out); err != nil || !bytes.Equal(data, uploaded) || downloads.Load() != 1 {
		t.Fatal("explicit download failed", err)
	}
	// A native quota receipt cools the host and requeues without an upload.
	job := runCLI(t, "media", "generate", "--provider", "auto", "--prompt", "A green square", "--async")
	setMode("quota-transcript")
	if result := run(t, runtimeID, claim(t, runtimeID)); !result.RateLimited {
		t.Fatalf("quota not propagated: %+v", result)
	}
	if queued := runCLI(t, "media", "job", "get", job.ID); queued.State != "queued" || claim(t, runtimeID) != nil {
		t.Fatal("quota did not cool and requeue")
	}
	fallback := fx.Runtime(t, "second image host", runtimeCols, testutil.Cols{"daemon_id": uuid.NewString()})
	setMode("ready")
	if result := run(t, fallback, claim(t, fallback)); result.Error != "" {
		t.Fatal(result.Error)
	}
	if completed := runCLI(t, "media", "job", "wait", job.ID); completed.State != "completed" || completed.RuntimeID != fallback {
		t.Fatalf("reassignment failed: %+v", completed)
	}
	calls, err := os.ReadFile(counter)
	if err != nil || (mediaProvider == "agy" && string(calls) != strings.Repeat("generate\n", 6)) || (mediaProvider == "codex" && strings.Count(string(calls), "submit\n") != 6) || uploads.Load() != 7 {
		t.Fatalf("model calls=%q uploads=%d err=%v", calls, uploads.Load(), err)
	}
	var attachments int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM attachment WHERE workspace_id=$1", workspaceID).Scan(&attachments); err != nil || attachments != 6 {
		t.Fatalf("expected five outputs and one uploaded input; attachments=%d err=%v", attachments, err)
	}
	for _, path := range []string{agyimage.TokenPath, ".codex/auth.json"} {
		if _, err := os.Stat(filepath.Join(callerHome, path)); !os.IsNotExist(err) {
			t.Fatal("caller unexpectedly has provider credentials")
		}
	}
	hostAuth, err := os.ReadFile(authPath)
	if err != nil || string(hostAuth) != authContent {
		t.Fatal("host authentication changed")
	}
}
