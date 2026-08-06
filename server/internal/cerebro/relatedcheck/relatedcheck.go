// Package relatedcheck answers one question for an agent that has just been
// handed an issue: is someone already working on this exact matter?
//
// It reuses the create-time duplicate check (FIR-2504): a keyword candidate
// search over open issues in the same workspace, then a Haiku judge that
// classifies every candidate as duplicate / related / unrelated. What is new
// here (FIR-4183) is that the answer is produced at ASSIGNMENT time and is
// written back into the issue graph — every actionable match becomes a
// symmetric `related` link (issue_dependency, FIR-823) — and the caller gets a
// short markdown report it can post as a comment.
//
// Everything in here is best-effort: no gateway, no candidates, or a judge
// timeout all resolve to "no match", never to an error that could block the
// agent from starting its work.
package relatedcheck

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/duplicatecheck"
)

// DefaultCandidateLimit is how many existing issues the keyword search hands
// to the judge. The create-time panel uses a pool of 10; we stay in the same
// range so the prompt (and its cost) does not grow.
const DefaultCandidateLimit = 10

// DefaultMatchLimit caps how many matches end up in the report. Three is the
// same cap the create-time panel shows a human — beyond that the signal drops.
const DefaultMatchLimit = 3

// Judger is the seam to the Haiku judge. *duplicatecheck.Judger satisfies it;
// tests inject a stub.
type Judger interface {
	Judge(ctx context.Context, draft duplicatecheck.Draft, candidates []duplicatecheck.Candidate) []duplicatecheck.JudgedCandidate
}

// Subject is the issue being checked.
type Subject struct {
	ID          string
	WorkspaceID string
	Identifier  string
	Title       string
	Description string
	ParentID    pgtype.UUID
}

// Candidate is one existing issue the keyword search surfaced.
type Candidate struct {
	ID          string
	Identifier  string
	Title       string
	Description string
	Status      string
	Score       int
}

// Match is one candidate the judge found actionable.
type Match struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	// Verdict is "duplicate" or "related" — unrelated candidates are dropped.
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
	// Linked reports whether THIS run created the related link. A repeat check
	// on the same issue reports the same match with Linked=false, which is how
	// callers stay quiet instead of re-commenting on every assignment.
	Linked bool `json:"linked"`
}

// Result is the full outcome of one check.
type Result struct {
	IssueID    string  `json:"issue_id"`
	Identifier string  `json:"identifier"`
	Candidates int     `json:"candidates"`
	Matches    []Match `json:"matches"`
	// Duplicate is the first duplicate-verdict match, if any. Callers use it to
	// decide whether the agent should stop and ask before starting work.
	Duplicate *Match `json:"duplicate,omitempty"`
	// NewLinks counts links created by this run.
	NewLinks int `json:"new_links"`
	// Skipped is set when the check could not run (title too short, judge not
	// configured). It is a fact for the caller, never an error.
	Skipped string `json:"skipped,omitempty"`
}

// HasNews reports whether this run found something the caller has not already
// been told about — the trigger for commenting.
func (r Result) HasNews() bool { return r.NewLinks > 0 }

// Options tunes one check.
type Options struct {
	CandidateLimit int
	MatchLimit     int
	// Link=false runs the check read-only (used by the CLI's --dry-run).
	Link bool
	// UserLabel tags the gateway call for cost tracking.
	UserLabel string
}

func (o Options) withDefaults() Options {
	if o.CandidateLimit <= 0 {
		o.CandidateLimit = DefaultCandidateLimit
	}
	if o.MatchLimit <= 0 {
		o.MatchLimit = DefaultMatchLimit
	}
	return o
}

// Check runs the whole pipeline for one issue: load it, find candidates, judge
// them, link the actionable ones, and return the report.
func Check(ctx context.Context, db cerebrodb.DBTX, judger Judger, issueID string, opts Options) (Result, error) {
	opts = opts.withDefaults()

	subject, err := LoadSubject(ctx, db, issueID)
	if err != nil {
		return Result{}, err
	}
	res := Result{IssueID: subject.ID, Identifier: subject.Identifier}

	terms := SearchTerms(subject.Title, subject.Description)
	if len(terms) == 0 {
		res.Skipped = "title too short to search on"
		return res, nil
	}

	candidates, err := FindCandidates(ctx, db, subject, terms, opts.CandidateLimit)
	if err != nil {
		return res, err
	}
	res.Candidates = len(candidates)
	if len(candidates) == 0 {
		return res, nil
	}
	if judger == nil {
		res.Skipped = "no judge configured"
		return res, nil
	}

	judged := judger.Judge(ctx, duplicatecheck.Draft{
		Title:       subject.Title,
		Description: subject.Description,
		UserLabel:   opts.UserLabel,
	}, toJudgeCandidates(candidates))
	if len(judged) == 0 {
		res.Skipped = "judge returned no verdict"
		return res, nil
	}

	res.Matches = selectMatches(candidates, judged, opts.MatchLimit)
	for i := range res.Matches {
		if opts.Link {
			created, linkErr := LinkRelated(ctx, db, subject.WorkspaceID, subject.ID, res.Matches[i].ID)
			if linkErr != nil {
				return res, linkErr
			}
			res.Matches[i].Linked = created
			if created {
				res.NewLinks++
			}
		}
		if res.Duplicate == nil && res.Matches[i].Verdict == string(duplicatecheck.VerdictDuplicate) {
			match := res.Matches[i]
			res.Duplicate = &match
		}
	}
	return res, nil
}

// LoadSubject reads the issue under check.
func LoadSubject(ctx context.Context, db cerebrodb.DBTX, issueID string) (Subject, error) {
	var (
		s      Subject
		id     pgtype.UUID
		wsID   pgtype.UUID
		number int32
		prefix string
	)
	err := db.QueryRow(ctx, `
SELECT i.id, i.workspace_id, i.number, i.title, COALESCE(i.description,''), i.parent_id,
       COALESCE(w.issue_prefix,'')
FROM issue i JOIN workspace w ON w.id = i.workspace_id
WHERE i.id = $1::uuid`, issueID).Scan(&id, &wsID, &number, &s.Title, &s.Description, &s.ParentID, &prefix)
	if err != nil {
		return Subject{}, err
	}
	s.ID = uuidString(id)
	s.WorkspaceID = uuidString(wsID)
	s.Identifier = FormatIdentifier(prefix, number)
	return s, nil
}

// FindCandidates scores open issues in the same workspace by how many of the
// search terms they contain, and returns the best `limit` of them.
//
// Terms come from SearchTerms, which emits only letters and digits, so they
// carry no LIKE metacharacters.
func FindCandidates(ctx context.Context, db cerebrodb.DBTX, subject Subject, terms []string, limit int) ([]Candidate, error) {
	rows, err := db.Query(ctx, `
SELECT i.id, i.number, i.title, LEFT(COALESCE(i.description,''), 400), i.status,
       COALESCE(w.issue_prefix,''), s.score
FROM issue i
JOIN workspace w ON w.id = i.workspace_id
CROSS JOIN LATERAL (
  SELECT count(*)::int AS score
  FROM unnest($2::text[]) AS t(term)
  WHERE i.title ILIKE '%' || t.term || '%'
     OR COALESCE(i.description,'') ILIKE '%' || t.term || '%'
) s
WHERE i.workspace_id = $1::uuid
  AND i.id <> $3::uuid
  AND i.status NOT IN ('done','cancelled')
  AND (i.parent_id IS NULL OR i.parent_id <> $3::uuid)
  AND ($4::uuid IS NULL OR i.id <> $4::uuid)
  AND s.score > 0
ORDER BY s.score DESC, i.updated_at DESC
LIMIT $5`, subject.WorkspaceID, terms, subject.ID, subject.ParentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Candidate{}
	for rows.Next() {
		var (
			id     pgtype.UUID
			number int32
			prefix string
			c      Candidate
		)
		if err := rows.Scan(&id, &number, &c.Title, &c.Description, &c.Status, &prefix, &c.Score); err != nil {
			return nil, err
		}
		c.ID = uuidString(id)
		c.Identifier = FormatIdentifier(prefix, number)
		out = append(out, c)
	}
	return out, rows.Err()
}

// LinkRelated records the symmetric related link and reports whether the link
// is new. Storage follows the FIR-823 convention: one LEAST/GREATEST-ordered
// row, so (A,B) and (B,A) collapse.
func LinkRelated(ctx context.Context, db cerebrodb.DBTX, workspaceID, issueA, issueB string) (bool, error) {
	tag, err := db.Exec(ctx, `
INSERT INTO issue_dependency (issue_id, depends_on_issue_id, type)
SELECT LEAST($1::uuid,$2::uuid), GREATEST($1::uuid,$2::uuid), 'related'
WHERE EXISTS (SELECT 1 FROM issue WHERE id = $1::uuid AND workspace_id = $3::uuid)
  AND EXISTS (SELECT 1 FROM issue WHERE id = $2::uuid AND workspace_id = $3::uuid)
  AND NOT EXISTS (
    SELECT 1 FROM issue_dependency d
    WHERE d.type = 'related'
      AND d.issue_id = LEAST($1::uuid,$2::uuid)
      AND d.depends_on_issue_id = GREATEST($1::uuid,$2::uuid))`, issueA, issueB, workspaceID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// selectMatches folds the judge verdicts back onto the candidates, drops the
// unrelated ones, puts duplicates first, and caps the list.
func selectMatches(candidates []Candidate, judged []duplicatecheck.JudgedCandidate, limit int) []Match {
	byID := make(map[string]Candidate, len(candidates))
	for _, c := range candidates {
		byID[c.ID] = c
	}
	order := make(map[string]int, len(candidates))
	for i, c := range candidates {
		order[c.ID] = i
	}

	matches := []Match{}
	seen := map[string]bool{}
	for _, j := range judged {
		if !j.Verdict.IsActionable() || seen[j.ID] {
			continue
		}
		c, ok := byID[j.ID]
		if !ok {
			continue
		}
		seen[j.ID] = true
		matches = append(matches, Match{
			ID:         c.ID,
			Identifier: c.Identifier,
			Title:      c.Title,
			Status:     c.Status,
			Verdict:    string(j.Verdict),
			Reason:     strings.TrimSpace(j.Reason),
		})
	}

	sort.SliceStable(matches, func(a, b int) bool {
		dupA := matches[a].Verdict == string(duplicatecheck.VerdictDuplicate)
		dupB := matches[b].Verdict == string(duplicatecheck.VerdictDuplicate)
		if dupA != dupB {
			return dupA
		}
		return order[matches[a].ID] < order[matches[b].ID]
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

func toJudgeCandidates(candidates []Candidate) []duplicatecheck.Candidate {
	out := make([]duplicatecheck.Candidate, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, duplicatecheck.Candidate{
			ID:          c.ID,
			Identifier:  c.Identifier,
			Title:       c.Title,
			Description: c.Description,
		})
	}
	return out
}

// FormatIdentifier renders the human issue key. Workspaces without a prefix
// fall back to the bare number rather than inventing one.
func FormatIdentifier(prefix string, number int32) string {
	if strings.TrimSpace(prefix) == "" {
		return fmt.Sprintf("#%d", number)
	}
	return fmt.Sprintf("%s-%d", prefix, number)
}

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	b := id.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
