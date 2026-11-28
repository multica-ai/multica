package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// A chat or issue wakeup can name one GitHub pull request by repository and
// number. It is looked up among the workspace's mirrored pull requests,
// whether or not any issue links it.

// These explain a wait on a named pull request that may still resolve.
var (
	errPRTargetMissing         = errors.New("The pull request is no longer mirrored in this workspace; check the GitHub integration")
	errPRTargetSnapshotPending = errors.New("Waiting for a check snapshot of the pull request's current head")
)

// prTargetInputError is a named pull request the platform could never watch.
type prTargetInputError struct{ msg string }

func (e prTargetInputError) Error() string { return e.msg }

// normalizePRTarget checks that the workspace mirrors repo#number and returns
// the repository as GitHub spells it.
func normalizePRTarget(ctx context.Context, tx pgx.Tx, workspaceID pgtype.UUID, repo string, number int32) (string, error) {
	owner, name, ok := strings.Cut(strings.TrimSpace(repo), "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", prTargetInputError{"repo must be owner/name"}
	}
	if number <= 0 {
		return "", prTargetInputError{"number must be a pull request number"}
	}
	err := tx.QueryRow(ctx, `SELECT repo_owner, repo_name FROM github_pull_request
		WHERE workspace_id=$1 AND lower(repo_owner)=lower($2) AND lower(repo_name)=lower($3) AND pr_number=$4
		ORDER BY updated_at DESC LIMIT 1`, workspaceID, owner, name, number).Scan(&owner, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", prTargetInputError{fmt.Sprintf("this workspace has no pull request %s/%s#%d; it must be on a repository connected through the GitHub integration", owner, name, number)}
	}
	if err != nil {
		return "", err
	}
	return owner + "/" + name, nil
}

// evaluatePRTarget reports whether the named pull request reached event
// (merged, closed or checks_finished), a fingerprint of the satisfying facts
// and what was observed. errPRTargetMissing and errPRTargetSnapshotPending
// explain a wait rather than a failure.
func evaluatePRTarget(ctx context.Context, tx pgx.Tx, workspaceID pgtype.UUID, repo string, number int32, event string) (bool, string, map[string]any, error) {
	owner, name, _ := strings.Cut(repo, "/")
	var state, head, snapshotHead, checks, url, title string
	err := tx.QueryRow(ctx, `SELECT state, head_sha, snapshot_head_sha, COALESCE(checks_rollup_state,''), html_url, title
		FROM github_pull_request
		WHERE workspace_id=$1 AND lower(repo_owner)=lower($2) AND lower(repo_name)=lower($3) AND pr_number=$4
		ORDER BY updated_at DESC LIMIT 1`, workspaceID, owner, name, number).Scan(&state, &head, &snapshotHead, &checks, &url, &title)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", nil, errPRTargetMissing
	}
	if err != nil {
		return false, "", nil, err
	}
	observed := map[string]any{"pull_request": fmt.Sprintf("%s#%d", repo, number), "title": title, "url": url, "state": state}
	switch event {
	case "merged":
		return state == "merged", "pr:" + state, observed, nil
	case "closed":
		return state == "merged" || state == "closed", "pr:" + state, observed, nil
	case "checks_finished":
		if snapshotHead == "" || snapshotHead != head {
			return false, "", observed, errPRTargetSnapshotPending
		}
		observed["head_sha"], observed["checks"] = head, strings.ToLower(checks)
		finished := slices.Contains([]string{"SUCCESS", "FAILURE", "ERROR"}, checks)
		return finished, "checks:" + head + ":" + checks, observed, nil
	}
	return false, "", nil, fmt.Errorf("unknown pull request event %q", event)
}
