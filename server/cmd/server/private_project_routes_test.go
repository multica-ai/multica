package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A route parameter is keyed by the literal path segment before it and its
// own name, "autopilots/{id}" or "runs/{runId}": `{id}` alone names a
// different kind of object under every resource.

type privateRouteResolver func(f *privateRouteFixture, caller string) string

// projectScopedRouteParams resolves each route parameter that can name an
// object inside a project (the project itself, or an object reached through
// one of its issues, autopilots or runs) to such an object in the fixture's
// private project. The resolver gets the caller for objects that are the
// caller's own, like a notification. Every route declaring one of these is
// probed by TestEveryRouteByProjectObjectIDHidesPrivateProjects.
var projectScopedRouteParams = map[string]privateRouteResolver{
	"projects/{id}":                    func(f *privateRouteFixture, _ string) string { return f.projectID },
	"resources/{resourceId}":           func(f *privateRouteFixture, _ string) string { return f.projectResource() },
	"issue-views/{id}":                 func(f *privateRouteFixture, _ string) string { return f.projectView() },
	"issues/{id}":                      func(f *privateRouteFixture, _ string) string { return f.issue() },
	"comments/{commentId}":             func(f *privateRouteFixture, _ string) string { return f.comment() },
	"supplements/{commentId}":          func(f *privateRouteFixture, _ string) string { return f.supplement() },
	"attachments/{id}":                 func(f *privateRouteFixture, _ string) string { return f.attachment() },
	"wakeups/{wakeupID}":               func(f *privateRouteFixture, _ string) string { return f.issueWakeup() },
	"environments/{environmentId}":     func(f *privateRouteFixture, _ string) string { return f.environment() },
	"environment-commands/{commandId}": func(f *privateRouteFixture, _ string) string { return f.environmentCommand() },
	"pull-requests/{prId}":             func(f *privateRouteFixture, _ string) string { return f.pullRequest() },
	"tasks/{taskId}":                   func(f *privateRouteFixture, _ string) string { return f.issueRun() },
	"autopilots/{id}":                  func(f *privateRouteFixture, _ string) string { return f.autopilot() },
	"runs/{runId}":                     func(f *privateRouteFixture, _ string) string { return f.autopilotRun() },
	"triggers/{triggerId}":             func(f *privateRouteFixture, _ string) string { return f.autopilotTrigger() },
	"deliveries/{deliveryId}":          func(f *privateRouteFixture, _ string) string { return f.webhookDelivery() },
	"inbox/{id}":                       func(f *privateRouteFixture, caller string) string { return f.inboxItem(caller) },
	// The inbox is where a member route names an issue as {issueId}; the
	// caller has a notification about it, so marking it read has an effect.
	"issues/{issueId}": func(f *privateRouteFixture, caller string) string { f.inboxItem(caller); return f.issue() },
}

// outsideProjectParam is a route parameter that cannot name an object inside a
// project. value is what a probed route gets for it; nil when no probed route
// declares it.
type outsideProjectParam struct {
	why   string
	value func(f *privateRouteFixture) string
}

// routeParamsOutsideProjects lists every other route parameter with the reason
// it cannot name a private project's object. A route declaring only these is
// not probed.
var routeParamsOutsideProjects = map[string]outsideProjectParam{
	"workspaces/{id}":                {why: "the workspace itself"},
	"members/{memberId}":             {why: "a workspace membership"},
	"invitations/{id}":               {why: "the caller's own workspace invitation"},
	"invitations/{invitationId}":     {why: "a workspace invitation"},
	"share-links/{code}":             {why: "a workspace join link"},
	"share-links/{linkId}":           {why: "a workspace join link"},
	"tokens/{id}":                    {why: "the caller's own access token"},
	"checkout-sessions/{sessionId}":  {why: "a billing checkout"},
	"themes/{themeId}":               {why: "a theme store entry"},
	"screenshots/{mode}":             {why: "a theme screenshot variant"},
	"avatars/{sig}":                  {why: "a signed avatar URL"},
	"plugin-surfaces/{token}":        {why: "a signed plugin surface URL"},
	"runtimes/{runtimeId}":           {why: "a workspace runtime; its runs are listed, not fetched through it"},
	"update/{updateId}":              {why: "a runtime update request"},
	"models/{requestId}":             {why: "a runtime model-list request"},
	"local-skills/{requestId}":       {why: "a runtime local-skill request"},
	"import/{requestId}":             {why: "a runtime local-skill import request"},
	"runtime-profiles/{profileId}":   {why: "a workspace runtime profile"},
	"agents/{id}":                    {why: "a workspace agent; its runs are listed, not fetched through it"},
	"pinned-agents/{agentId}":        {why: "the caller's pinned agent"},
	"skills/{id}":                    {why: "a workspace skill"},
	"skills/{skillId}":               {why: "a workspace skill"},
	"files/{fileId}":                 {why: "a skill file"},
	"versions/{version}":             {why: "a skill version"},
	"mcp-servers/{serverId}":         {why: "a workspace MCP server"},
	"squads/{id}":                    {why: "a workspace squad"},
	"labels/{id}":                    {why: "a workspace label"},
	"labels/{labelId}":               {why: "a workspace label", value: func(f *privateRouteFixture) string { return f.shared.labelID }},
	"properties/{id}":                {why: "a workspace issue property definition"},
	"properties/{propertyId}":        {why: "a workspace issue property definition", value: func(f *privateRouteFixture) string { return f.shared.propertyID }},
	"quick-actions/{id}":             {why: "a workspace quick action"},
	"quick-actions/{quickActionId}":  {why: "a workspace quick action", value: func(f *privateRouteFixture) string { return f.shared.quickActionID }},
	"issue-statuses/{id}":            {why: "a workspace issue status"},
	"issue-workflows/{id}":           {why: "a workspace workflow; the projects using it are filtered out of its payload"},
	"handoffs/{status}":              {why: "a workflow handoff status key"},
	"project-folders/{id}":           {why: "a workspace folder of projects"},
	"docs/{id}":                      {why: "a workspace doc"},
	"doc-folders/{id}":               {why: "a workspace folder of docs"},
	"comments/{docCommentId}":        {why: "a discussion on a workspace doc"},
	"versions/{number}":              {why: "a version of a workspace doc; the run that wrote it is recorded by id only"},
	"metadata/{key}":                 {why: "a metadata key", value: func(*privateRouteFixture) string { return "route-probe" }},
	"system-wakeups/{rule}":          {why: "a system wakeup rule name", value: func(*privateRouteFixture) string { return "child_done" }},
	"members/{userId}":               {why: "a user", value: func(f *privateRouteFixture) string { return f.fx.UserID }},
	"collaborators/{userId}":         {why: "a user", value: func(f *privateRouteFixture) string { return f.fx.UserID }},
	"sessions/{sessionId}":           {why: "a chat or agent-builder session, which only its creator reaches; a hidden project drops out of its runs' context instead"},
	"queued-tasks/{taskId}":          {why: "a run of the caller's own chat session"},
	"draft-restores/{restoreId}":     {why: "a draft of the caller's own chat session"},
	"wakeups/{wakeupId}":             {why: "a wakeup of the caller's own chat session"},
	"pins/{itemType}":                {why: "the caller's own pin, removed without reading what it names"},
	"pins/{itemId}":                  {why: "the caller's own pin, removed without reading what it names"},
	"app-requests/{id}":              {why: "an app action request, answered only by the human it was raised for"},
	"connections/{id}":               {why: "an app or Composio connection"},
	"connections/{connectionId}":     {why: "a VCS connection"},
	"installations/{installationId}": {why: "a chat or VCS integration installation"},
	"plugins/{installationId}":       {why: "a plugin installation"},
	"packages/{packageId}":           {why: "a plugin package"},
	"mcp/{hookKey}":                  {why: "a plugin MCP hook"},
	"mcp/{app}":                      {why: "a built-in remote MCP app slug, reached with a task token"},
	"surfaces/{surfaceKey}":          {why: "a plugin surface"},
	"groups/{conversationId}":        {why: "a DingTalk group"},
	"install/{sessionId}":            {why: "an integration install session"},
	"logins/{loginId}":               {why: "a Weixin login session"},
}

// routePrefixesNotProbed are route trees a member session cannot probe, each
// with the reason.
var routePrefixesNotProbed = map[string]string{
	"/api/daemon/":           "daemon credentials: a runtime reaches the runs it executes, scoped by runtime ownership",
	"/v1/":                   "Plugin API: plugin installation tokens only",
	"/api/plugin-bridge/v1/": "plugin actions need an installed plugin, and the plugin API applies no project visibility yet (left as a decision in #10316)",
	"/api/webhooks/":         "inbound webhooks, authenticated by their own token or signature",
}

// privateProjectRoutesNotProbed lists routes ("METHOD pattern") declaring a
// project-scoped parameter that the probe cannot judge, each with the reason.
var privateProjectRoutesNotProbed = map[string]string{
	"GET /api/attachments/{id}/signed-download": "the signed URL is the credential, not a session; it is minted only by the attachment routes probed here",
	"POST /api/projects/{id}/join":              "the workspace owner's way into a private project they cannot see; TestWorkspaceOwnerJoinsPrivateProject covers who may call it",
}

// privateRouteRequest shapes the probe for a route an empty request cannot
// get past: body is its JSON body, and params override a parameter's default
// object with a more specific one.
type privateRouteRequest struct {
	body   func(f *privateRouteFixture, caller string) string
	params map[string]privateRouteResolver
}

var privateProjectRouteRequests = map[string]privateRouteRequest{
	"POST /api/issues/{id}/labels": {body: func(f *privateRouteFixture, _ string) string {
		return fmt.Sprintf(`{"label_id":%q}`, f.shared.labelID)
	}},
	"PUT /api/issues/{id}/metadata/{key}": {body: func(*privateRouteFixture, string) string { return `{"value":"probe"}` }},
	"POST /api/comments/{commentId}/sub-issues": {body: func(f *privateRouteFixture, _ string) string {
		return fmt.Sprintf(`{"mode":"manual","capture_token":%q,"issue":{"title":"Probe sub-issue"}}`, f.captureToken())
	}},
	"POST /api/tasks/{taskId}/retry-source-context": {params: map[string]privateRouteResolver{
		"tasks/{taskId}": func(f *privateRouteFixture, caller string) string { return f.quickCreateRun(caller) },
	}},
}

type privateRouteShared struct {
	insiderID                          string
	agentID, runtimeID                 string
	labelID, propertyID, quickActionID string
	uploadDir                          string
}

// privateRouteFixture is one route's private project: the insider is its
// member, the outsider is not. Objects are created on first use, so a route
// pays only for the parameters it declares. Every route gets a fresh one,
// because the insider's control call may change or delete what it names.
type privateRouteFixture struct {
	t         *testing.T
	fx        *dbfx.Fixture
	shared    privateRouteShared
	projectID string
	ids       map[string]string
}

func (f *privateRouteFixture) once(key string, create func() string) string {
	if id, ok := f.ids[key]; ok {
		return id
	}
	id := create()
	f.ids[key] = id
	return id
}

func (f *privateRouteFixture) issue() string {
	return f.once("issue", func() string {
		return f.fx.Issue(f.t, "Private route probe issue", dbfx.Cols{"project_id": f.projectID})
	})
}

func (f *privateRouteFixture) comment() string {
	return f.once("comment", func() string { return f.fx.Comment(f.t, f.issue(), "Private route probe comment") })
}

func (f *privateRouteFixture) issueRun() string {
	return f.once("issueRun", func() string {
		return f.fx.Task(f.t, f.shared.agentID, dbfx.Cols{
			"issue_id": f.issue(), "runtime_id": f.shared.runtimeID, "status": "failed",
			"originator_user_id": f.shared.insiderID, "accountable_user_id": f.shared.insiderID,
		})
	})
}

// supplement is a comment delivered to the issue's run as a supplement.
func (f *privateRouteFixture) supplement() string {
	return f.once("supplement", func() string {
		comment := f.comment()
		f.fx.InsertNoID(f.t, "task_supplement", dbfx.Cols{
			"task_id": f.issueRun(), "workspace_id": f.fx.WorkspaceID, "issue_id": f.issue(), "comment_id": comment,
			"author_id": f.shared.insiderID, "client_request_id": uuid.NewString(), "status": "failed",
		}, "comment_id = $1", comment)
		return comment
	})
}

// attachment is a file on the private issue, stored in the test's own upload
// directory: the member's download reads it and the member's delete removes
// only it.
func (f *privateRouteFixture) attachment() string {
	return f.once("attachment", func() string {
		const body = "probe"
		key := "workspaces/" + f.fx.WorkspaceID + "/" + uuid.NewString() + ".txt"
		path := filepath.Join(f.shared.uploadDir, filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			f.t.Fatal(err)
		}
		return f.fx.Insert(f.t, "attachment", dbfx.Cols{
			"workspace_id": f.fx.WorkspaceID, "issue_id": f.issue(), "uploader_type": "member",
			"uploader_id": f.shared.insiderID, "filename": "probe.txt", "url": "/uploads/" + key,
			"content_type": "text/plain", "size_bytes": len(body),
		})
	})
}

func (f *privateRouteFixture) issueWakeup() string {
	return f.once("issueWakeup", func() string {
		return f.fx.Insert(f.t, "issue_wakeup", dbfx.Cols{
			"id": uuid.NewString(), "workspace_id": f.fx.WorkspaceID, "issue_id": f.issue(),
			"agent_id": f.shared.agentID, "created_by": f.shared.insiderID, "instruction": "Probe",
			"kind": "event", "mode": "once", "event_types": dbfx.Raw("ARRAY['comment_created']"),
		})
	})
}

func (f *privateRouteFixture) environment() string {
	return f.once("environment", func() string {
		return f.fx.Insert(f.t, "issue_environment", dbfx.Cols{
			"workspace_id": f.fx.WorkspaceID, "issue_id": f.issue(), "task_id": f.issueRun(),
			"local_key": "probe", "source": "config", "name": "probe", "state": "running", "started_at": dbfx.Raw("now()"),
		})
	})
}

func (f *privateRouteFixture) environmentCommand() string {
	return f.once("environmentCommand", func() string {
		return f.fx.Insert(f.t, "issue_environment_command", dbfx.Cols{
			"workspace_id": f.fx.WorkspaceID, "issue_id": f.issue(), "runtime_id": f.shared.runtimeID,
			"environment_id": f.environment(), "action": "logs", "actor_type": "member", "actor_id": f.shared.insiderID,
		})
	})
}

func (f *privateRouteFixture) pullRequest() string {
	return f.once("pullRequest", func() string {
		prID := f.fx.Insert(f.t, "github_pull_request", dbfx.Cols{
			"workspace_id": f.fx.WorkspaceID, "installation_id": 1, "repo_owner": "multica-ai", "repo_name": "probe",
			"pr_number": 1, "title": "Probe", "state": "open", "html_url": "https://example.test/pull/1",
			"pr_created_at": dbfx.Raw("now()"), "pr_updated_at": dbfx.Raw("now()"),
		})
		issueID := f.issue()
		f.fx.InsertNoID(f.t, "issue_pull_request", dbfx.Cols{"issue_id": issueID, "pull_request_id": prID},
			"issue_id = $1 AND pull_request_id = $2", issueID, prID)
		return prID
	})
}

func (f *privateRouteFixture) autopilot() string {
	return f.once("autopilot", func() string {
		return f.fx.Insert(f.t, "autopilot", dbfx.Cols{
			"workspace_id": f.fx.WorkspaceID, "title": "Private route probe autopilot", "project_id": f.projectID,
			"assignee_type": "agent", "assignee_id": f.shared.agentID, "status": "paused", "execution_mode": "run_only",
			"created_by_type": "member", "created_by_id": f.shared.insiderID,
		})
	})
}

func (f *privateRouteFixture) autopilotTrigger() string {
	return f.once("autopilotTrigger", func() string {
		return f.fx.Insert(f.t, "autopilot_trigger", dbfx.Cols{
			"autopilot_id": f.autopilot(), "kind": "webhook", "enabled": false, "provider": "generic",
			"webhook_token": "probe-" + uuid.NewString(),
		})
	})
}

func (f *privateRouteFixture) autopilotRun() string {
	return f.once("autopilotRun", func() string {
		return f.fx.Insert(f.t, "autopilot_run", dbfx.Cols{"autopilot_id": f.autopilot(), "source": "manual", "status": "completed"})
	})
}

func (f *privateRouteFixture) webhookDelivery() string {
	return f.once("webhookDelivery", func() string {
		return f.fx.Insert(f.t, "webhook_delivery", dbfx.Cols{
			"workspace_id": f.fx.WorkspaceID, "autopilot_id": f.autopilot(), "trigger_id": f.autopilotTrigger(),
			"provider": "generic", "status": "failed", "raw_body": dbfx.Raw(`'\x7b7d'::bytea`),
		})
	})
}

func (f *privateRouteFixture) projectResource() string {
	return f.once("projectResource", func() string {
		return f.fx.Insert(f.t, "project_resource", dbfx.Cols{
			"project_id": f.projectID, "workspace_id": f.fx.WorkspaceID, "resource_type": "github_repo",
			"resource_ref": dbfx.Raw(`'{"url":"https://github.com/multica-ai/probe"}'::jsonb`),
		})
	})
}

func (f *privateRouteFixture) projectView() string {
	return f.once("projectView", func() string {
		return f.fx.Insert(f.t, "issue_view", dbfx.Cols{
			"workspace_id": f.fx.WorkspaceID, "owner_id": f.shared.insiderID, "name": "Private route probe view",
			"scope_type": "project", "scope_id": f.projectID, "visibility": "workspace", "query": dbfx.Raw("'{}'::jsonb"),
		})
	})
}

// inboxItem is the caller's own notification about the private issue, as the
// outsider would keep one from before they lost access to the project.
func (f *privateRouteFixture) inboxItem(caller string) string {
	return f.once("inbox:"+caller, func() string {
		return f.fx.Insert(f.t, "inbox_item", dbfx.Cols{
			"workspace_id": f.fx.WorkspaceID, "issue_id": f.issue(), "recipient_type": "member", "recipient_id": caller,
			"type": "new_comment", "severity": "info", "title": "Private route probe notice",
		})
	})
}

// captureToken is a current sub-issue capture of the private comment.
func (f *privateRouteFixture) captureToken() string {
	return f.once("captureToken", func() string {
		build, err := service.BuildSourceContext(context.Background(), db.New(f.fx.Pool),
			util.MustParseUUID(f.fx.WorkspaceID), util.MustParseUUID(f.comment()))
		if err != nil {
			f.t.Fatalf("capture source context: %v", err)
		}
		return build.Token
	})
}

// quickCreateRun is the caller's failed quick-create of a sub-issue branched
// from the private comment, which the caller may retry.
func (f *privateRouteFixture) quickCreateRun(caller string) string {
	return f.once("quickCreate:"+caller, func() string {
		contextID := uuid.NewString()
		qc, err := json.Marshal(service.QuickCreateContext{
			Type: service.QuickCreateContextType, Prompt: "Probe", RequesterID: caller, WorkspaceID: f.fx.WorkspaceID,
			ParentIssueID: f.issue(), SourceContextID: contextID,
		})
		if err != nil {
			f.t.Fatal(err)
		}
		taskID := f.fx.Task(f.t, f.shared.agentID, dbfx.Cols{
			"runtime_id": f.shared.runtimeID, "status": "failed", "context": string(qc),
			"originator_user_id": caller, "accountable_user_id": caller,
		})
		f.fx.Insert(f.t, "issue_source_context", dbfx.Cols{
			"id": contextID, "workspace_id": f.fx.WorkspaceID, "origin_task_id": taskID, "source_issue_id": f.issue(),
			"anchor_comment_id": f.comment(), "captured_by_user_id": caller, "snapshot_version": 1,
			"snapshot": dbfx.Raw("'{}'::jsonb"), "capture_digest": "probe", "state": "pending",
		})
		return taskID
	})
}

type routeParamRef struct{ segment, name string }

func (p routeParamRef) key() string { return p.segment + "/{" + p.name + "}" }

// routeParams returns the parameters a chi pattern declares, in order, each
// with the last literal segment before it.
func routeParams(pattern string) []routeParamRef {
	var params []routeParamRef
	literal := ""
	for _, segment := range strings.Split(strings.Trim(pattern, "/"), "/") {
		m := chiRouteParam.FindStringSubmatch(segment)
		if m == nil || m[0] != segment {
			literal = segment
			continue
		}
		params = append(params, routeParamRef{literal, m[1]})
	}
	return params
}

// routePath fills a pattern's parameters in declaration order.
func routePath(pattern string, values []string) string {
	i := 0
	path := chiRouteParam.ReplaceAllStringFunc(pattern, func(string) string {
		v := values[i]
		i++
		return v
	})
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

type routeAnswer struct {
	status int
	body   string
}

func (a routeAnswer) String() string {
	return fmt.Sprintf("%d %s", a.status, strings.TrimSpace(a.body))
}

// Every handler that loads an object by its own id must refuse one in a
// private project the caller cannot see, answering exactly as for an id that
// does not exist. Each handler has to remember that check, and it has been
// forgotten route by route. This walks the real router, so a new route is
// covered the moment it is registered: on every route declaring a
// project-scoped parameter, a workspace admin outside the project must get
// the answer a missing id gets, and a project member must not.
func TestEveryRouteByProjectObjectIDHidesPrivateProjects(t *testing.T) {
	fx := dbfx.New(testPool, testWorkspaceID, testUserID)
	fx.WorkspaceID = fx.Workspace(t, "Private project routes", "private-routes-"+uuid.NewString())
	fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
	// The insider's control calls create rows no fixture names.
	fx.Cleanup(t, `DELETE FROM project WHERE workspace_id = $1`, fx.WorkspaceID)
	fx.Cleanup(t, `DELETE FROM autopilot WHERE workspace_id = $1`, fx.WorkspaceID)
	fx.Cleanup(t, `DELETE FROM issue WHERE workspace_id = $1`, fx.WorkspaceID)
	fx.Cleanup(t, `DELETE FROM issue_source_context WHERE workspace_id = $1`, fx.WorkspaceID)

	// Both callers are admins, so role checks pass and only project
	// membership tells them apart: a workspace role grants no visibility.
	newAdmin := func(name string) (string, string) {
		email := "private-routes-" + uuid.NewString() + "@example.test"
		id := fx.User(t, name, email)
		fx.Member(t, fx.WorkspaceID, id, "admin")
		token, err := generateTestJWT(id, email, name)
		if err != nil {
			t.Fatalf("sign %s token: %v", name, err)
		}
		return id, token
	}
	insiderID, insiderToken := newAdmin("Private routes insider")
	outsiderID, outsiderToken := newAdmin("Private routes outsider")

	shared := privateRouteShared{insiderID: insiderID}
	shared.runtimeID = fx.Runtime(t, "Private routes runtime", dbfx.Cols{"owner_id": insiderID})
	// Anyone in the workspace may invoke the agent, so only visibility can
	// stop the outsider from starting its runs.
	shared.agentID = fx.Agent(t, "Private routes agent", shared.runtimeID, dbfx.Cols{
		"owner_id": insiderID, "visibility": "workspace", "permission_mode": "public_to",
	})
	fx.Insert(t, "agent_invocation_target", dbfx.Cols{"agent_id": shared.agentID, "target_type": "workspace", "target_id": fx.WorkspaceID})
	shared.labelID = fx.Insert(t, "issue_label", dbfx.Cols{"workspace_id": fx.WorkspaceID, "name": "probe", "color": "#000000"})
	shared.propertyID = fx.Insert(t, "issue_property", dbfx.Cols{"workspace_id": fx.WorkspaceID, "name": "Probe", "type": "text"})
	shared.quickActionID = fx.Insert(t, "quick_action", dbfx.Cols{
		"workspace_id": fx.WorkspaceID, "name": "Probe", "assignee_type": "agent", "assignee_id": shared.agentID,
		"prompt": "Probe", "created_by_type": "member", "created_by_id": insiderID,
	})

	// The insider's control calls read and delete stored files: keep them in
	// a directory of this test's own, never the runner's configured storage.
	t.Setenv("S3_BUCKET", "")
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "")
	shared.uploadDir = t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", shared.uploadDir)
	router, _ := NewRouterWithOptions(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, RouterOptions{})
	call := func(token, method, path, body string) routeAnswer {
		var req *http.Request
		switch method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			if body == "" {
				body = "{}"
			}
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		default:
			req = httptest.NewRequest(method, path, nil)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return routeAnswer{rec.Code, rec.Body.String()}
	}

	probe := func(t *testing.T, method, pattern string, params []routeParamRef, shape privateRouteRequest) {
		f := &privateRouteFixture{t: t, fx: fx, shared: shared, ids: map[string]string{}}
		f.projectID = fx.Project(t, "Private route probe project", dbfx.Cols{"visibility": projectaccess.VisibilityPrivate})
		for _, member := range []string{insiderID, fx.UserID} {
			fx.InsertNoID(t, "project_member", dbfx.Cols{"workspace_id": fx.WorkspaceID, "project_id": f.projectID, "user_id": member},
				"project_id = $1 AND user_id = $2", f.projectID, member)
		}
		// request returns the parameter values naming the fixture's objects
		// for the caller, the same values with every project object replaced
		// by an id that does not exist, and the request body.
		request := func(caller string) (existingValues, missingValues []string, body string) {
			for _, p := range params {
				resolve, scoped := shape.params[p.key()]
				if !scoped {
					resolve, scoped = projectScopedRouteParams[p.key()]
				}
				if scoped {
					existingValues = append(existingValues, resolve(f, caller))
					missingValues = append(missingValues, uuid.NewString())
					continue
				}
				outside := routeParamsOutsideProjects[p.key()]
				if outside.value == nil {
					t.Fatalf("%s declares %s next to a project-scoped parameter; give it a value in routeParamsOutsideProjects", pattern, p.key())
				}
				v := outside.value(f)
				existingValues = append(existingValues, v)
				missingValues = append(missingValues, v)
			}
			if shape.body != nil {
				body = shape.body(f, caller)
			}
			return existingValues, missingValues, body
		}

		existing, missing, body := request(outsiderID)
		notFound := call(outsiderToken, method, routePath(pattern, missing), body)
		hidden := call(outsiderToken, method, routePath(pattern, existing), body)
		// An answer that echoes the id it was asked about still matches.
		for i := range existing {
			hidden.body = strings.ReplaceAll(hidden.body, existing[i], missing[i])
		}
		if hidden != notFound {
			t.Errorf("a workspace admin outside the project got %v; an id that does not exist gets %v. Does the handler check the project visibility of the object it loads?", hidden, notFound)
		}

		// Control: the same request from a project member must not look like
		// a missing id, or the outsider's answer above proves nothing.
		existing, missing, body = request(insiderID)
		insiderNotFound := call(insiderToken, method, routePath(pattern, missing), body)
		if visible := call(insiderToken, method, routePath(pattern, existing), body); visible == insiderNotFound {
			t.Errorf("a project member got the missing-id answer too, %v: the fixture does not reach the object, or the route needs a request in privateProjectRouteRequests", visible)
		}
	}

	usedParams := map[string]bool{}
	usedPrefixes := map[string]bool{}
	walked := map[string]bool{}
	err := chi.Walk(router, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + pattern
		walked[key] = true
		for prefix := range routePrefixesNotProbed {
			if strings.HasPrefix(pattern, prefix) {
				usedPrefixes[prefix] = true
				return nil
			}
		}
		params := routeParams(pattern)
		scoped := false
		for _, p := range params {
			usedParams[p.key()] = true
			if _, ok := projectScopedRouteParams[p.key()]; ok {
				scoped = true
			} else if _, ok := routeParamsOutsideProjects[p.key()]; !ok {
				t.Errorf("%s declares %s: add it to projectScopedRouteParams with a fixture object if it can name an object inside a project, or to routeParamsOutsideProjects with the reason it cannot", key, p.key())
				return nil
			}
		}
		if !scoped {
			return nil
		}
		if _, exempt := privateProjectRoutesNotProbed[key]; exempt {
			return nil
		}
		t.Run(key, func(t *testing.T) { probe(t, method, pattern, params, privateProjectRouteRequests[key]) })
		return nil
	})
	if err != nil {
		t.Fatalf("walk router: %v", err)
	}

	for key := range projectScopedRouteParams {
		if !usedParams[key] {
			t.Errorf("projectScopedRouteParams lists %s, which no route outside routePrefixesNotProbed declares", key)
		}
	}
	for key := range routeParamsOutsideProjects {
		if !usedParams[key] {
			t.Errorf("routeParamsOutsideProjects lists %s, which no route outside routePrefixesNotProbed declares", key)
		}
	}
	for prefix := range routePrefixesNotProbed {
		if !usedPrefixes[prefix] {
			t.Errorf("routePrefixesNotProbed lists %s, which no route starts with", prefix)
		}
	}
	for key := range privateProjectRoutesNotProbed {
		if !walked[key] {
			t.Errorf("privateProjectRoutesNotProbed lists %q, which the router no longer registers", key)
		}
	}
	for key := range privateProjectRouteRequests {
		if !walked[key] {
			t.Errorf("privateProjectRouteRequests lists %q, which the router no longer registers", key)
		}
	}
}
