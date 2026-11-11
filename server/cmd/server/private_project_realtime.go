package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// privateProjectAudience narrows realtime fanout for events about a private
// project (MUL-8472): the project itself, its resources, its issues, their
// comments and their runs reach only project members and workspace
// owners/admins. Every other event keeps workspace fanout, and so does an
// event whose project cannot be resolved (a deleted issue's id, a
// workspace-level object): narrowing anything wider is how #9133 silenced
// realtime for whole workspaces.
type privateProjectAudience interface {
	// Audience reports whether e belongs to a private project and, if so, the
	// user ids that may receive it. err means the decision could not be made;
	// the caller drops the event rather than widen it.
	Audience(ctx context.Context, e events.Event) (userIDs []string, private bool, err error)
}

type audienceQuerier interface {
	ListPrivateProjectAudience(ctx context.Context, workspaceID pgtype.UUID) ([]db.ListPrivateProjectAudienceRow, error)
	ListWorkspaceManagerUserIDs(ctx context.Context, workspaceID pgtype.UUID) ([]pgtype.UUID, error)
	GetIssueProjectID(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error)
	GetCommentIssueProjectID(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error)
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	GetAutopilot(ctx context.Context, id pgtype.UUID) (db.Autopilot, error)
	projectaccess.TaskQuerier
}

// dbPrivateProjectAudience caches each workspace's private projects and their
// audience for audienceTTL. Producers run on the replica that handles the
// write, which invalidates its own cache on project and member events; other
// replicas converge within the TTL.
type dbPrivateProjectAudience struct {
	q   audienceQuerier
	ttl time.Duration
	now func() time.Time

	mu         sync.Mutex
	workspaces map[string]workspaceAudience
	tasks      map[taskAudienceKey]taskProjects
}

type taskAudienceKey struct {
	workspaceID string
	taskID      string
}

type taskProjects struct {
	loadedAt   time.Time
	projectIDs []string
}

type workspaceAudience struct {
	loadedAt time.Time
	// projects maps a private project id to its member user ids.
	projects map[string][]string
	managers []string
}

const (
	audienceTTL            = 2 * time.Second
	audienceResolveTimeout = 2 * time.Second
	maxTaskProjectCache    = 4096
)

func newPrivateProjectAudience(q audienceQuerier) *dbPrivateProjectAudience {
	return &dbPrivateProjectAudience{q: q, ttl: audienceTTL, now: time.Now,
		workspaces: map[string]workspaceAudience{}, tasks: map[taskAudienceKey]taskProjects{}}
}

// audienceInvalidatingEvents change who may see a private project.
func audienceInvalidatingEvent(eventType string) bool {
	return strings.HasPrefix(eventType, "project:") || strings.HasPrefix(eventType, "member:")
}

// workspaceVisibleTaskEvents carry only ids and status (task:failed has its
// error stripped by projectOutbound), and every member's agent presence,
// workload and run-history views refresh on them, so they keep workspace
// fanout for runs on private issues too. task:message and task:progress
// carry run content and stay narrowed.
var workspaceVisibleTaskEvents = map[string]bool{
	protocol.EventTaskQueued:                true,
	protocol.EventTaskCoalesced:             true,
	protocol.EventTaskDispatch:              true,
	protocol.EventTaskRunning:               true,
	protocol.EventTaskWaitingLocalDirectory: true,
	protocol.EventTaskUsage:                 true,
	protocol.EventTaskCompleted:             true,
	protocol.EventTaskFailed:                true,
	protocol.EventTaskCancelled:             true,
}

func (a *dbPrivateProjectAudience) Audience(ctx context.Context, e events.Event) ([]string, bool, error) {
	if e.WorkspaceID == "" {
		return nil, false, nil
	}
	a.invalidateTaskProjects(e)
	if workspaceVisibleTaskEvents[e.Type] {
		return nil, false, nil
	}
	if audienceInvalidatingEvent(e.Type) {
		a.mu.Lock()
		delete(a.workspaces, e.WorkspaceID)
		a.mu.Unlock()
	}
	ws, err := a.workspace(ctx, e.WorkspaceID)
	if err != nil {
		return nil, false, err
	}
	if len(ws.projects) == 0 {
		return nil, false, nil
	}
	projectIDs, err := a.eventProjectIDs(ctx, e)
	if err != nil {
		return nil, false, err
	}
	// A run can read more than one project (a quick-create into one project
	// under a parent in another); only members of every private one may see it.
	var members []string
	private := false
	for _, projectID := range projectIDs {
		projectMembers, ok := ws.projects[projectID]
		if !ok {
			continue
		}
		if !private {
			members, private = projectMembers, true
			continue
		}
		members = intersect(members, projectMembers)
	}
	if !private {
		return nil, false, nil
	}
	seen := make(map[string]struct{}, len(members)+len(ws.managers))
	out := make([]string, 0, len(members)+len(ws.managers))
	for _, ids := range [][]string{ws.managers, members} {
		for _, id := range ids {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out, true, nil
}

func intersect(a, b []string) []string {
	in := make(map[string]struct{}, len(b))
	for _, id := range b {
		in[id] = struct{}{}
	}
	out := a[:0:0]
	for _, id := range a {
		if _, ok := in[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

func (a *dbPrivateProjectAudience) workspace(ctx context.Context, workspaceID string) (workspaceAudience, error) {
	a.mu.Lock()
	cached, ok := a.workspaces[workspaceID]
	a.mu.Unlock()
	if ok && a.now().Sub(cached.loadedAt) < a.ttl {
		return cached, nil
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return workspaceAudience{}, nil
	}
	rows, err := a.q.ListPrivateProjectAudience(ctx, wsUUID)
	if err != nil {
		return workspaceAudience{}, err
	}
	loaded := workspaceAudience{loadedAt: a.now(), projects: map[string][]string{}}
	for _, row := range rows {
		pid := util.UUIDToString(row.ProjectID)
		members := loaded.projects[pid]
		if row.UserID.Valid {
			members = append(members, util.UUIDToString(row.UserID))
		}
		loaded.projects[pid] = members
	}
	if len(loaded.projects) > 0 {
		managers, err := a.q.ListWorkspaceManagerUserIDs(ctx, wsUUID)
		if err != nil {
			return workspaceAudience{}, err
		}
		for _, id := range managers {
			loaded.managers = append(loaded.managers, util.UUIDToString(id))
		}
	}
	a.mu.Lock()
	a.workspaces[workspaceID] = loaded
	a.mu.Unlock()
	return loaded, nil
}

// eventProjectIDs resolves the projects an event is about, or none. It reads
// the payload's own references first and only falls back to the database for
// a bare issue, comment, autopilot or task id.
func (a *dbPrivateProjectAudience) eventProjectIDs(ctx context.Context, e events.Event) ([]string, error) {
	refs := extractEventRefs(e.Type, e.Payload)
	if refs.projectKnown {
		return []string{refs.projectID}, nil
	}
	lookup := func(id string, get func(context.Context, pgtype.UUID) (pgtype.UUID, error)) ([]string, error) {
		uid, err := util.ParseUUID(id)
		if err != nil {
			return nil, nil
		}
		projectID, err := get(ctx, uid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []string{util.UUIDToString(projectID)}, nil
	}
	if refs.issueID != "" {
		return lookup(refs.issueID, a.q.GetIssueProjectID)
	}
	if refs.commentID != "" {
		return lookup(refs.commentID, a.q.GetCommentIssueProjectID)
	}
	if refs.autopilotID != "" {
		return lookup(refs.autopilotID, func(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error) {
			ap, err := a.q.GetAutopilot(ctx, id)
			return ap.ProjectID, err
		})
	}
	if e.TaskID != "" && e.ChatSessionID == "" {
		return a.taskProjectIDs(ctx, e)
	}
	return nil, nil
}

// Cache only project references; membership is always taken from workspace().
// The short TTL also bounds staleness on replicas that did not handle a move.
func (a *dbPrivateProjectAudience) taskProjectIDs(ctx context.Context, e events.Event) ([]string, error) {
	key := taskAudienceKey{workspaceID: e.WorkspaceID, taskID: e.TaskID}
	a.mu.Lock()
	cached, ok := a.tasks[key]
	a.mu.Unlock()
	if ok && a.now().Sub(cached.loadedAt) < a.ttl {
		return cached.projectIDs, nil
	}
	taskUUID, err := util.ParseUUID(e.TaskID)
	if err != nil {
		return nil, nil
	}
	task, err := a.q.GetAgentTask(ctx, taskUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	projects, err := projectaccess.TaskProjects(ctx, a.q, task)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(projects))
	for _, id := range projects {
		ids = append(ids, util.UUIDToString(id))
	}
	a.mu.Lock()
	// Bound memory even when completed runs never emit another event here.
	if len(a.tasks) >= maxTaskProjectCache {
		clear(a.tasks)
	}
	a.tasks[key] = taskProjects{loadedAt: a.now(), projectIDs: ids}
	a.mu.Unlock()
	return ids, nil
}

func (a *dbPrivateProjectAudience) invalidateTaskProjects(e events.Event) {
	if strings.HasPrefix(e.Type, "issue:") || strings.HasPrefix(e.Type, "autopilot:") || audienceInvalidatingEvent(e.Type) {
		a.mu.Lock()
		for key := range a.tasks {
			if key.workspaceID == e.WorkspaceID {
				delete(a.tasks, key)
			}
		}
		a.mu.Unlock()
		return
	}
	switch e.Type {
	case protocol.EventTaskQueued, protocol.EventTaskCompleted, protocol.EventTaskFailed, protocol.EventTaskCancelled:
		a.mu.Lock()
		delete(a.tasks, taskAudienceKey{workspaceID: e.WorkspaceID, taskID: e.TaskID})
		a.mu.Unlock()
	}
}

type eventRefs struct {
	// projectKnown is set when the payload states the project outright,
	// including an issue that states it has none.
	projectKnown bool
	projectID    string
	issueID      string
	commentID    string
	autopilotID  string
}

// extractEventRefs reads project / issue / comment references from an event
// payload. Payloads are maps of handler response structs, so this goes
// through their JSON form, the shape clients receive.
func extractEventRefs(eventType string, payload any) eventRefs {
	var refs eventRefs
	raw, err := json.Marshal(payload)
	if err != nil {
		return refs
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return refs
	}
	str := func(m map[string]json.RawMessage, key string) (string, bool) {
		v, ok := m[key]
		if !ok {
			return "", false
		}
		var s *string
		if json.Unmarshal(v, &s) != nil {
			return "", false
		}
		if s == nil {
			return "", true
		}
		return *s, true
	}
	object := func(key string) map[string]json.RawMessage {
		var m map[string]json.RawMessage
		if v, ok := top[key]; ok {
			_ = json.Unmarshal(v, &m)
		}
		return m
	}
	isProjectEvent := strings.HasPrefix(eventType, "project:") || strings.HasPrefix(eventType, "project_resource:")
	if isProjectEvent {
		if p := object("project"); p != nil {
			if id, ok := str(p, "id"); ok && id != "" {
				return eventRefs{projectKnown: true, projectID: id}
			}
		}
	}
	if id, ok := str(top, "project_id"); ok && id != "" && (isProjectEvent || eventType == protocol.EventProjectMembersChanged) {
		return eventRefs{projectKnown: true, projectID: id}
	}
	// An autopilot bound to a private project carries its prompt and runs.
	if strings.HasPrefix(eventType, "autopilot:") {
		if ap := object("autopilot"); ap != nil {
			if pid, ok := str(ap, "project_id"); ok {
				return eventRefs{projectKnown: true, projectID: pid}
			}
		}
		if id, ok := str(top, "autopilot_id"); ok && id != "" {
			return eventRefs{autopilotID: id}
		}
	}
	if issue := object("issue"); issue != nil {
		if pid, ok := str(issue, "project_id"); ok {
			return eventRefs{projectKnown: true, projectID: pid}
		}
		if id, ok := str(issue, "id"); ok {
			refs.issueID = id
		}
	}
	if refs.issueID == "" {
		if id, ok := str(top, "issue_id"); ok {
			refs.issueID = id
		}
	}
	if refs.issueID == "" {
		if c := object("comment"); c != nil {
			if id, ok := str(c, "issue_id"); ok && id != "" {
				refs.issueID = id
			} else if id, ok := str(c, "id"); ok {
				refs.commentID = id
			}
		}
	}
	if refs.issueID == "" && refs.commentID == "" {
		if id, ok := str(top, "comment_id"); ok {
			refs.commentID = id
		}
	}
	return refs
}

func logAudienceFailure(e events.Event, err error) {
	slog.Warn("realtime: dropping event, private project audience unresolved",
		"event_type", e.Type, "workspace_id", e.WorkspaceID, "task_id", e.TaskID, "error", err)
}
