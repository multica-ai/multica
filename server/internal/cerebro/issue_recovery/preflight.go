package issuerecovery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	defaultCandidateLimit = 50
	latestCommentLimit    = 10
)

type Category string

const (
	CategoryCloseCandidate           Category = "kan_lukkes_nu"
	CategoryUnverifiedAgentDelivery  Category = "agent_leverance_mangler"
	CategoryRuntimeOrUsageFailure    Category = "runtime_usage_fejl"
	CategoryPRReviewDeployQueue      Category = "pr_review_deploy_koe"
	CategoryHumanDecision            Category = "venter_paa_menneskevalg"
	CategoryBlocker                  Category = "reel_blocker"
	CategoryPlanWithoutExecutionLink Category = "plan_uden_execution_link"
	CategoryUnclear                  Category = "uklart"
)

type CandidateInput struct {
	IssueTitle  string
	IssueStatus string
	Description string
	Comments    []db.Comment
	Tasks       []db.AgentTaskQueue
}

type Classification struct {
	Category Category
	Evidence string
}

type candidate struct {
	ID             pgtype.UUID
	Identifier     string
	Title          string
	Status         string
	Priority       string
	UpdatedAt      time.Time
	AssigneeID     pgtype.UUID
	Classification Classification
}

// AppendPreflight adds deterministic platform data for the Firtal issue-recovery
// autopilot. Normal autopilots are returned unchanged.
func AppendPreflight(ctx context.Context, q *db.Queries, ap db.Autopilot, base string, now time.Time) string {
	if !IsIssueRecoveryAutopilot(ap) {
		return base
	}

	preflight, err := BuildPreflight(ctx, q, ap.WorkspaceID, now, defaultCandidateLimit)
	if err != nil {
		return base + "\n\n## Platform recovery preflight\n\nPreflight generation failed: `" + sanitizeInline(err.Error()) + "`\n"
	}
	return base + "\n\n" + preflight
}

func IsIssueRecoveryAutopilot(ap db.Autopilot) bool {
	haystack := strings.ToLower(ap.Title + " " + ap.Description.String + " " + ap.IssueTitleTemplate.String)
	return strings.Contains(haystack, "issue-recovery") || strings.Contains(haystack, "stalled til handling")
}

func BuildPreflight(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, now time.Time, limit int) (string, error) {
	if limit <= 0 {
		limit = defaultCandidateLimit
	}

	openIssues, err := q.ListOpenIssues(ctx, db.ListOpenIssuesParams{WorkspaceID: workspaceID})
	if err != nil {
		return "", fmt.Errorf("list open issues: %w", err)
	}

	prefix := ""
	if ws, err := q.GetWorkspace(ctx, workspaceID); err == nil {
		prefix = ws.IssuePrefix
	}

	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		loc = time.UTC
	}
	today := startOfDay(now.In(loc), loc)

	var stale []db.ListOpenIssuesRow
	for _, issue := range openIssues {
		if !isActiveRecoveryStatus(issue.Status) {
			continue
		}
		if !issue.UpdatedAt.Valid || !issue.UpdatedAt.Time.In(loc).Before(today) {
			continue
		}
		stale = append(stale, issue)
	}
	sort.SliceStable(stale, func(i, j int) bool {
		return stale[i].UpdatedAt.Time.Before(stale[j].UpdatedAt.Time)
	})

	shownRows := stale
	if len(shownRows) > limit {
		shownRows = shownRows[:limit]
	}

	candidates := make([]candidate, 0, len(shownRows))
	counts := map[Category]int{}
	for _, issue := range shownRows {
		comments, _ := q.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: workspaceID,
			Limit:       latestCommentLimit,
		})
		tasks, _ := q.ListTasksByIssue(ctx, issue.ID)
		classification := Classify(CandidateInput{
			IssueTitle:  issue.Title,
			IssueStatus: issue.Status,
			Description: textString(issue.Description),
			Comments:    comments,
			Tasks:       tasks,
		})
		counts[classification.Category]++
		candidates = append(candidates, candidate{
			ID:             issue.ID,
			Identifier:     issueIdentifier(prefix, issue.Number),
			Title:          issue.Title,
			Status:         issue.Status,
			Priority:       issue.Priority,
			UpdatedAt:      issue.UpdatedAt.Time.In(loc),
			AssigneeID:     issue.AssigneeID,
			Classification: classification,
		})
	}

	return renderPreflight(len(openIssues), len(stale), len(candidates), limit, counts, candidates), nil
}

func Classify(input CandidateInput) Classification {
	text := strings.ToLower(input.IssueTitle + "\n" + input.Description + "\n" + commentsText(input.Comments) + "\n" + tasksText(input.Tasks))

	if hasAny(text, "monthly usage limit", "usage limit", "runtime went offline", "runtime_offline", "runtime_recovery", "empty output", "claude returned empty output", "task timed out", "timeout") {
		return Classification{Category: CategoryRuntimeOrUsageFailure, Evidence: matchingEvidence(text, []string{"usage limit", "runtime went offline", "runtime_recovery", "empty output", "timeout"})}
	}
	if hasAny(text, "repo ikke", "repo not", "not in workspace", "permission", "credential", "missing tool", "manglende tool", "not installed", "full disk access", "tcc") {
		return Classification{Category: CategoryBlocker, Evidence: matchingEvidence(text, []string{"repo", "permission", "credential", "missing tool", "not installed", "full disk access"})}
	}
	if hasAny(text, "confirmed", "bekræftet", "smoketest passed", "smoketest ok", "kan lukkes", "duplicate of", "duplikat") {
		return Classification{Category: CategoryCloseCandidate, Evidence: matchingEvidence(text, []string{"confirmed", "bekræftet", "smoketest", "kan lukkes", "duplicate", "duplikat"})}
	}
	if hasAny(text, "venter på dig", "afventer dig", "venter paa dig", "menneskevalg", "beslutning", "accept", "skal jeg", "vil du", "mention://member/") {
		return Classification{Category: CategoryHumanDecision, Evidence: matchingEvidence(text, []string{"venter", "afventer", "beslutning", "accept", "skal jeg", "vil du", "mention://member/"})}
	}

	hasPRSignal := hasAny(text, "pull request", "github.com/", "/pull/", "pr #", "merge", "merged", "deploy", "/ultrareview", "review")
	hasVerificationSignal := hasAny(text, "git push", "pushed", "pull request", "/pull/", "pr #", "merged", "deploy", "smoketest")
	missingDeliverySignal := hasAny(text, "no pr", "ingen pr", "mangler pr", "uden pr", "not pushed", "ikke pushet", "mangler push", "ikke merget")
	hasDoneLanguage := hasAny(text, "leveret", "færdig", "faerdig", "done", "fixed", "implemented", "complete")

	if missingDeliverySignal {
		return Classification{Category: CategoryUnverifiedAgentDelivery, Evidence: matchingEvidence(text, []string{"mangler pr", "ingen pr", "not pushed", "ikke pushet", "mangler push", "ikke merget"})}
	}
	if input.IssueStatus == "in_review" && !hasVerificationSignal {
		return Classification{Category: CategoryUnverifiedAgentDelivery, Evidence: "in_review without verified push/PR/merge/deploy signal"}
	}
	if hasDoneLanguage && !hasVerificationSignal {
		return Classification{Category: CategoryUnverifiedAgentDelivery, Evidence: matchingEvidence(text, []string{"leveret", "færdig", "done", "fixed", "implemented"})}
	}
	if hasPRSignal {
		return Classification{Category: CategoryPRReviewDeployQueue, Evidence: matchingEvidence(text, []string{"pull request", "pr #", "merged", "merge", "deploy", "review"})}
	}
	if hasAny(text, "plan", "research", "analyse", "analysis", "spike") && !hasAny(text, "sub-issue", "subissue", "artifact", "pr #", "pull request") {
		return Classification{Category: CategoryPlanWithoutExecutionLink, Evidence: matchingEvidence(text, []string{"plan", "research", "analyse", "analysis", "spike"})}
	}

	return Classification{Category: CategoryUnclear, Evidence: "no deterministic recovery signal in latest comments/runs"}
}

func renderPreflight(openCount, staleCount, shownCount, limit int, counts map[Category]int, candidates []candidate) string {
	var b strings.Builder
	b.WriteString("## Platform recovery preflight\n\n")
	b.WriteString("Generated by Multica before the agent run. Treat this as the recovery worklist, then verify before taking irreversible action.\n\n")
	fmt.Fprintf(&b, "- Open issues scanned: `%d`\n", openCount)
	fmt.Fprintf(&b, "- Stale active candidates: `%d`\n", staleCount)
	fmt.Fprintf(&b, "- Included in this run: `%d`", shownCount)
	if staleCount > limit {
		fmt.Fprintf(&b, " (oldest `%d`, capped from `%d`)", limit, staleCount)
	}
	b.WriteString("\n\n")

	b.WriteString("Category counts:\n")
	for _, category := range []Category{
		CategoryCloseCandidate,
		CategoryUnverifiedAgentDelivery,
		CategoryRuntimeOrUsageFailure,
		CategoryPRReviewDeployQueue,
		CategoryHumanDecision,
		CategoryBlocker,
		CategoryPlanWithoutExecutionLink,
		CategoryUnclear,
	} {
		if counts[category] > 0 {
			fmt.Fprintf(&b, "- `%s`: `%d`\n", category, counts[category])
		}
	}
	if len(counts) == 0 {
		b.WriteString("- none\n")
	}

	b.WriteString("\n### Candidate worklist\n\n")
	if len(candidates) == 0 {
		b.WriteString("No stale active issues matched the recovery criteria.\n")
		return b.String()
	}

	b.WriteString("| Issue | Status | Updated | Category | Evidence |\n")
	b.WriteString("|---|---:|---:|---|---|\n")
	for _, c := range candidates {
		issueLink := fmt.Sprintf("[%s](mention://issue/%s)", c.Identifier, util.UUIDToString(c.ID))
		fmt.Fprintf(
			&b,
			"| %s - %s | `%s` | `%s` | `%s` | %s |\n",
			escapeTable(issueLink),
			escapeTable(truncate(c.Title, 70)),
			escapeTable(c.Status),
			c.UpdatedAt.Format("2006-01-02 15:04"),
			c.Classification.Category,
			escapeTable(truncate(c.Classification.Evidence, 90)),
		)
	}
	return b.String()
}

func isActiveRecoveryStatus(status string) bool {
	return status == "in_progress" || status == "in_review" || status == "blocked"
}

func startOfDay(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func commentsText(comments []db.Comment) string {
	var b strings.Builder
	for _, c := range comments {
		b.WriteString(c.AuthorType)
		b.WriteByte(':')
		b.WriteString(c.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

func tasksText(tasks []db.AgentTaskQueue) string {
	var b strings.Builder
	for _, t := range tasks {
		b.WriteString(t.Status)
		b.WriteByte(' ')
		if t.FailureReason.Valid {
			b.WriteString(t.FailureReason.String)
			b.WriteByte(' ')
		}
		if t.Error.Valid {
			b.WriteString(t.Error.String)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func hasAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func matchingEvidence(text string, needles []string) string {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return "matched `" + sanitizeInline(needle) + "` signal"
		}
	}
	return "matched recovery rule"
}

func issueIdentifier(prefix string, number int32) string {
	if prefix == "" {
		return fmt.Sprintf("#%d", number)
	}
	return fmt.Sprintf("%s-%d", prefix, number)
}

func textString(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

func escapeTable(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return s
}

func sanitizeInline(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "`", "'")
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	rs := []rune(s)
	return string(rs[:maxRunes]) + "..."
}
