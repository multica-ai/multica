package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

type environmentControlFixture struct {
	issueID, taskID, runtimeID string
}

func newEnvironmentControlFixture(t *testing.T, runtimeCols ...testutil.Cols) environmentControlFixture {
	t.Helper()
	suffix := fmt.Sprint(time.Now().UnixNano())
	runtimeID := dbfx.Runtime(t, "environment-control runtime "+suffix, runtimeCols...)
	agentID := dbfx.Agent(t, "environment-control agent "+suffix, runtimeID)
	issueID := dbfx.Issue(t, "environment-control issue")
	taskID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "running"})
	dbfx.Cleanup(t, `DELETE FROM issue_environment WHERE task_id = $1`, taskID)
	dbfx.Cleanup(t, `DELETE FROM issue_environment_command WHERE issue_id = $1`, issueID)
	dbfx.Cleanup(t, `DELETE FROM issue_environment_config WHERE issue_id = $1`, issueID)
	dbfx.Cleanup(t, `DELETE FROM activity_log WHERE issue_id = $1`, issueID)
	return environmentControlFixture{issueID: issueID, taskID: taskID, runtimeID: runtimeID}
}

func (f environmentControlFixture) report(t *testing.T, controllable bool, envs ...map[string]any) {
	t.Helper()
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/tasks/"+f.taskID+"/environments",
		map[string]any{"environments": envs, "controllable": controllable}), "taskId", f.taskID)
	testutil.Call(t, testHandler.ReportTaskEnvironments, req).Want(http.StatusOK)
}

func (f environmentControlFixture) list(t *testing.T, userID string) IssueEnvironmentsResponse {
	t.Helper()
	var out IssueEnvironmentsResponse
	testutil.Call(t, testHandler.ListIssueEnvironments,
		withURLParam(newRequestAs(userID, http.MethodGet, "/api/issues/"+f.issueID+"/environments", nil), "id", f.issueID)).
		Want(http.StatusOK).JSON(&out)
	return out
}

func (f environmentControlFixture) env(t *testing.T, name string) IssueEnvironmentResponse {
	t.Helper()
	for _, env := range f.list(t, testUserID).Environments {
		if env.Name == name {
			return env
		}
	}
	t.Fatalf("no environment %q", name)
	return IssueEnvironmentResponse{}
}

func (f environmentControlFixture) command(t *testing.T, req *http.Request, action, envID string) *testutil.Response {
	t.Helper()
	req = withURLParams(req, "id", f.issueID, "environmentId", envID)
	switch action {
	case "stop":
		return testutil.Call(t, testHandler.StopIssueEnvironment, req)
	case "restart":
		return testutil.Call(t, testHandler.RestartIssueEnvironment, req)
	default:
		return testutil.Call(t, testHandler.ReadIssueEnvironmentLogs, req)
	}
}

func (f environmentControlFixture) commandAs(t *testing.T, userID, action, envID string) *testutil.Response {
	t.Helper()
	return f.command(t, newRequestAs(userID, http.MethodPost, "/api/issues/"+f.issueID+"/environments/"+envID+"/"+action, nil), action, envID)
}

func (f environmentControlFixture) claim(t *testing.T) []DaemonEnvironmentCommand {
	t.Helper()
	var out ClaimEnvironmentCommandsResponse
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+f.runtimeID+"/environment-commands/claim", nil), "runtimeId", f.runtimeID)
	testutil.Call(t, testHandler.ClaimEnvironmentCommands, req).Want(http.StatusOK).JSON(&out)
	return out.Commands
}

func (f environmentControlFixture) result(t *testing.T, commandID string, body EnvironmentCommandResultRequest) *testutil.Response {
	t.Helper()
	req := withURLParams(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+f.runtimeID+"/environment-commands/"+commandID+"/result", body),
		"runtimeId", f.runtimeID, "commandId", commandID)
	return testutil.Call(t, testHandler.ReportEnvironmentCommandResult, req)
}

func (f environmentControlFixture) getCommand(t *testing.T, commandID string) IssueEnvironmentCommandResponse {
	t.Helper()
	var out IssueEnvironmentCommandResponse
	req := withURLParams(newRequest(http.MethodGet, "/api/issues/"+f.issueID+"/environment-commands/"+commandID, nil),
		"id", f.issueID, "commandId", commandID)
	testutil.Call(t, testHandler.GetIssueEnvironmentCommand, req).Want(http.StatusOK).JSON(&out)
	return out
}

func detectedEnvironment() map[string]any {
	return map[string]any{"key": "detected:abc", "source": "detected", "name": "vite", "url": "http://localhost:5173", "port": 5173, "state": "running"}
}

func TestEnvironmentStop_RoundTripsThroughTheDaemon(t *testing.T) {
	f := newEnvironmentControlFixture(t)
	f.report(t, true, webEnvironment("running"))
	web := f.env(t, "web")
	if !web.Controllable || !web.CanControl || web.PendingAction != nil {
		t.Fatalf("reported environment = %+v, want controllable by its machine's owner", web)
	}

	var queued IssueEnvironmentCommandResponse
	f.commandAs(t, testUserID, "stop", web.ID).Want(http.StatusAccepted).JSON(&queued)
	if queued.Action != "stop" || queued.Status != "pending" {
		t.Fatalf("queued command = %+v", queued)
	}
	if pending := f.env(t, "web").PendingAction; pending == nil || pending.ID != queued.ID {
		t.Fatalf("pending action = %+v, want the queued stop", pending)
	}
	f.commandAs(t, testUserID, "stop", web.ID).Want(http.StatusConflict)

	cmds := f.claim(t)
	if len(cmds) != 1 || cmds[0].ID != queued.ID || cmds[0].LocalKey != "config:web" || cmds[0].IssueID != f.issueID {
		t.Fatalf("claimed = %+v", cmds)
	}
	if again := f.claim(t); len(again) != 0 {
		t.Fatalf("a claimed command was handed out twice: %+v", again)
	}
	f.result(t, queued.ID, EnvironmentCommandResultRequest{OK: true}).Want(http.StatusOK)
	f.result(t, queued.ID, EnvironmentCommandResultRequest{OK: true}).Want(http.StatusNotFound)

	web = f.env(t, "web")
	if web.PendingAction != nil || web.LastAction == nil || web.LastAction.Status != "succeeded" ||
		web.LastAction.ActorID == nil || *web.LastAction.ActorID != testUserID {
		t.Fatalf("after the stop = %+v / %+v", web.PendingAction, web.LastAction)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM activity_log WHERE issue_id = $1 AND action = 'environment_controlled' AND details->>'action' = 'stop'`, f.issueID); n != 1 {
		t.Fatalf("stop activities = %d, want 1", n)
	}
}

func TestEnvironmentRestart_LinksTheNewEnvironment(t *testing.T) {
	f := newEnvironmentControlFixture(t)
	f.report(t, true, webEnvironment("exited"))
	web := f.env(t, "web")

	var queued IssueEnvironmentCommandResponse
	f.commandAs(t, testUserID, "restart", web.ID).Want(http.StatusAccepted).JSON(&queued)
	f.claim(t)
	f.result(t, queued.ID, EnvironmentCommandResultRequest{OK: true, EnvironmentKey: "config:web2"}).Want(http.StatusOK)

	restarted := webEnvironment("starting")
	restarted["key"] = "config:web2"
	restarted["name"] = "web2"
	f.report(t, true, restarted)
	if origin := f.env(t, "web2").Origin; origin == nil || origin.Action != "restart" || origin.ActorID == nil || *origin.ActorID != testUserID {
		t.Fatalf("restarted environment origin = %+v", origin)
	}
}

func TestEnvironmentFailedCommandReportsItsError(t *testing.T) {
	f := newEnvironmentControlFixture(t)
	f.report(t, true, webEnvironment("exited"))
	web := f.env(t, "web")
	var queued IssueEnvironmentCommandResponse
	f.commandAs(t, testUserID, "restart", web.ID).Want(http.StatusAccepted).JSON(&queued)
	f.claim(t)
	f.result(t, queued.ID, EnvironmentCommandResultRequest{Error: "the code this environment ran from has been cleaned up"}).Want(http.StatusOK)
	got := f.getCommand(t, queued.ID)
	if got.Status != "failed" || !strings.Contains(got.Error, "cleaned up") {
		t.Fatalf("failed command = %+v", got)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM activity_log WHERE issue_id = $1 AND action = 'environment_controlled'`, f.issueID); n != 0 {
		t.Fatalf("a failed restart recorded %d activities", n)
	}
}

func TestEnvironmentLogs_AreReadOnDemandAndRedacted(t *testing.T) {
	f := newEnvironmentControlFixture(t)
	f.report(t, true, webEnvironment("running"))
	web := f.env(t, "web")

	var queued IssueEnvironmentCommandResponse
	f.commandAs(t, testUserID, "logs", web.ID).Want(http.StatusAccepted).JSON(&queued)
	if queued.Output != nil {
		t.Fatal("a pending log read has no output")
	}
	f.claim(t)
	output := "listening\nERROR cannot reach postgres://app:hunter2@db.local/app\n"
	f.result(t, queued.ID, EnvironmentCommandResultRequest{OK: true, Output: &output}).Want(http.StatusOK)

	got := f.getCommand(t, queued.ID)
	if got.Status != "succeeded" || got.Output == nil {
		t.Fatalf("log read = %+v", got)
	}
	if strings.Contains(*got.Output, "hunter2") || !strings.Contains(*got.Output, "ERROR cannot reach [REDACTED") {
		t.Fatalf("log output = %q, want the connection string redacted", *got.Output)
	}
	if f.env(t, "web").LastAction != nil {
		t.Fatal("a log read is not an action on the environment")
	}

	dbfx.Exec(t, `UPDATE issue_environment_command SET created_at = now() - interval '11 minutes' WHERE id = $1`, queued.ID)
	if _, err := SweepEnvironmentLogCommands(t.Context(), testHandler.Queries, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM issue_environment_command WHERE id = $1`, queued.ID); n != 0 {
		t.Fatal("an old log read was kept")
	}
}

func TestEnvironmentCrashExcerptIsRedacted(t *testing.T) {
	f := newEnvironmentControlFixture(t)
	crashed := webEnvironment("exited")
	crashed["excerpt"] = "panic: dial postgres://app:hunter2@db.local/app\nexit"
	f.report(t, true, crashed)
	web := f.env(t, "web")
	if strings.Contains(web.Excerpt, "hunter2") || !strings.HasPrefix(web.Excerpt, "panic: dial [REDACTED") {
		t.Fatalf("excerpt = %q", web.Excerpt)
	}

	running := webEnvironment("running")
	running["excerpt"] = "only kept for a crash"
	f.report(t, true, running)
	if got := f.env(t, "web").Excerpt; got != "" {
		t.Fatalf("running environment excerpt = %q", got)
	}
}

func TestEnvironmentCommandsAreRefusedWhereTheyCannotWork(t *testing.T) {
	f := newEnvironmentControlFixture(t)
	f.report(t, true, webEnvironment("exited"), detectedEnvironment())
	web, vite := f.env(t, "web"), f.env(t, "vite")

	f.commandAs(t, testUserID, "stop", web.ID).Want(http.StatusConflict)
	f.commandAs(t, testUserID, "restart", vite.ID).Want(http.StatusConflict)
	f.commandAs(t, testUserID, "logs", vite.ID).Want(http.StatusConflict)
	f.commandAs(t, testUserID, "stop", vite.ID).Want(http.StatusAccepted)

	f.report(t, true, webEnvironment("exited"), detectedEnvironment(),
		map[string]any{"key": "detected:app", "source": "detected", "kind": "app", "name": "Multica Canary", "state": "running"},
		map[string]any{"key": "config:desktop", "source": "config", "kind": "app", "name": "desktop", "state": "running"})
	f.commandAs(t, testUserID, "stop", f.env(t, "Multica Canary").ID).Want(http.StatusConflict)
	// A desktop app the repository declares is supervised like any declared environment.
	f.commandAs(t, testUserID, "stop", f.env(t, "desktop").ID).Want(http.StatusAccepted)

	dbfx.Exec(t, `UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, f.runtimeID)
	f.commandAs(t, testUserID, "restart", web.ID).Want(http.StatusConflict)
	f.commandAs(t, testUserID, "logs", web.ID).Want(http.StatusConflict)
}

func TestEnvironmentFromAnOlderDaemonIsReadOnly(t *testing.T) {
	f := newEnvironmentControlFixture(t)
	f.report(t, false, webEnvironment("running"))
	web := f.env(t, "web")
	if web.Controllable || web.CanControl {
		t.Fatalf("environment from an older daemon = %+v", web)
	}
	f.commandAs(t, testUserID, "stop", web.ID).Want(http.StatusConflict)
	f.commandAs(t, testUserID, "logs", web.ID).Want(http.StatusConflict)
}

func TestEnvironmentControlPermissions(t *testing.T) {
	f := newEnvironmentControlFixture(t)
	f.report(t, true, webEnvironment("running"))
	web := f.env(t, "web")

	t.Run("a member who cannot use the private machine", func(t *testing.T) {
		member := plainMember(t, "env-control-member")
		if got := f.list(t, member).Environments[0]; got.CanControl {
			t.Fatal("a member who cannot use the machine is offered control")
		}
		f.commandAs(t, member, "stop", web.ID).Want(http.StatusForbidden)
		f.commandAs(t, member, "restart", web.ID).Want(http.StatusForbidden)
		// Reading the log only needs the issue.
		f.commandAs(t, member, "logs", web.ID).Want(http.StatusAccepted)
	})

	// Membership for every issue route is enforced by RequireWorkspaceMember;
	// acting on a machine checks it again.
	t.Run("someone outside the workspace", func(t *testing.T) {
		outsider := dbfx.User(t, "env-control-outsider", fmt.Sprintf("env-control-outsider-%d@multica.test", time.Now().UnixNano()))
		f.commandAs(t, outsider, "stop", web.ID).Want(http.StatusNotFound)
	})

	t.Run("an agent's task token", func(t *testing.T) {
		req := newRequest(http.MethodPost, "/api/issues/"+f.issueID+"/environments/"+web.ID+"/stop", nil)
		req.Header.Set("X-Actor-Source", "task_token")
		f.command(t, req, "stop", web.ID).Want(http.StatusForbidden)
	})

	t.Run("an environment of another issue", func(t *testing.T) {
		other := newEnvironmentControlFixture(t)
		req := withURLParams(newRequest(http.MethodPost, "/", nil), "id", other.issueID, "environmentId", web.ID)
		testutil.Call(t, testHandler.StopIssueEnvironment, req).Want(http.StatusNotFound)
	})

	t.Run("a public machine", func(t *testing.T) {
		shared := newEnvironmentControlFixture(t, testutil.Cols{"visibility": "public"})
		shared.report(t, true, webEnvironment("running"))
		member := plainMember(t, "env-control-public")
		if !shared.list(t, member).Environments[0].CanControl {
			t.Fatal("a member is not offered control of a public machine")
		}
		shared.commandAs(t, member, "stop", shared.env(t, "web").ID).Want(http.StatusAccepted)
	})
}

func TestEnvironmentCommandsStayWithTheirDaemon(t *testing.T) {
	f := newEnvironmentControlFixture(t)
	f.report(t, true, webEnvironment("running"))
	var queued IssueEnvironmentCommandResponse
	f.commandAs(t, testUserID, "stop", f.env(t, "web").ID).Want(http.StatusAccepted).JSON(&queued)

	foreign := withURLParam(newDaemonTokenRequest(http.MethodPost, "/", nil, testWorkspaceID, "attacker-daemon"), "runtimeId", f.runtimeID)
	testutil.Call(t, testHandler.ClaimEnvironmentCommands, foreign).Want(http.StatusNotFound)

	other := newEnvironmentControlFixture(t)
	if cmds := other.claim(t); len(cmds) != 0 {
		t.Fatalf("another runtime claimed %+v", cmds)
	}
	f.claim(t)
	// Another runtime cannot answer for this one.
	req := withURLParams(newOwnerDaemonRequest(http.MethodPost, "/", EnvironmentCommandResultRequest{OK: true}),
		"runtimeId", other.runtimeID, "commandId", queued.ID)
	testutil.Call(t, testHandler.ReportEnvironmentCommandResult, req).Want(http.StatusNotFound)
}

func TestEnvironmentUnansweredCommandsExpire(t *testing.T) {
	f := newEnvironmentControlFixture(t)
	f.report(t, true, webEnvironment("running"))
	web := f.env(t, "web")
	var queued IssueEnvironmentCommandResponse
	f.commandAs(t, testUserID, "stop", web.ID).Want(http.StatusAccepted).JSON(&queued)
	dbfx.Exec(t, `UPDATE issue_environment_command SET created_at = now() - interval '1 minute' WHERE id = $1`, queued.ID)

	if got := f.getCommand(t, queued.ID); got.Status != "expired" {
		t.Fatalf("unanswered command status = %q, want expired", got.Status)
	}
	if cmds := f.claim(t); len(cmds) != 0 {
		t.Fatalf("an expired command was carried out late: %+v", cmds)
	}
	got := f.env(t, "web")
	if got.PendingAction != nil || got.LastAction == nil || got.LastAction.Status != "expired" {
		t.Fatalf("after the command expired = %+v / %+v", got.PendingAction, got.LastAction)
	}
	// It no longer blocks another try.
	f.commandAs(t, testUserID, "stop", web.ID).Want(http.StatusAccepted)
}

func TestEnvironmentStartFromDeclaredConfig(t *testing.T) {
	f := newEnvironmentControlFixture(t)
	reportConfigs := func(envs ...DeclaredEnvironment) *testutil.Response {
		req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/tasks/"+f.taskID+"/environment-configs",
			ReportTaskEnvironmentConfigsRequest{Environments: envs}), "taskId", f.taskID)
		return testutil.Call(t, testHandler.ReportTaskEnvironmentConfigs, req)
	}
	reportConfigs(DeclaredEnvironment{Repo: "multica", Name: "BAD NAME", Start: "x"}).Want(http.StatusBadRequest)
	reportConfigs(DeclaredEnvironment{Repo: "../etc", Name: "web", Start: "x"}).Want(http.StatusBadRequest)
	reportConfigs(
		DeclaredEnvironment{Repo: "multica", Name: "web", Label: "Web app", Start: "make dev-web"},
		DeclaredEnvironment{Repo: "multica", Name: "api", Label: "API", Start: "make server"},
	).Want(http.StatusOK)

	list := f.list(t, testUserID)
	if len(list.Configs) != 1 || list.Configs[0].TaskID != f.taskID || !list.Configs[0].CanControl || len(list.Configs[0].Environments) != 2 {
		t.Fatalf("configs = %+v", list.Configs)
	}

	start := func(userID string, body StartIssueEnvironmentRequest) *testutil.Response {
		req := withURLParam(newRequestAs(userID, http.MethodPost, "/api/issues/"+f.issueID+"/environments/start", body), "id", f.issueID)
		return testutil.Call(t, testHandler.StartIssueEnvironment, req)
	}
	start(testUserID, StartIssueEnvironmentRequest{TaskID: f.taskID, Repo: "multica", Name: "worker"}).Want(http.StatusNotFound)
	start(plainMember(t, "env-start-member"), StartIssueEnvironmentRequest{TaskID: f.taskID, Repo: "multica", Name: "web"}).Want(http.StatusForbidden)

	var queued IssueEnvironmentCommandResponse
	start(testUserID, StartIssueEnvironmentRequest{TaskID: f.taskID, Repo: "multica", Name: "web"}).Want(http.StatusAccepted).JSON(&queued)
	if pending := f.list(t, testUserID).PendingStarts; len(pending) != 1 || pending[0].Name != "web" || pending[0].RuntimeID != f.runtimeID {
		t.Fatalf("pending starts = %+v", pending)
	}
	cmds := f.claim(t)
	if len(cmds) != 1 || cmds[0].Action != "start" || cmds[0].TaskID != f.taskID || cmds[0].Repo != "multica" || cmds[0].Name != "web" {
		t.Fatalf("claimed start = %+v", cmds)
	}

	// A later run that declares nothing clears the offer.
	reportConfigs().Want(http.StatusOK)
	if list := f.list(t, testUserID); len(list.Configs) != 1 || len(list.Configs[0].Environments) != 0 {
		t.Fatalf("configs after an empty report = %+v", list.Configs)
	}
}

func TestEnvironmentStartFailuresStayVisibleUntilRetried(t *testing.T) {
	f := newEnvironmentControlFixture(t)
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/tasks/"+f.taskID+"/environment-configs",
		ReportTaskEnvironmentConfigsRequest{Environments: []DeclaredEnvironment{
			{Repo: "multica", Name: "web", Start: "make dev-web"},
			{Repo: "multica", Name: "api", Start: "make server"},
		}}), "taskId", f.taskID)
	testutil.Call(t, testHandler.ReportTaskEnvironmentConfigs, req).Want(http.StatusOK)
	start := func(name string) IssueEnvironmentCommandResponse {
		var queued IssueEnvironmentCommandResponse
		req := withURLParam(newRequestAs(testUserID, http.MethodPost, "/api/issues/"+f.issueID+"/environments/start",
			StartIssueEnvironmentRequest{TaskID: f.taskID, Repo: "multica", Name: name}), "id", f.issueID)
		testutil.Call(t, testHandler.StartIssueEnvironment, req).Want(http.StatusAccepted).JSON(&queued)
		return queued
	}

	t.Run("failed after the machine accepted it", func(t *testing.T) {
		queued := start("web")
		f.claim(t)
		f.result(t, queued.ID, EnvironmentCommandResultRequest{Error: "the run's checkout was cleaned up"}).Want(http.StatusOK)

		list := f.list(t, testUserID)
		if len(list.PendingStarts) != 0 || len(list.FailedStarts) != 1 {
			t.Fatalf("pending = %+v, failed = %+v", list.PendingStarts, list.FailedStarts)
		}
		failed := list.FailedStarts[0]
		if failed.ID != queued.ID || failed.Status != "failed" || failed.Error != "the run's checkout was cleaned up" ||
			failed.TaskID == nil || *failed.TaskID != f.taskID {
			t.Fatalf("failed start = %+v", failed)
		}

		// Retrying replaces the failure with the new attempt.
		start("web")
		list = f.list(t, testUserID)
		if len(list.PendingStarts) != 1 || len(list.FailedStarts) != 0 {
			t.Fatalf("after retry: pending = %+v, failed = %+v", list.PendingStarts, list.FailedStarts)
		}
		f.claim(t)
	})

	t.Run("never answered", func(t *testing.T) {
		queued := start("api")
		dbfx.Exec(t, `UPDATE issue_environment_command SET created_at = now() - interval '1 minute' WHERE id = $1`, queued.ID)
		list := f.list(t, testUserID)
		var found *IssueEnvironmentActionResponse
		for i := range list.FailedStarts {
			if list.FailedStarts[i].ID == queued.ID {
				found = &list.FailedStarts[i]
			}
		}
		if found == nil || found.Status != "expired" {
			t.Fatalf("failed starts = %+v, want the unanswered start as expired", list.FailedStarts)
		}

		// An old failure is no longer news.
		dbfx.Exec(t, `UPDATE issue_environment_command SET created_at = now() - interval '20 minutes' WHERE id = $1`, queued.ID)
		for _, s := range f.list(t, testUserID).FailedStarts {
			if s.ID == queued.ID {
				t.Fatalf("a start that expired 20 minutes ago is still listed: %+v", s)
			}
		}
	})
}

func TestEnvironmentReportsKeepTheirRepository(t *testing.T) {
	f := newEnvironmentControlFixture(t)
	web := webEnvironment("running")
	web["repo"] = "multica"
	detected := detectedEnvironment()
	detected["repo"] = "multica"
	f.report(t, true, web, detected)
	if got := f.env(t, "web").Repo; got != "multica" {
		t.Fatalf("declared environment repo = %q", got)
	}
	if got := f.env(t, "vite").Repo; got != "" {
		t.Fatalf("detected environment repo = %q, want none", got)
	}
}
