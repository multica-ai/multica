package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/ghsnapshot"
	"github.com/multica-ai/multica/server/internal/integrations/vcs"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Parse only the public PR route on a configured instance. The caller never
// fetches the supplied URL; requests use the stored provider API origin.
func prPolicyURL(raw, origin, provider string) (string, string, int32, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	base, e := url.Parse(origin)
	if err != nil || e != nil || u.User != nil || u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) || u.RawQuery != "" || u.Fragment != "" {
		return "", "", 0, errors.New("use a PR URL from a connected provider")
	}
	prefix := strings.TrimRight(base.Path, "/") + "/"
	if !strings.HasPrefix(u.Path, prefix) {
		return "", "", 0, errors.New("invalid provider path")
	}
	path := strings.TrimPrefix(u.Path, prefix)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	cut := len(parts) - 2
	route := "pulls"
	if provider == "github" {
		route = "pull"
	}
	if provider == "gitlab" {
		route = "merge_requests"
	}
	if cut < 2 || parts[cut] != route {
		return "", "", 0, errors.New("invalid PR URL")
	}
	end := cut
	if provider == "gitlab" {
		if parts[cut-1] != "-" {
			return "", "", 0, errors.New("invalid merge request URL")
		}
		end--
	}
	if end < 2 || (provider != "gitlab" && end != 2) {
		return "", "", 0, errors.New("invalid repository path")
	}
	for _, s := range parts {
		if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "\\%") {
			return "", "", 0, errors.New("invalid repository path")
		}
	}
	n, err := strconv.ParseInt(parts[len(parts)-1], 10, 32)
	if err != nil || n <= 0 {
		return "", "", 0, errors.New("invalid PR number")
	}
	return strings.Join(parts[:end-1], "/"), parts[end-1], int32(n), nil
}

func fetchVCSPolicyPR(ctx context.Context, conn db.VcsConnection, token, owner, repo string, n int32) (vcs.PullRequestEvent, error) {
	base := strings.TrimRight(conn.InstanceUrl, "/")
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/pulls/%d", base, url.PathEscape(owner), url.PathEscape(repo), n)
	if conn.Provider == "gitlab" {
		endpoint = fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d", base, url.PathEscape(owner+"/"+repo), n)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return vcs.PullRequestEvent{}, err
	}
	req.Header.Set("Accept", "application/json")
	if conn.Provider == "gitlab" {
		req.Header.Set("PRIVATE-TOKEN", token)
	} else {
		req.Header.Set("Authorization", "token "+token)
	}
	client := http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return vcs.PullRequestEvent{}, errors.New("provider PR request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return vcs.PullRequestEvent{}, fmt.Errorf("provider PR status %d", resp.StatusCode)
	}
	var raw map[string]any
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw); err != nil {
		return vcs.PullRequestEvent{}, errors.New("invalid provider PR response")
	}
	envelope := map[string]any{"action": "synchronized", "pull_request": raw, "repository": map[string]any{"name": repo, "owner": map[string]string{"login": owner}}}
	if conn.Provider == "gitlab" {
		raw["url"] = raw["web_url"]
		raw["last_commit"] = map[string]any{"id": raw["sha"]}
		raw["action"] = "synchronized"
		envelope = map[string]any{"object_kind": "merge_request", "object_attributes": raw, "project": map[string]string{"path_with_namespace": owner + "/" + repo}, "user": raw["author"]}
	}
	body, _ := json.Marshal(envelope)
	provider, ok := vcs.For(conn.Provider)
	if !ok {
		return vcs.PullRequestEvent{}, errors.New("unsupported provider")
	}
	ev, err := provider.ParsePullRequest(body)
	if err != nil || ev.Number != n || ev.HTMLURL == "" || ev.UpdatedAt == "" || ev.CreatedAt == "" {
		return ev, errors.New("incomplete provider PR response")
	}
	return ev, nil
}

// A provider read has no database or issue side effects. Its caller chooses
// the transaction that stores the observation and whether to apply a policy.
type prPolicySnapshot struct {
	github     *ghPullRequestPayload
	connection db.VcsConnection
	vcs        vcs.PullRequestEvent
}

func storePRPolicyEvidence(ctx context.Context, tx pgx.Tx, ws, pr pgtype.UUID, body string, at pgtype.Timestamptz) error {
	_, err := tx.Exec(ctx, `INSERT INTO pr_automation_evidence(pr_id,workspace_id,body,observed_at,sync_attempted_at) VALUES($1,$2,$3,$4,now())
 ON CONFLICT(pr_id) DO UPDATE SET body=EXCLUDED.body,observed_at=EXCLUDED.observed_at,sync_error=NULL WHERE EXCLUDED.observed_at>=pr_automation_evidence.observed_at`, pr, ws, body, at)
	return err
}

func (s *prPolicySnapshot) store(ctx context.Context, tx pgx.Tx, ws pgtype.UUID) error {
	queries := db.New(tx)
	if s.github != nil {
		pr, err := upsertGitHubPolicyPR(ctx, queries, ws, s.github.Installation.ID, s.github)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // A newer webhook won the race with the provider read.
		}
		if err != nil {
			return err
		}
		return storePRPolicyEvidence(ctx, tx, ws, pr.ID, s.github.PullRequest.Body, pr.PrUpdatedAt)
	}
	if s.connection.WorkspaceID != ws {
		return errors.New("PR connection belongs to a different workspace")
	}
	pr, err := upsertVCSPolicyPR(ctx, queries, s.connection, s.vcs)
	if err != nil {
		return err
	}
	if at := parseGHTimeRequired(s.vcs.UpdatedAt); pr.PrUpdatedAt.Valid && at.Valid && pr.PrUpdatedAt.Time.After(at.Time) {
		return nil
	}
	return storePRPolicyEvidence(ctx, tx, ws, pr.ID, s.vcs.Body, pr.PrUpdatedAt)
}

// Synchronization only refreshes metadata, including for legacy workspaces.
// Webhooks, policy apply and the scheduler own linking/completion decisions.
func (h *Handler) syncPRPolicyURL(ctx context.Context, ws pgtype.UUID, raw string) error {
	snapshot, err := h.fetchPRPolicyURL(ctx, ws, raw)
	if err != nil {
		return err
	}
	tx, err := h.prPolicyTx(ctx, ws)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = snapshot.store(ctx, tx, ws); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	h.publish(protocol.EventPullRequestUpdated, uuidToString(ws), "system", "", map[string]any{"linked_issue_ids": []string{}})
	return nil
}

// Resolve explicitly supplied URLs against workspace credentials. A shared
// GitHub installation cannot create a link in a different workspace here.
func (h *Handler) fetchPRPolicyURL(ctx context.Context, ws pgtype.UUID, raw string) (*prPolicySnapshot, error) {
	if owner, repo, n, err := prPolicyURL(raw, "https://github.com", "github"); err == nil {
		w, e := h.Queries.GetWorkspace(ctx, ws)
		if e != nil {
			return nil, e
		}
		var flags struct {
			Enabled *bool `json:"github_enabled"`
		}
		if json.Unmarshal(w.Settings, &flags) != nil || (flags.Enabled != nil && !*flags.Enabled) {
			return nil, errors.New("GitHub integration is disabled")
		}
		insts, e := h.Queries.ListGitHubInstallationsByWorkspace(ctx, ws)
		if e != nil {
			return nil, e
		}
		client, e := ghsnapshot.NewClientFromEnv()
		if e != nil {
			return nil, e
		}
		for _, inst := range insts {
			body, e := client.FetchPullRequest(ctx, inst.InstallationID, owner, repo, n)
			if e != nil {
				continue
			}
			p := ghPullRequestPayload{}
			p.Action = "synchronized"
			p.Installation.ID = inst.InstallationID
			p.Repository.Name = repo
			p.Repository.Owner.Login = owner
			if json.Unmarshal(body, &p.PullRequest) != nil || p.PullRequest.Number != n || p.PullRequest.UpdatedAt == "" || p.PullRequest.CreatedAt == "" {
				return nil, errors.New("incomplete GitHub PR response")
			}
			return &prPolicySnapshot{github: &p}, nil
		}
		return nil, errors.New("cannot read PR with this workspace's GitHub installation")
	}
	if !h.isVCSAvailable() || !h.isVCSConfigured() {
		return nil, errors.New("provider is not configured")
	}
	conns, err := h.Queries.ListVCSConnectionsByWorkspace(ctx, ws)
	if err != nil {
		return nil, err
	}
	for _, conn := range conns {
		owner, repo, n, e := prPolicyURL(raw, conn.InstanceUrl, conn.Provider)
		if e != nil {
			continue
		}
		token, e := h.openVCSSecret(conn.AccessTokenEncrypted)
		if e != nil {
			return nil, errors.New("provider credential unavailable")
		}
		ev, e := fetchVCSPolicyPR(ctx, conn, token, owner, repo, n)
		if e != nil {
			return nil, e
		}
		return &prPolicySnapshot{connection: conn, vcs: ev}, nil
	}
	return nil, errors.New("use a PR URL from a connected provider")
}

// Bound API work and persist attempted_at even on failure, so an unavailable
// repository cannot starve later entries. The next scheduled pass retries it.
func (h *Handler) syncPRPolicyBatch(ctx context.Context, ws pgtype.UUID, limit int) (int, error) {
	rows, err := h.DB.Query(ctx, `SELECT p.id,p.html_url FROM (
 SELECT id,workspace_id,html_url,pr_updated_at,state FROM github_pull_request
 UNION ALL SELECT id,workspace_id,html_url,pr_updated_at,state FROM vcs_pull_request) p
 LEFT JOIN pr_automation_evidence e ON e.pr_id=p.id
 WHERE p.workspace_id=$1 AND (e.observed_at IS NULL OR e.observed_at<p.pr_updated_at OR e.sync_error IS NOT NULL OR p.state IN ('open','draft'))
 AND (e.sync_attempted_at IS NULL OR e.sync_attempted_at<now()-interval '10 minutes')
 ORDER BY e.sync_attempted_at NULLS FIRST,p.id LIMIT $2`, ws, limit)
	if err != nil {
		return 0, err
	}
	type entry struct {
		id  pgtype.UUID
		url string
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.id, &e.url); err != nil {
			rows.Close()
			return 0, err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	synced := 0
	for _, e := range entries {
		_, err = h.DB.Exec(ctx, `INSERT INTO pr_automation_evidence(pr_id,workspace_id,body,observed_at,sync_attempted_at) VALUES($1,$2,'','epoch',now())
 ON CONFLICT(pr_id) DO UPDATE SET sync_attempted_at=now()`, e.id, ws)
		if err != nil {
			return synced, err
		}
		fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		syncErr := h.syncPRPolicyURL(fetchCtx, ws, e.url)
		cancel()
		var reason *string
		if syncErr != nil {
			v := "provider synchronization failed"
			reason = &v
		} else {
			synced++
		}
		_, err = h.DB.Exec(ctx, `UPDATE pr_automation_evidence SET sync_error=$2 WHERE pr_id=$1`, e.id, reason)
		if err != nil {
			return synced, err
		}
	}
	return synced, nil
}

func (h *Handler) SyncPRPolicy(w http.ResponseWriter, r *http.Request) {
	member, ok := h.requireWorkspaceRole(w, r, chi.URLParam(r, "id"), "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	// A legacy workspace can fetch metadata without activating the new policy.
	count, err := h.syncPRPolicyBatch(r.Context(), member.WorkspaceID, 3)
	if err != nil {
		writeError(w, 500, "cannot synchronize PR metadata")
		return
	}
	writeJSON(w, 200, map[string]int{"synchronized": count})
}
