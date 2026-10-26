package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
)

// Expected-deny regression, explicitly enabled only on a disposable DB.
// A same-workspace daemon token must not claim another daemon's task.
func TestAIDODaemonRuntimeIsolation(t *testing.T) {
	if os.Getenv("NEX1565_SCOPE_PROBE") != "1" {
		t.Skip("explicit isolated diagnostic only")
	}
	ctx := context.Background()
	var runtimeID, agentID, taskID string
	otherDaemon := uuid.NewString()
	if err := testPool.QueryRow(ctx, `INSERT INTO agent_runtime(workspace_id,daemon_id,name,runtime_mode,provider,status,owner_id) VALUES($1,$2,'foreign synthetic','local','claude','online',$3) RETURNING id`, testWorkspaceID, otherDaemon, testUserID).Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO agent(workspace_id,name,runtime_mode,runtime_config,runtime_id,visibility,max_concurrent_tasks,owner_id) VALUES($1,'foreign agent','local','{}',$2,'workspace',1,$3) RETURNING id`, testWorkspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO agent_task_queue(agent_id,runtime_id,status,trigger_summary) VALUES($1,$2,'queued','EXTERNAL_SCOPE_PROBE synthetic third-party task') RETURNING id`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	raw := "mdt_" + uuid.NewString()
	if _, err := testPool.Exec(ctx, `INSERT INTO daemon_token(token_hash,workspace_id,daemon_id,expires_at) VALUES($1,$2,$3,now()+interval '5 minutes')`, auth.HashToken(raw), testWorkspaceID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest("POST", testServer.URL+"/api/daemon/runtimes/"+runtimeID+"/tasks/claim", bytes.NewBufferString("{}"))
	request.Header.Set("Authorization", "Bearer "+raw)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	// Never print the response: a successful claim contains a task credential.
	task, _ := result["task"].(map[string]any)
	claimed := task["id"] == taskID
	t.Logf("different-daemon same-workspace claim HTTP=%d foreign_task_returned=%v", response.StatusCode, claimed)
	if response.StatusCode != 403 && response.StatusCode != 404 {
		t.Fatal("dedicated mdt can claim another daemon's task; owner read capability not isolated")
	}
}

func TestDaemonBoundWorkspaceBootstrap(t *testing.T) {
	ctx := context.Background()
	raw := "mdt_" + uuid.NewString()
	if _, err := testPool.Exec(ctx, `INSERT INTO daemon_token(token_hash,workspace_id,daemon_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`, auth.HashToken(raw), testWorkspaceID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	defer testPool.Exec(ctx, `DELETE FROM daemon_token WHERE token_hash=$1`, auth.HashToken(raw))
	client := daemon.NewClient(testServer.URL)
	client.SetToken(raw)
	workspaces, err := client.ListWorkspaces(ctx)
	if err != nil || len(workspaces) != 1 || workspaces[0].ID != testWorkspaceID {
		t.Fatal("bootstrap escaped bound workspace", workspaces, err)
	}
	if response, err := client.RenewToken(ctx); err != nil || response.Renewed {
		t.Fatal("mdt used PAT renewal", response, err)
	}
	for _, item := range []struct {
		path, token string
		status      int
	}{
		{"/api/workspaces", raw, 401},
		{"/api/daemon/workspaces", testToken, 403},
		{"/api/daemon/workspaces", "mdt_invalid", 401},
	} {
		req, _ := http.NewRequest("GET", testServer.URL+item.path, nil)
		req.Header.Set("Authorization", "Bearer "+item.token)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != item.status {
			t.Fatalf("%s status=%d expected=%d", item.path, response.StatusCode, item.status)
		}
	}
}

// Opt-in, compiled by the staging runner and executed with a clean environment
// as UID490. The ordinary suite never starts a daemon or accesses protected paths.
func TestAIDOUidStage(t *testing.T) {
	if os.Getenv("NEX1565_UID_STAGE") != "1" {
		t.Skip("explicit isolated staging only")
	}
	if os.Getuid() != 490 {
		t.Fatal("requires UID490")
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	// TestMain already opened the isolated pool. Never inherit even that
	// synthetic server database connection string into the model child.
	t.Setenv("DATABASE_URL", "")
	root := os.Getenv("NEX1565_STAGE_ROOT")
	if root == "" {
		t.Fatal("missing private stage root")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	daemonID := uuid.NewString()
	// Trusted staging fixture pre-provisions the owner-bearing runtime.
	// mdt registration intentionally cannot invent a human owner identity.
	if _, err := testPool.Exec(ctx, `INSERT INTO agent_runtime(workspace_id,daemon_id,name,runtime_mode,provider,status,owner_id) VALUES($1,$2,'UID490 preprovisioned','local','claude','offline',$3)`, testWorkspaceID, daemonID, testUserID); err != nil {
		t.Fatal(err)
	}
	raw := "mdt_" + uuid.NewString() + uuid.NewString()
	if _, err := testPool.Exec(ctx, `INSERT INTO daemon_token(token_hash,workspace_id,daemon_id,expires_at) VALUES($1,$2,$3,now()+interval '10 minutes')`, auth.HashToken(raw), testWorkspaceID, daemonID); err != nil {
		t.Fatal(err)
	}
	profile := "nex1565-" + uuid.NewString()
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{Token: raw, ServerURL: testServer.URL, WorkspaceID: testWorkspaceID}, profile); err != nil {
		t.Fatal(err)
	}
	// Stage-only Seatbelt boundary: the daemon and every provider child inherit
	// this profile. Production PostgreSQL configuration is never touched.
	pgSandbox := os.Getenv("NEX1565_PG_PROFILE")
	if pgSandbox == "" {
		pgSandbox = filepath.Join(root, "pg-boundary.sb")
	}
	pgPolicy := `(version 1) (allow default) (deny network-outbound (remote ip "localhost:5432")) (deny network-outbound (local unix-socket))`
	if err := os.WriteFile(pgSandbox, []byte(pgPolicy), 0600); err != nil {
		t.Fatal(err)
	}
	profileHash := 0
	for _, b := range []byte(profile) {
		profileHash += int(b)
	}
	port := daemon.DefaultHealthPort + 1 + profileHash%1000
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	dlog, err := os.Create(filepath.Join(root, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer dlog.Close()
	d := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", "-f", pgSandbox, filepath.Join(root, "multica-stage"), "--profile", profile, "daemon", "start", "--foreground", "--no-auto-update", "--daemon-id", daemonID, "--device-name", "NEX1565 UID490", "--poll-interval", "100ms", "--heartbeat-interval", "1s", "--max-concurrent-tasks", "1")
	d.Env = append(os.Environ(), "PATH=/usr/bin:/bin", "SHELL=/usr/bin/false", "MULTICA_CLAUDE_PATH="+filepath.Join(root, "mock-model.py"), "MULTICA_WORKSPACES_ROOT="+filepath.Join(root, "workspaces"), "MULTICA_KEEP_ENV_AFTER_TASK=true", "MULTICA_GC_ENABLED=false")
	d.Stdout, d.Stderr = dlog, dlog
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Wait() }()
	defer func() {
		_ = d.Process.Signal(os.Interrupt)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("daemon shutdown: %v", err)
			}
		case <-time.After(20 * time.Second):
			_ = d.Process.Kill()
			<-done
			t.Error("daemon did not join")
		}
	}()
	var runtimeID string
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		err = testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id=$1 AND daemon_id=$2 AND provider='claude'`, testWorkspaceID, daemonID).Scan(&runtimeID)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if runtimeID == "" {
		t.Fatal("dedicated runtime never registered", err)
	}
	agent := func(name string, bound bool) string {
		var id string
		if err := testPool.QueryRow(ctx, `INSERT INTO agent(workspace_id,name,runtime_mode,runtime_config,runtime_id,visibility,max_concurrent_tasks,owner_id) VALUES($1,$2,'local','{}',$3,'workspace',1,$4) RETURNING id`, testWorkspaceID, name, runtimeID, testUserID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if bound {
			if _, err := testPool.Exec(ctx, `INSERT INTO aido_owner_binding(agent_id,runtime_id) VALUES($1,$2)`, id, runtimeID); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	owner, internal := agent("AIDO isolated synthetic", true), agent("internal synthetic", false)
	// Test server credentials exist only in this synthetic fixture, never daemon
	// environment. The provider receives its real task-scoped mat token.
	command := exec.CommandContext(ctx, os.Getenv("NEX1565_PYTHON"), filepath.Join(root, "driver.py"))
	command.Env = append(os.Environ(), "AIDO_OWNER_SERVER_URL="+testServer.URL, "AIDO_OWNER_TOKEN="+testToken, "AIDO_OWNER_WORKSPACE_ID="+testWorkspaceID, "AIDO_OWNER_AGENT_ID="+owner, "AIDO_OWNER_RUNTIME_ID="+runtimeID, "NEX1565_INTERNAL_AGENT="+internal)
	output, err := command.CombinedOutput()
	if err != nil {
		log, _ := os.ReadFile(filepath.Join(root, "daemon.log"))
		t.Fatalf("relay integration: %v: %s daemon: %s", err, output, log)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("driver output: %v %s", err, output)
	}
	t.Logf("UID=%d serverPID=%d daemonPID=%d workspace=%s runtime=%s owner=%s internal=%s result=%s", os.Getuid(), os.Getpid(), d.Process.Pid, testWorkspaceID, runtimeID, owner, internal, output)
	// Demonstrate credential type did not change to PAT during bootstrap.
	config, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil || config.Token != raw {
		t.Fatal("daemon credential changed", err)
	}
}
