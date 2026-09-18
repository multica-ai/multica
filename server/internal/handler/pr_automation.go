package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	pa "github.com/multica-ai/multica/server/internal/integrations/prautomation"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type prPolicyIssue struct {
	ID         string      `json:"id"`
	Identifier string      `json:"identifier"`
	Title      string      `json:"title"`
	Status     string      `json:"status"`
	Revision   int64       `json:"revision"`
	Terminal   bool        `json:"terminal"`
	Triage     bool        `json:"triage"`
	Disabled   bool        `json:"disabled"`
	Links      []pa.Link   `json:"links"`
	Removed    []pa.Link   `json:"removed"`
	Added      []pa.Link   `json:"added"`
	Excluded   []pa.Link   `json:"excluded"`
	Decision   pa.Decision `json:"decision"`
}
type prPolicyPlan struct {
	Policy   pa.Policy       `json:"policy"`
	Migrated bool            `json:"migrated"`
	Issues   []prPolicyIssue `json:"issues"`
	Pending  []string        `json:"pending"`
	Token    string          `json:"token"`
}
type prPolicyPR struct {
	pa.Link
	Branch       string
	Body         string
	Known        bool
	Updated      time.Time
	Installation int64
}

func readPRPolicy(ctx context.Context, d dbExecutor, ws pgtype.UUID) (pa.Policy, bool, error) {
	p := pa.Policy{Source: "title_branch"}
	err := d.QueryRow(ctx, `SELECT source, auto_complete, revision FROM pr_automation_policy WHERE workspace_id=$1`, ws).Scan(&p.Source, &p.AutoComplete, &p.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, false, nil
	}
	return p, err == nil, err
}

// Every policy/link writer serializes on the workspace. Issue row locks below
// also serialize completion against ordinary issue edits and manual reopening.
func (h *Handler) prPolicyTx(ctx context.Context, ws pgtype.UUID) (pgx.Tx, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,7429))`, uuidToString(ws)); err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	var locked pgtype.UUID
	if err = tx.QueryRow(ctx, "SELECT id FROM workspace WHERE id=$1 FOR SHARE", ws).Scan(&locked); err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func (h *Handler) prPolicyPlan(ctx context.Context, d dbExecutor, ws pgtype.UUID, proposed *pa.Policy, lock bool, issueID ...pgtype.UUID) (prPolicyPlan, error) {
	p, migrated, err := readPRPolicy(ctx, d, ws)
	if err != nil {
		return prPolicyPlan{}, err
	}
	if proposed != nil {
		p.Source = proposed.Source
		p.AutoComplete = proposed.AutoComplete
	}
	plan := prPolicyPlan{Policy: p, Migrated: migrated, Issues: []prPolicyIssue{}, Pending: []string{}}
	w, err := db.New(d).GetWorkspace(ctx, ws)
	if err != nil {
		return plan, err
	}
	prefix := issuePrefixForWorkspace(w)
	suffix := ""
	if lock {
		// Hold mirrored metadata stable through completion and preview apply.
		// Webhook upserts use these same rows even before taking the policy lock.
		for _, table := range []string{"github_pull_request", "vcs_pull_request"} {
			if _, err := d.Exec(ctx, "SELECT id FROM "+table+" WHERE workspace_id=$1 ORDER BY id FOR UPDATE", ws); err != nil {
				return plan, err
			}
		}
		suffix = " FOR UPDATE OF i"
	}
	rows, err := d.Query(ctx, `SELECT p.id::text,'github',p.title,p.html_url,p.state,COALESCE(p.branch,''),p.pr_updated_at,p.installation_id,
 EXISTS(SELECT 1 FROM github_installation g WHERE g.workspace_id=p.workspace_id AND g.installation_id=p.installation_id)
 AND COALESCE((w.settings->>'github_enabled')::boolean,true),e.body,e.observed_at,e.sync_error
 FROM github_pull_request p JOIN workspace w ON w.id=p.workspace_id LEFT JOIN pr_automation_evidence e ON e.pr_id=p.id
 WHERE p.workspace_id=$1
 UNION ALL
 SELECT p.id::text,p.provider,p.title,p.html_url,p.state,COALESCE(p.branch,''),p.pr_updated_at,0,
 EXISTS(SELECT 1 FROM vcs_connection c WHERE c.id=p.connection_id AND c.workspace_id=p.workspace_id),e.body,e.observed_at,e.sync_error
 FROM vcs_pull_request p LEFT JOIN pr_automation_evidence e ON e.pr_id=p.id WHERE p.workspace_id=$1 ORDER BY 1`, ws)
	if err != nil {
		return plan, err
	}
	prs := map[string]prPolicyPR{}
	order := []string{}
	unknownBody, conflict := false, false
	for rows.Next() {
		var pr prPolicyPR
		var body pgtype.Text
		var at pgtype.Timestamptz
		var syncError pgtype.Text
		if err = rows.Scan(&pr.PRID, &pr.Provider, &pr.Title, &pr.URL, &pr.State, &pr.Branch, &pr.Updated, &pr.Installation, &pr.Connected, &body, &at, &syncError); err != nil {
			rows.Close()
			return plan, err
		}
		pr.Body = body.String
		if syncError.Valid {
			if syncError.String == "conflicting observation" {
				plan.Pending = append(plan.Pending, "conflict:"+pr.PRID)
				conflict = true
			}
			pr.Connected = false
		}
		pr.Known = body.Valid && at.Valid && !at.Time.Before(pr.Updated)
		unknownBody = unknownBody || !pr.Known
		if pr.Provider != "github" && !h.isVCSAvailable() {
			pr.Connected = false
		}
		prs[pr.PRID] = pr
		order = append(order, pr.PRID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return plan, err
	}
	numbers := []int32{}
	for _, pr := range prs {
		for key := range pa.Identifiers(p, pr.Title, pr.Branch, pr.Body) {
			if n, ok := issueNumberForPrefix(key, prefix); ok {
				numbers = append(numbers, n)
			}
		}
	}
	filter := ` AND (i.number=ANY($2::int[]) OR EXISTS(SELECT 1 FROM issue_pull_request l WHERE l.issue_id=i.id)
 OR EXISTS(SELECT 1 FROM issue_vcs_pull_request l WHERE l.issue_id=i.id)
 OR EXISTS(SELECT 1 FROM pr_automation_override o WHERE o.issue_id=i.id))`
	args := []any{ws, numbers}
	if len(issueID) > 0 {
		filter = " AND i.id=$2"
		args = []any{ws, issueID[0]}
	}
	rows, err = d.Query(ctx, `SELECT i.id::text,i.number,i.title,i.status,i.revision,
 issue_effective_status(i.workspace_id,i.status) IN ('done','cancelled'),i.triage_state IS NOT NULL,
 COALESCE(a.disabled,false) FROM issue i LEFT JOIN pr_automation_issue a ON a.issue_id=i.id AND a.workspace_id=i.workspace_id
 WHERE i.workspace_id=$1`+filter+` ORDER BY i.id`+suffix, args...)
	if err != nil {
		return plan, err
	}
	byID := map[string]int{}
	byKey := map[string]int{}
	for rows.Next() {
		var item prPolicyIssue
		var number int32
		if err = rows.Scan(&item.ID, &number, &item.Title, &item.Status, &item.Revision, &item.Terminal, &item.Triage, &item.Disabled); err != nil {
			rows.Close()
			return plan, err
		}
		item.Identifier = fmt.Sprintf("%s-%d", prefix, number)
		item.Links = []pa.Link{}
		item.Removed = []pa.Link{}
		item.Added = []pa.Link{}
		item.Excluded = []pa.Link{}
		byID[item.ID] = len(plan.Issues)
		byKey[strings.ToUpper(item.Identifier)] = len(plan.Issues)
		plan.Issues = append(plan.Issues, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return plan, err
	}
	existing := map[string]map[string]bool{}
	overrides := map[string]map[string]string{}
	rows, err = d.Query(ctx, `SELECT l.issue_id::text,l.pull_request_id::text FROM issue_pull_request l JOIN issue i ON i.id=l.issue_id WHERE i.workspace_id=$1
 UNION ALL SELECT l.issue_id::text,l.pull_request_id::text FROM issue_vcs_pull_request l JOIN issue i ON i.id=l.issue_id WHERE i.workspace_id=$1`, ws)
	if err != nil {
		return plan, err
	}
	for rows.Next() {
		var iid, pid string
		if err = rows.Scan(&iid, &pid); err != nil {
			rows.Close()
			return plan, err
		}
		if existing[pid] == nil {
			existing[pid] = map[string]bool{}
		}
		existing[pid][iid] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return plan, err
	}
	rows, err = d.Query(ctx, `SELECT issue_id::text,pr_id::text,mode FROM pr_automation_override WHERE workspace_id=$1 ORDER BY issue_id,pr_id`, ws)
	if err != nil {
		return plan, err
	}
	for rows.Next() {
		var iid, pid, mode string
		if err = rows.Scan(&iid, &pid, &mode); err != nil {
			rows.Close()
			return plan, err
		}
		if overrides[pid] == nil {
			overrides[pid] = map[string]string{}
		}
		overrides[pid][iid] = mode
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return plan, err
	}
	for _, pid := range order {
		pr := prs[pid]
		wanted := map[string]string{}
		matches := pa.Identifiers(p, pr.Title, pr.Branch, pr.Body)
		if !migrated && proposed == nil {
			matches = nil
			for iid := range existing[pid] {
				wanted[iid] = "legacy"
			}
		}
		if !pr.Connected {
			matches = nil
			for iid := range existing[pid] {
				wanted[iid] = "pending"
			}
		}
		// Reuse the installation-wide identity resolver for every automatic match,
		// not only keywords. Explicit manual links are already workspace-scoped.
		policy := closeIntentPolicy{unrestricted: true}
		if pr.Provider == "github" && len(matches) > 0 {
			insts, e := db.New(d).ListGitHubInstallationsByInstallationID(ctx, pr.Installation)
			if e != nil {
				return plan, e
			}
			policy = h.resolvePRAutomationIdentity(ctx, d, insts, matches)
		}
		for key, source := range matches {
			if idx, ok := byKey[key]; ok {
				if overrides[pid][plan.Issues[idx].ID] != "" {
					continue
				} else if policy.permits(key, uuidToString(ws)) {
					wanted[plan.Issues[idx].ID] = source
				} else {
					plan.Pending = append(plan.Pending, "ambiguous:"+pid+":"+key)
					if existing[pid][plan.Issues[idx].ID] {
						wanted[plan.Issues[idx].ID] = "pending"
					}
				}
			}
		}
		if p.Source == "all" && !pr.Known {
			plan.Pending = append(plan.Pending, pid)
			for iid := range existing[pid] {
				if wanted[iid] == "" {
					wanted[iid] = "pending"
				}
			}
		}
		for iid, mode := range overrides[pid] {
			if mode == "manual" {
				wanted[iid] = "manual"
			} else {
				delete(wanted, iid)
				if idx, ok := byID[iid]; ok {
					plan.Issues[idx].Excluded = append(plan.Issues[idx].Excluded, pr.Link)
				}
			}
		}
		for _, iid := range sortedStringKeys(wanted) {
			idx, ok := byID[iid]
			if !ok {
				continue
			}
			link := pr.Link
			link.Source = wanted[iid]
			plan.Issues[idx].Links = append(plan.Issues[idx].Links, link)
			if !existing[pid][iid] {
				plan.Issues[idx].Added = append(plan.Issues[idx].Added, link)
			}
		}
		for _, iid := range sortedStringKeys(existing[pid]) {
			if _, ok := wanted[iid]; !ok {
				if idx, found := byID[iid]; found {
					plan.Issues[idx].Removed = append(plan.Issues[idx].Removed, pr.Link)
				}
			}
		}
	}
	for idx := range plan.Issues {
		i := &plan.Issues[idx]
		i.Decision = pa.Decide(p, i.Terminal, i.Triage, i.Disabled, i.Links)
		for _, pending := range plan.Pending {
			if strings.HasPrefix(pending, "ambiguous:") && strings.HasSuffix(pending, ":"+strings.ToUpper(i.Identifier)) && !i.Terminal {
				i.Decision.Reason = "ambiguous"
				i.Decision.Complete = false
			}
		}
		if !migrated && proposed == nil {
			i.Decision.Complete = false
			i.Decision.Reason = "legacy"
		}
		// Unknown bodies can contain another deliverable for any issue. Never claim
		// all PRs have been accounted for until this selected source is synchronized.
		if ((p.Source == "all" && unknownBody) || conflict) && i.Decision.Complete {
			i.Decision.Complete = false
			i.Decision.Reason = "sync_required"
		}
	}
	sort.Strings(plan.Pending)
	// Hash the evaluated impact, scoped to the workspace. Unrelated issue edits
	// cannot invalidate the preview; a new link or changed decision always does.
	encoded, _ := json.Marshal(struct {
		Workspace string
		Plan      prPolicyPlan
	}{uuidToString(ws), plan})
	sum := sha256.Sum256(encoded)
	plan.Token = hex.EncodeToString(sum[:])
	return plan, nil
}
func sortedStringKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type prCompleted struct {
	Before, After db.Issue
	Activity      db.ActivityLog
}

func (h *Handler) applyPRPlan(ctx context.Context, tx pgx.Tx, ws pgtype.UUID, plan prPolicyPlan) ([]prCompleted, error) {
	done := []prCompleted{}
	q := h.Queries.WithTx(tx)
	applied := 0
	for _, i := range plan.Issues {
		if len(i.Added) == 0 && len(i.Removed) == 0 && !i.Decision.Complete {
			continue
		}
		// Canonical links + terminal issue state are durable progress. Each next
		// pass recomputes the remaining diff under the latest policy, so turning
		// automation off cancels completion without draining an old command queue.
		if applied == 100 {
			break
		}
		applied++
		for _, link := range i.Removed {
			table := "issue_pull_request"
			if link.Provider != "github" {
				table = "issue_vcs_pull_request"
			}
			if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE issue_id=$1 AND pull_request_id=$2", i.ID, link.PRID); err != nil {
				return nil, err
			}
		}
		for _, link := range i.Added {
			table := "issue_pull_request"
			if link.Provider != "github" {
				table = "issue_vcs_pull_request"
			}
			if _, err := tx.Exec(ctx, "INSERT INTO "+table+" (issue_id,pull_request_id,linked_by_type,close_intent) VALUES ($1,$2,'system',false) ON CONFLICT (issue_id,pull_request_id) DO NOTHING", i.ID, link.PRID); err != nil {
				return nil, err
			}
		}
		if !i.Decision.Complete {
			continue
		}
		before, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: parseUUID(i.ID), WorkspaceID: ws})
		if err != nil {
			return nil, err
		}
		after, err := q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: before.ID, WorkspaceID: ws, Status: "done"})
		if err != nil {
			return nil, err
		}
		details, _ := json.Marshal(map[string]any{"from": before.Status, "to": "done", "source": "pr_automation", "policy_revision": plan.Policy.Revision, "pull_requests": i.Links})
		activity, err := q.CreateActivity(ctx, db.CreateActivityParams{WorkspaceID: ws, IssueID: before.ID, ActorType: strToText("system"), Action: "status_changed", Details: details})
		if err != nil {
			return nil, err
		}
		done = append(done, prCompleted{before, after, activity})
	}
	return done, nil
}
func (h *Handler) publishPRPlan(ctx context.Context, ws pgtype.UUID, done []prCompleted) {
	for _, item := range done {
		h.notifyParentOfChildDone(ctx, item.Before, item.After)
		resp := issueToResponse(item.After, h.getIssuePrefix(ctx, ws))
		h.fillStatusCategory(ctx, item.After.WorkspaceID, &resp)
		h.publish(protocol.EventIssueUpdated, uuidToString(ws), "system", "", map[string]any{"issue": resp, "status_changed": true, "prev_status": item.Before.Status, "source": "pr_automation", "status_activity": item.Activity})
	}
	h.publish(protocol.EventPullRequestUpdated, uuidToString(ws), "system", "", map[string]any{"linked_issue_ids": []string{}})
}

func (h *Handler) ReconcilePRPolicy(ctx context.Context, ws pgtype.UUID) error {
	tx, err := h.prPolicyTx(ctx, ws)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	plan, err := h.prPolicyPlan(ctx, tx, ws, nil, true)
	if err != nil {
		return err
	}
	if !plan.Migrated {
		return nil
	}
	done, err := h.applyPRPlan(ctx, tx, ws, plan)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE pr_automation_policy SET checked_at=now(),last_error=NULL WHERE workspace_id=$1`, ws); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	changed := len(done) > 0
	for _, i := range plan.Issues {
		changed = changed || len(i.Added) > 0 || len(i.Removed) > 0
	}
	if changed {
		h.publishPRPlan(ctx, ws, done)
	}
	return nil
}

// Store bodies while still on legacy policy, so switching sources later has
// evidence. Failure never opts a migrated workspace back into legacy rules.
func (h *Handler) recordPRPolicyEvidence(ctx context.Context, ws, pr pgtype.UUID, body string, at pgtype.Timestamptz) bool {
	_, err := h.DB.Exec(ctx, `INSERT INTO pr_automation_evidence(pr_id,workspace_id,body,observed_at) VALUES($1,$2,$3,$4)
 ON CONFLICT(pr_id) DO UPDATE SET body=EXCLUDED.body,observed_at=EXCLUDED.observed_at WHERE EXCLUDED.observed_at>=pr_automation_evidence.observed_at`, pr, ws, body, at)
	if err != nil {
		slog.Error("PR evidence write failed", "error", err)
	}
	_, migrated, readErr := readPRPolicy(ctx, h.DB, ws)
	if readErr != nil {
		slog.Error("PR policy read failed", "error", readErr)
		return true
	}
	if migrated {
		if err == nil {
			err = h.ReconcilePRPolicy(ctx, ws)
		}
		if err != nil {
			_, _ = h.DB.Exec(ctx, `UPDATE pr_automation_policy SET last_error=$2 WHERE workspace_id=$1`, ws, "synchronization failed")
			slog.Error("PR automation deferred", "error", err)
		}
		return true
	}
	return false
}

func (h *Handler) GetPRPolicy(w http.ResponseWriter, r *http.Request) {
	member, ok := h.requireWorkspaceMember(w, r, chi.URLParam(r, "id"), "workspace not found")
	if !ok {
		return
	}
	p, m, err := readPRPolicy(r.Context(), h.DB, member.WorkspaceID)
	if err != nil {
		writeError(w, 500, "cannot read PR policy")
		return
	}
	writeJSON(w, 200, map[string]any{"policy": p, "migrated": m})
}

type prPolicyRequest struct {
	pa.Policy
	Token string `json:"token"`
}

func (h *Handler) PreviewPRPolicy(w http.ResponseWriter, r *http.Request) {
	h.writePRPolicy(w, r, false)
}
func (h *Handler) UpdatePRPolicy(w http.ResponseWriter, r *http.Request) { h.writePRPolicy(w, r, true) }
func (h *Handler) writePRPolicy(w http.ResponseWriter, r *http.Request, apply bool) {
	member, ok := h.requireWorkspaceRole(w, r, chi.URLParam(r, "id"), "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	ws := member.WorkspaceID
	var req prPolicyRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req) != nil || !req.Policy.Valid() {
		writeError(w, 400, "invalid PR policy")
		return
	}
	tx, err := h.prPolicyTx(r.Context(), ws)
	if err != nil {
		writeError(w, 500, "cannot lock PR policy")
		return
	}
	defer tx.Rollback(r.Context())
	plan, err := h.prPolicyPlan(r.Context(), tx, ws, &req.Policy, apply)
	if err != nil {
		slog.Error("PR policy preview failed", "error", err)
		writeError(w, 500, "cannot evaluate PR policy")
		return
	}
	if !apply {
		writeJSON(w, 200, plan)
		return
	}
	if req.Token == "" || req.Token != plan.Token {
		writeJSON(w, 409, map[string]any{"error": "PR state changed; preview again", "preview": plan})
		return
	}
	plan.Policy.Revision++
	_, err = tx.Exec(r.Context(), `INSERT INTO pr_automation_policy(workspace_id,source,auto_complete,revision) VALUES($1,$2,$3,$4)
 ON CONFLICT(workspace_id) DO UPDATE SET source=EXCLUDED.source,auto_complete=EXCLUDED.auto_complete,revision=EXCLUDED.revision,updated_at=now()`, ws, plan.Policy.Source, plan.Policy.AutoComplete, plan.Policy.Revision)
	if err != nil {
		writeError(w, 500, "cannot save PR policy")
		return
	}
	done, err := h.applyPRPlan(r.Context(), tx, ws, plan)
	if err != nil {
		writeError(w, 500, "cannot apply PR policy")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "cannot commit PR policy")
		return
	}
	h.publishPRPlan(r.Context(), ws, done)
	plan.Migrated = true
	writeJSON(w, 200, plan)
}

func (h *Handler) GetIssuePRPolicy(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok = h.requireWorkspaceMember(w, r, uuidToString(issue.WorkspaceID), "issue not found"); !ok {
		return
	}
	plan, err := h.prPolicyPlan(r.Context(), h.DB, issue.WorkspaceID, nil, false, issue.ID)
	if err != nil {
		writeError(w, 500, "cannot evaluate PR policy")
		return
	}
	for _, item := range plan.Issues {
		if item.ID == uuidToString(issue.ID) {
			writeJSON(w, 200, map[string]any{"policy": plan.Policy, "migrated": plan.Migrated, "issue": item})
			return
		}
	}
	writeError(w, 404, "issue not found")
}

// The same endpoint handles visible issue exceptions and manual link control.
// Restoring automatic linking removes the explicit exclusion, not the PR.
func (h *Handler) UpdateIssuePRPolicy(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok = h.requireWorkspaceMember(w, r, uuidToString(issue.WorkspaceID), "issue not found"); !ok {
		return
	}
	var req struct {
		Disabled *bool  `json:"disabled"`
		PRID     string `json:"pr_id"`
		URL      string `json:"url"`
		Mode     string `json:"mode"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req) != nil {
		writeError(w, 400, "invalid request")
		return
	}
	if req.Disabled != nil && (req.Mode != "" || req.PRID != "" || req.URL != "") {
		writeError(w, 400, "choose one PR automation operation")
		return
	}
	if req.PRID != "" {
		if req.URL != "" {
			writeError(w, 400, "choose a PR id or URL")
			return
		}
		if _, ok := parseUUIDOrBadRequest(w, req.PRID, "PR id"); !ok {
			return
		}
	}
	if req.Disabled == nil && req.Mode != "manual" && req.Mode != "excluded" && req.Mode != "automatic" {
		writeError(w, 400, "invalid link mode")
		return
	}
	var snapshot *prPolicySnapshot
	if req.Disabled == nil && req.Mode == "manual" && strings.TrimSpace(req.URL) != "" {
		_, migrated, err := readPRPolicy(r.Context(), h.DB, issue.WorkspaceID)
		if err != nil || !migrated {
			writeError(w, 409, "migrate the workspace PR policy first")
			return
		}
		// Fetch before taking database locks; only configured origins are used.
		var known bool
		if err = h.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM github_pull_request WHERE workspace_id=$1 AND html_url=$2 UNION ALL SELECT 1 FROM vcs_pull_request WHERE workspace_id=$1 AND html_url=$2)`, issue.WorkspaceID, strings.TrimSpace(req.URL)).Scan(&known); err != nil {
			writeError(w, 500, "cannot resolve PR")
			return
		}
		if !known {
			if snapshot, err = h.fetchPRPolicyURL(r.Context(), issue.WorkspaceID, req.URL); err != nil {
				writeError(w, 422, err.Error())
				return
			}
		}
	}
	tx, err := h.prPolicyTx(r.Context(), issue.WorkspaceID)
	if err != nil {
		writeError(w, 500, "cannot lock PR policy")
		return
	}
	defer tx.Rollback(r.Context())
	_, migrated, err := readPRPolicy(r.Context(), tx, issue.WorkspaceID)
	if err != nil || !migrated {
		writeError(w, 409, "migrate the workspace PR policy first")
		return
	}
	// The fetched observation and manual association become visible together.
	// No scheduler/webhook can reconcile an intermediate, unlinked snapshot.
	if snapshot != nil {
		if err = snapshot.store(r.Context(), tx, issue.WorkspaceID); err != nil {
			writeError(w, 500, "cannot store PR metadata")
			return
		}
	}
	if req.Disabled != nil {
		// Lock the issue before changing its override, in the same order used by
		// normal issue writes and the reopen trigger.
		if _, err = tx.Exec(r.Context(), `SELECT id FROM issue WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, issue.ID, issue.WorkspaceID); err != nil {
			writeError(w, 500, "cannot lock issue")
			return
		}
		_, err = tx.Exec(r.Context(), `INSERT INTO pr_automation_issue(issue_id,workspace_id,disabled) VALUES($1,$2,$3) ON CONFLICT(issue_id) DO UPDATE SET disabled=EXCLUDED.disabled`, issue.ID, issue.WorkspaceID, *req.Disabled)
	} else {
		var id string
		err = tx.QueryRow(r.Context(), `SELECT id::text FROM github_pull_request WHERE workspace_id=$1 AND (id::text=$2 OR html_url=$3)
  UNION ALL SELECT id::text FROM vcs_pull_request WHERE workspace_id=$1 AND (id::text=$2 OR html_url=$3)`, issue.WorkspaceID, req.PRID, strings.TrimSpace(req.URL)).Scan(&id)
		if err != nil {
			writeError(w, 404, "PR is not synchronized in this workspace")
			return
		}
		if req.Mode == "manual" {
			var connected bool
			err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM github_pull_request p JOIN github_installation g ON g.workspace_id=p.workspace_id AND g.installation_id=p.installation_id JOIN workspace w ON w.id=p.workspace_id WHERE p.id::text=$1 AND p.workspace_id=$2 AND COALESCE((w.settings->>'github_enabled')::boolean,true))
 OR ($3 AND EXISTS(SELECT 1 FROM vcs_pull_request p JOIN vcs_connection c ON c.id=p.connection_id AND c.workspace_id=p.workspace_id WHERE p.id::text=$1 AND p.workspace_id=$2))`, id, issue.WorkspaceID, h.isVCSAvailable()).Scan(&connected)
			if err != nil || !connected {
				writeError(w, 409, "PR provider is disconnected or disabled")
				return
			}
		}
		if req.Mode == "automatic" {
			_, err = tx.Exec(r.Context(), `DELETE FROM pr_automation_override WHERE workspace_id=$1 AND issue_id=$2 AND pr_id=$3`, issue.WorkspaceID, issue.ID, id)
		} else {
			_, err = tx.Exec(r.Context(), `INSERT INTO pr_automation_override(issue_id,pr_id,workspace_id,mode) VALUES($1,$2,$3,$4)
   ON CONFLICT(issue_id,pr_id) DO UPDATE SET mode=EXCLUDED.mode`, issue.ID, id, issue.WorkspaceID, req.Mode)
		}
	}
	if err != nil {
		writeError(w, 500, "cannot update PR automation")
		return
	}
	plan, err := h.prPolicyPlan(r.Context(), tx, issue.WorkspaceID, nil, true, issue.ID)
	if err != nil {
		writeError(w, 500, "cannot evaluate PR policy")
		return
	}
	// A local action must not finish unrelated tasks discovered by the planner.
	local := plan
	local.Issues = nil
	for _, i := range plan.Issues {
		if i.ID == uuidToString(issue.ID) {
			local.Issues = append(local.Issues, i)
		}
	}
	done, err := h.applyPRPlan(r.Context(), tx, issue.WorkspaceID, local)
	if err != nil {
		writeError(w, 500, "cannot apply issue PR policy")
		return
	}
	details, _ := json.Marshal(req)
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	activity, err := h.Queries.WithTx(tx).CreateActivity(r.Context(), db.CreateActivityParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, ActorType: strToText(actorType), ActorID: parseUUID(actorID), Action: "pr_automation_updated", Details: details})
	if err != nil {
		writeError(w, 500, "cannot record PR policy change")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "cannot commit PR policy")
		return
	}
	h.publish(protocol.EventActivityCreated, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{
		"issue_id": uuidToString(issue.ID), "entry": map[string]any{"type": "activity", "id": uuidToString(activity.ID), "actor_type": actorType, "actor_id": actorID, "action": activity.Action, "details": json.RawMessage(activity.Details), "created_at": timestampToString(activity.CreatedAt)},
	})
	h.publishPRPlan(r.Context(), issue.WorkspaceID, done)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// Scheduled repair covers dropped webhook processing, issue acceptance,
// connection recovery and work left unfinished by an interrupted request.
func (h *Handler) ReconcilePRPolicies(ctx context.Context) error {
	rows, err := h.DB.Query(ctx, `SELECT workspace_id FROM pr_automation_policy ORDER BY checked_at NULLS FIRST,workspace_id LIMIT 20`)
	if err != nil {
		return err
	}
	var ids []pgtype.UUID
	for rows.Next() {
		var id pgtype.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for n, id := range ids {
		if n < 3 {
			_, _ = h.syncPRPolicyBatch(ctx, id, 1)
		}
		if err = h.ReconcilePRPolicy(ctx, id); err != nil {
			_, _ = h.DB.Exec(ctx, `UPDATE pr_automation_policy SET checked_at=now(),last_error=$2 WHERE workspace_id=$1`, id, "synchronization failed")
			return err
		}
	}
	return nil
}

// Identity depends only on workspace membership of the installation and issue
// existence, never on another workspace's automation preferences.
func (h *Handler) resolvePRAutomationIdentity(ctx context.Context, d dbExecutor, insts []db.GithubInstallation, matches map[string]string) closeIntentPolicy {
	owners := map[string]string{}
	for key := range matches {
		count := 0
		for _, inst := range insts {
			w, err := db.New(d).GetWorkspace(ctx, inst.WorkspaceID)
			if err != nil {
				return closeIntentPolicy{}
			}
			n, ok := issueNumberForPrefix(key, issuePrefixForWorkspace(w))
			if !ok {
				continue
			}
			_, err = db.New(d).GetIssueByNumber(ctx, db.GetIssueByNumberParams{WorkspaceID: inst.WorkspaceID, Number: n})
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return closeIntentPolicy{}
			}
			count++
			owners[key] = uuidToString(inst.WorkspaceID)
		}
		if count != 1 {
			delete(owners, key)
		}
	}
	return closeIntentPolicy{owner: owners}
}

// Metadata and body evidence commit together under the workspace lock.
// Preview apply holds that same lock; reconciliation can recover the durable
// observation after an interrupted request.
func (h *Handler) finishPRPolicyMirror(ctx context.Context, tx pgx.Tx, ws, pr pgtype.UUID, body string, at pgtype.Timestamptz) error {
	err := storePRPolicyEvidence(ctx, tx, ws, pr, body, at)
	if err != nil {
		return err
	}
	// Persist ingestion first. Reconciliation is durably retried by the scheduler
	// after a crash or evaluation failure; new writers take the same lock.
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if err = h.ReconcilePRPolicy(ctx, ws); err != nil {
		return err
	}
	h.publish(protocol.EventPullRequestUpdated, uuidToString(ws), "system", "", map[string]any{"linked_issue_ids": []string{}})
	return nil
}

// Equal timestamps do not order conflicting payloads. Preserve the observation,
// suspend completion, and ask the provider API to resolve the conflict.
func (h *Handler) conflictingPRObservation(ctx context.Context, tx pgx.Tx, ws pgtype.UUID, table, keyColumn string, key any, owner, repo string, number int32, title, branch, body, state, action string, at pgtype.Timestamptz) (bool, error) {
	if action == "synchronized" {
		return false, nil
	}
	var id pgtype.UUID
	var oldTitle, oldBranch, oldState string
	var oldBody pgtype.Text
	var oldAt pgtype.Timestamptz
	err := tx.QueryRow(ctx, `SELECT p.id,p.title,COALESCE(p.branch,''),p.state,p.pr_updated_at,e.body FROM `+table+` p
 LEFT JOIN pr_automation_evidence e ON e.pr_id=p.id WHERE p.workspace_id=$1 AND p.`+keyColumn+`=$2 AND p.repo_owner=$3 AND p.repo_name=$4 AND p.pr_number=$5`, ws, key, owner, repo, number).Scan(&id, &oldTitle, &oldBranch, &oldState, &oldAt, &oldBody)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !at.Valid || !oldAt.Valid || !at.Time.Equal(oldAt.Time) || (title == oldTitle && branch == oldBranch && state == oldState && (!oldBody.Valid || oldBody.String == body)) {
		return false, nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO pr_automation_evidence(pr_id,workspace_id,body,observed_at,sync_error) VALUES($1,$2,$3,$4,'conflicting observation')
 ON CONFLICT(pr_id) DO UPDATE SET sync_error='conflicting observation',sync_attempted_at=NULL`, id, ws, oldBody.String, oldAt)
	if err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, h.ReconcilePRPolicy(ctx, ws)
}

// The legacy path shares the migration lock and connection too. Buffer external
// effects so the old helpers still publish only committed state.
type prDeferredEffects struct {
	handler *Handler
	effects []func()
}

func (h *Handler) deferPREffect(effect func(*Handler)) bool {
	if h.prEffects == nil {
		return false
	}
	root := h.prEffects.handler
	h.prEffects.effects = append(h.prEffects.effects, func() { effect(root) })
	return true
}
