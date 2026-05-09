// Phase 4 — Ship Hub linkage.
//
// This file is the bridge that stops PR rows from being standalone GitHub
// objects and connects them back into Multica's existing graph. Three
// operations live here:
//
//  1. DetectMultiicaReferences — given a PR (body / title / branch /
//     latest commit message), find the originating Multica issue and the
//     originating agent_task_queue row. Pure function over text + a few
//     DB lookups. Idempotent: running it twice on the same PR produces
//     the same result.
//
//  2. ClassifySource — pick the `source` enum value for a PR. The
//     classifier doesn't depend on any GitHub-side state beyond what
//     upsertPR already mirrors locally (originating_*_id and
//     author_login), so it runs in-process after each upsert.
//
//  3. ApplyLinkage — persist the result of (1) + (2) onto the
//     pull_request row via UpdatePullRequestLinkage. Wrapped here so the
//     webhook dispatcher can call a single function rather than
//     duplicating the COALESCE/narg dance.
//
// The helpers stay in the service package (not the handler) because the
// webhook ingest path runs them automatically — no HTTP plumbing
// required. The HTTP-level manual-override endpoint reuses
// UpdatePullRequestLinkage directly.

package ship

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
)

// Source values written to pull_request.source. Open string column so a
// future variant ("multica_external_agent" for cloud runtimes, etc.) can
// land without a CREATE TYPE migration.
const (
	SourceMultiicaAgent       = "multica_agent"
	SourceMultiicaHuman       = "multica_human"
	SourceExternalTool        = "external_tool"
	SourceExternalContributor = "external_contributor"
)

// urlIssueRefRe matches the canonical Multica issue URL:
//
//	https://multica.wisco.wine/{slug}/issues/{number}
//
// Captures the issue number. We allow http:// for non-prod environments
// and accept any host so a self-hosted deployment doesn't have to patch
// the regex.
var urlIssueRefRe = regexp.MustCompile(`(?i)https?://[^/\s]+/[^/\s]+/issues/(\d+)`)

// agentTaskRefRe matches the trailer the Multica agent runtime appends
// to commits it pushes:
//
//	agent_task_id=00000000-0000-0000-0000-000000000000
//
// Case-insensitive and accepts uppercase/dashed UUID. Captures the UUID.
var agentTaskRefRe = regexp.MustCompile(
	`(?i)agent_task_id\s*=\s*([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`,
)

// LinkageResult is the outcome of DetectMultiicaReferences. nil pgtype
// fields mean "no match"; the caller treats those as "leave the column
// unchanged" via narg.
type LinkageResult struct {
	IssueID     pgtype.UUID
	AgentTaskID pgtype.UUID
	// Source is the classification used for the source column. Always
	// non-empty when DetectMultiicaReferences returns without error.
	Source string
}

// DetectMultiicaReferences scans a PR's text fields (title, body,
// head_ref, plus an optional commit message) for references to a
// Multica issue or agent task. The result is suitable for passing
// directly into UpdatePullRequestLinkage.
//
// The function performs at most two DB lookups (one per match) so
// running it on every webhook event stays cheap. Lookups are scoped to
// `workspaceID` so a coincidental issue-prefix collision across
// workspaces never cross-links.
func (s *Service) DetectMultiicaReferences(
	ctx context.Context,
	workspaceID pgtype.UUID,
	issuePrefix string,
	pr gh.PullRequest,
	commitMessage string,
) (LinkageResult, error) {
	// Concatenate the PR's text surface area into a single buffer for
	// scanning. The regex engine then reads it once per pattern instead
	// of N times per source. Order doesn't matter because we extract by
	// pattern — but we keep the "title + body" head so a quick visual
	// inspection of the joined string in tests reads naturally.
	combined := strings.Join([]string{
		pr.Title,
		pr.Body,
		pr.Head.Ref,
		commitMessage,
	}, "\n")

	result := LinkageResult{}

	if issueID, ok := s.findIssueByCombined(ctx, workspaceID, issuePrefix, combined); ok {
		result.IssueID = issueID
	}
	if taskID, ok := s.findAgentTaskByCombined(ctx, combined); ok {
		result.AgentTaskID = taskID
	}
	result.Source = ClassifySource(pr.User.Login, result.IssueID, result.AgentTaskID, /*authorIsMember*/ false)

	return result, nil
}

// buildPrefixIssueRegexForTest is exported here purely so the linkage
// regression test can build the same pattern. Production code uses the
// inlined construction in findIssueByCombined.
func buildPrefixIssueRegexForTest(prefix string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(prefix) + `-(\d+)\b`)
}

// findIssueByCombined extracts the first issue identifier from the
// combined text and resolves it via GetIssueByNumber. Returns ok=false
// when no pattern matches OR the matched number doesn't resolve to a
// real issue in the workspace.
func (s *Service) findIssueByCombined(
	ctx context.Context,
	workspaceID pgtype.UUID,
	issuePrefix string,
	combined string,
) (pgtype.UUID, bool) {
	// The {prefix}-NNN pattern dominates because the agent runtime
	// includes it in branch names by default. Build the regex from the
	// runtime workspace prefix so a workspace whose prefix is "ROA"
	// doesn't accidentally match "MUL-123".
	if issuePrefix != "" {
		pattern := buildPrefixIssueRegexForTest(issuePrefix)
		if m := pattern.FindStringSubmatch(combined); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
				if issue, ok := s.lookupIssueByNumber(ctx, workspaceID, int32(n)); ok {
					return issue.ID, true
				}
			}
		}
	}
	// URL form is a strict fallback. The slug component isn't checked
	// against the workspace's own slug because the URL might point at a
	// different self-hosted deployment that mirrors the same issue
	// numbers — we still extract the number and validate it exists in
	// THIS workspace via GetIssueByNumber. Same number in a different
	// workspace is just "not in our table", returns ok=false.
	if m := urlIssueRefRe.FindStringSubmatch(combined); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			if issue, ok := s.lookupIssueByNumber(ctx, workspaceID, int32(n)); ok {
				return issue.ID, true
			}
		}
	}
	return pgtype.UUID{}, false
}

// lookupIssueByNumber wraps GetIssueByNumber with the no-rows -> ok=false
// translation. ErrNoRows is the expected outcome for "user wrote a
// number but it isn't an issue here" — we just don't link.
func (s *Service) lookupIssueByNumber(ctx context.Context, workspaceID pgtype.UUID, number int32) (db.Issue, bool) {
	issue, err := s.Q.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
		WorkspaceID: workspaceID,
		Number:      number,
	})
	if err != nil {
		if !isNoRows(err) {
			slog.Warn("ship: lookup issue by number failed",
				"workspace_id", uuidString(workspaceID), "number", number, "error", err)
		}
		return db.Issue{}, false
	}
	return issue, true
}

// findAgentTaskByCombined extracts the agent_task_id trailer (if any)
// and verifies the task exists. The task may have been deleted since
// the agent pushed the commit, in which case ok=false.
func (s *Service) findAgentTaskByCombined(ctx context.Context, combined string) (pgtype.UUID, bool) {
	m := agentTaskRefRe.FindStringSubmatch(combined)
	if m == nil {
		return pgtype.UUID{}, false
	}
	taskUUID, err := parseUUIDText(m[1])
	if err != nil {
		return pgtype.UUID{}, false
	}
	task, err := s.Q.GetAgentTask(ctx, taskUUID)
	if err != nil {
		if !isNoRows(err) {
			slog.Warn("ship: lookup agent task failed",
				"agent_task_id", m[1], "error", err)
		}
		return pgtype.UUID{}, false
	}
	return task.ID, true
}

// ClassifySource picks the source enum value. Pure: callers can run it
// without any DB access if they already know whether the author is a
// workspace member.
//
// Priority (highest first):
//
//  1. originating_agent_task_id set    → multica_agent
//  2. originating_issue_id set         → multica_human (a Multica user
//     created the issue, then a tool/IDE-driven PR was opened against
//     it; even if the GitHub author isn't a workspace member this is
//     still "intentional Multica work").
//  3. authorIsMember == true           → external_tool (workspace
//     member opened a PR without referencing an issue — likely a tool
//     like Cursor or a manual `gh pr create`).
//  4. otherwise                        → external_contributor.
func ClassifySource(authorLogin string, originatingIssue, originatingAgentTask pgtype.UUID, authorIsMember bool) string {
	if originatingAgentTask.Valid {
		return SourceMultiicaAgent
	}
	if originatingIssue.Valid {
		return SourceMultiicaHuman
	}
	if authorIsMember {
		return SourceExternalTool
	}
	return SourceExternalContributor
}

// ApplyLinkage persists the LinkageResult onto the pull_request row.
// Returns the updated row so the webhook dispatcher can read back the
// final source classification (the row may have other concurrent
// updates the in-process result didn't see).
func (s *Service) ApplyLinkage(ctx context.Context, prID pgtype.UUID, result LinkageResult) (db.PullRequest, error) {
	params := db.UpdatePullRequestLinkageParams{
		ID:                     prID,
		OriginatingIssueID:     result.IssueID,
		OriginatingAgentTaskID: result.AgentTaskID,
	}
	if result.Source != "" {
		params.Source = pgtype.Text{String: result.Source, Valid: true}
	}
	row, err := s.Q.UpdatePullRequestLinkage(ctx, params)
	if err != nil {
		return db.PullRequest{}, fmt.Errorf("apply linkage: %w", err)
	}
	return row, nil
}

// parseUUIDText parses a hyphenated UUID string into pgtype.UUID. We
// don't reuse handler/parseUUID here to keep the service package
// independent of internal/handler.
func parseUUIDText(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

// isNoRows centralises the pgx ErrNoRows check so callers stay readable.
func isNoRows(err error) bool {
	return err != nil && errors.Is(err, pgx.ErrNoRows)
}
