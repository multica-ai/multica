// Package issueidentifier is the single authority for turning an issue into its
// human-readable "PREFIX-NUMBER" identifier and for resolving the bare prefix
// (Team key) of an issue or workspace.
//
// It replaces four drifted copies of the same fallback chain that previously
// lived in the handler, task service, autopilot service, and channel router.
// The chain is, in order:
//
//  1. the issue's own Team key (when the issue has a Team);
//  2. the workspace's default Team key;
//  3. the legacy workspace issue_prefix (compatibility window);
//  4. a prefix generated from the workspace name.
//
// Step 4 is the defensive fallback-of-last-resort: callers never emit "-42" or
// "#42" for an issue whose Team/workspace lookups all failed.
package issueidentifier

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Queries is the read surface the resolver needs. Both *db.Queries and the
// channel engine's SessionReader satisfy it.
type Queries interface {
	GetWorkspaceTeam(ctx context.Context, arg db.GetWorkspaceTeamParams) (db.WorkspaceTeam, error)
	GetDefaultWorkspaceTeam(ctx context.Context, workspaceID pgtype.UUID) (db.WorkspaceTeam, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
}

var nonAlpha = regexp.MustCompile(`[^a-zA-Z]`)

// GeneratePrefix produces a 2-3 char uppercase prefix from a workspace name.
// Examples: "Jiayuan's Workspace" -> "JIA", "My Team" -> "MYT", "AB" -> "AB".
func GeneratePrefix(name string) string {
	letters := nonAlpha.ReplaceAllString(name, "")
	if len(letters) == 0 {
		return "WS"
	}
	letters = strings.ToUpper(letters)
	if len(letters) > 3 {
		letters = letters[:3]
	}
	return letters
}

// PrefixForIssue returns the identifier prefix (Team key) for a single issue,
// following the full fallback chain.
func PrefixForIssue(ctx context.Context, q Queries, issue db.Issue) string {
	if issue.TeamID.Valid {
		team, err := q.GetWorkspaceTeam(ctx, db.GetWorkspaceTeamParams{
			ID:          issue.TeamID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err == nil && team.Key != "" {
			return team.Key
		}
	}
	return PrefixForWorkspace(ctx, q, issue.WorkspaceID)
}

// PrefixForWorkspace returns the workspace-level identifier prefix: the default
// Team key, then the legacy issue_prefix, then a generated prefix.
func PrefixForWorkspace(ctx context.Context, q Queries, workspaceID pgtype.UUID) string {
	team, err := q.GetDefaultWorkspaceTeam(ctx, workspaceID)
	if err == nil && team.Key != "" {
		return team.Key
	}
	ws, err := q.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return ""
	}
	if ws.IssuePrefix != "" {
		return ws.IssuePrefix
	}
	return GeneratePrefix(ws.Name)
}

// ForIssue returns the full "PREFIX-NUMBER" identifier for an issue.
func ForIssue(ctx context.Context, q Queries, issue db.Issue) string {
	return fmt.Sprintf("%s-%d", PrefixForIssue(ctx, q, issue), issue.Number)
}

// Resolver memoizes Team-key lookups so list/batch paths that resolve
// identifiers for many issues avoid a per-row GetWorkspaceTeam query. It is not
// safe for concurrent use; construct one per request/loop.
type Resolver struct {
	q        Queries
	teamKeys map[pgtype.UUID]string // resolved Team ID -> Team key ("" = lookup failed/empty)
	wsPrefix map[pgtype.UUID]string // workspace ID -> fallback prefix
}

// NewResolver returns a memoizing Resolver over the given queries.
func NewResolver(q Queries) *Resolver {
	return &Resolver{
		q:        q,
		teamKeys: make(map[pgtype.UUID]string),
		wsPrefix: make(map[pgtype.UUID]string),
	}
}

// PrefixForIssue mirrors the package-level function but caches Team and
// workspace lookups across calls on the same Resolver.
func (r *Resolver) PrefixForIssue(ctx context.Context, issue db.Issue) string {
	if issue.TeamID.Valid {
		if key := r.teamKey(ctx, issue.TeamID, issue.WorkspaceID); key != "" {
			return key
		}
	}
	return r.prefixForWorkspace(ctx, issue.WorkspaceID)
}

func (r *Resolver) teamKey(ctx context.Context, teamID, workspaceID pgtype.UUID) string {
	if key, ok := r.teamKeys[teamID]; ok {
		return key
	}
	key := ""
	team, err := r.q.GetWorkspaceTeam(ctx, db.GetWorkspaceTeamParams{
		ID:          teamID,
		WorkspaceID: workspaceID,
	})
	if err == nil {
		key = team.Key
	}
	r.teamKeys[teamID] = key
	return key
}

func (r *Resolver) prefixForWorkspace(ctx context.Context, workspaceID pgtype.UUID) string {
	if prefix, ok := r.wsPrefix[workspaceID]; ok {
		return prefix
	}
	prefix := PrefixForWorkspace(ctx, r.q, workspaceID)
	r.wsPrefix[workspaceID] = prefix
	return prefix
}
