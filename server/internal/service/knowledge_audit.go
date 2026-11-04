package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The Librarian's routines (spec 3.3, plan 10.9): the jobs behind the
// Librarian's instructions. Each job reads the registry and WeKnora, records a
// LibrarianReport for the workspace, and changes nothing a person would not
// expect, except the one repair the monthly audit makes: deleting link rows
// whose document WeKnora no longer holds. The Librarian's template and
// routines are defined elsewhere; these are the jobs they call.

// Librarian job names. The scheduler registers one job per name.
const (
	LibrarianJobTriage           = "triage"
	LibrarianJobFreshness        = "freshness"
	LibrarianJobAudit            = "audit"
	LibrarianJobEval             = "eval"
	LibrarianJobTaxonomyBackfill = "taxonomy_backfill"
)

const (
	// librarianFindingLimit bounds the findings one report lists; the counts
	// carry the totals.
	librarianFindingLimit = 50
	// librarianReadLimit bounds the drafts whose text triage reads.
	librarianReadLimit = 50
	// librarianListPageSize and librarianListMaxPages bound a WeKnora listing
	// the audit walks: 100 documents a page, 50 pages.
	librarianListPageSize = 100
	// librarianDraftWindow is how far back a draft counts as new.
	librarianDraftWindow = 24 * time.Hour
)

// librarianListMaxPages caps the WeKnora listing the audit walks, 50 pages of
// librarianListPageSize. A variable, so a test can reach the cap.
var librarianListMaxPages = 50

// LibrarianFinding is one document a routine points at.
type LibrarianFinding struct {
	LinkID        string   `json:"link_id"`
	Title         string   `json:"title"`
	KnowledgeBase string   `json:"knowledge_base"`
	Kind          string   `json:"knowledge_kind"`
	SquadID       string   `json:"squad_id"`
	Reason        string   `json:"reason"`
	Missing       []string `json:"missing_sections,omitempty"`
	OtherLinkID   string   `json:"other_link_id,omitempty"`
}

// LibrarianReport is what a routine found, as stored and as the digest and
// the Librarian read it. Counts are totals; Findings is capped.
type LibrarianReport struct {
	Job       string             `json:"job"`
	RanAt     time.Time          `json:"ran_at"`
	Counts    map[string]int     `json:"counts"`
	Findings  []LibrarianFinding `json:"findings"`
	Truncated bool               `json:"truncated"`
}

func newLibrarianReport(job string, now time.Time) *LibrarianReport {
	return &LibrarianReport{Job: job, RanAt: now, Counts: map[string]int{}, Findings: []LibrarianFinding{}}
}

// add records a finding and counts it under reason. Past the cap the finding
// is counted but not listed.
func (r *LibrarianReport) add(reason string, f LibrarianFinding) {
	r.Counts[reason]++
	f.Reason = reason
	if len(r.Findings) >= librarianFindingLimit {
		r.Truncated = true
		return
	}
	r.Findings = append(r.Findings, f)
}

func linkFinding(bases map[string]db.KnowledgeBase, l db.KnowledgeLink) LibrarianFinding {
	f := LibrarianFinding{
		LinkID: util.UUIDToString(l.ID), Title: l.Title, Kind: l.Kind,
		KnowledgeBase: bases[util.UUIDToString(l.KnowledgeBaseID)].Key,
	}
	if l.SquadID.Valid {
		f.SquadID = util.UUIDToString(l.SquadID)
	}
	return f
}

// normalizeTitle is the form duplicate titles are compared in.
func normalizeTitle(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// kindsByKey loads the workspace's kinds.
func (s *KnowledgeService) kindsByKey(ctx context.Context, workspaceID pgtype.UUID) (map[string]db.KnowledgeKind, error) {
	rows, err := s.Queries.ListKnowledgeKinds(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list knowledge kinds: %w", err)
	}
	out := make(map[string]db.KnowledgeKind, len(rows))
	for _, k := range rows {
		out[k.Key] = k
	}
	return out, nil
}

// DraftTriage is the daily routine: what came in as a draft since the last run,
// which drafts repeat a title already in the base, and which drafts lack the
// headings their kind requires. Drafts are read from WeKnora for the headings,
// so an unreachable WeKnora fails the run for a retry.
func (s *KnowledgeService) DraftTriage(ctx context.Context, workspaceID pgtype.UUID, now time.Time) (*LibrarianReport, error) {
	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	bases, err := s.baseKeys(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	kinds, err := s.kindsByKey(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	links, err := s.Queries.ListKnowledgeLinks(ctx, db.ListKnowledgeLinksParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, fmt.Errorf("list knowledge records: %w", err)
	}
	report := newLibrarianReport(LibrarianJobTriage, now)

	// Titles of every live document, per base, so a draft can find its twin.
	type titleKey struct{ base, title string }
	owners := map[titleKey]db.KnowledgeLink{}
	for _, l := range links {
		if l.State != KnowledgeLinkDraft && l.State != KnowledgeLinkPublished {
			continue
		}
		k := titleKey{util.UUIDToString(l.KnowledgeBaseID), normalizeTitle(l.Title)}
		if _, taken := owners[k]; !taken {
			owners[k] = l
		}
	}
	reads := 0
	for _, l := range links {
		if l.State != KnowledgeLinkDraft {
			continue
		}
		if now.Sub(l.CreatedAt.Time) <= librarianDraftWindow {
			report.Counts["new_drafts"]++
		}
		f := linkFinding(bases, l)
		if other, ok := owners[titleKey{util.UUIDToString(l.KnowledgeBaseID), normalizeTitle(l.Title)}]; ok && other.ID != l.ID {
			f.OtherLinkID = util.UUIDToString(other.ID)
			report.add("duplicate", f)
		}
		k, typed := kinds[l.Kind]
		if !typed || l.WeknoraKnowledgeID == "" || len(k.RequiredSections) == 0 || reads >= librarianReadLimit {
			continue
		}
		reads++
		doc, err := ws.Read(ctx, l.WeknoraKnowledgeID)
		if errors.Is(err, knowledge.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read draft for triage: %w", err)
		}
		if missing := MissingSections(k.RequiredSections, doc.Content); len(missing) > 0 {
			f.Missing = missing
			report.add("missing_sections", f)
		}
	}
	if reads >= librarianReadLimit {
		report.Truncated = true
	}
	return report, nil
}

// knowledgeReviewWindow is how long a document's last review request holds off
// the next one, once that request is closed.
const knowledgeReviewWindow = 30 * 24 * time.Hour

// FreshnessReview is the weekly routine. A published document past its
// valid_until, or past its kind's review_days, is due. The squad that owns it
// gets a review request: an issue assigned to the squad that names the document
// (see requestReview). A document with no owning squad is reported and not
// requested. A superseded document is not reviewed: its successor is.
func (s *KnowledgeService) FreshnessReview(ctx context.Context, workspaceID pgtype.UUID, now time.Time) (*LibrarianReport, error) {
	if s.Issues == nil {
		return nil, errors.New("knowledge: the freshness review needs the issue service")
	}
	bases, err := s.baseKeys(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	kinds, err := s.kindsByKey(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	links, err := s.Queries.ListPublishedKnowledgeLinks(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list published documents: %w", err)
	}
	superseded, err := s.supersededSet(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	report := newLibrarianReport(LibrarianJobFreshness, now)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	for _, l := range links {
		if superseded[util.UUIDToString(l.ID)] {
			continue
		}
		due := false
		switch {
		case l.ValidUntil.Valid && l.ValidUntil.Time.Before(today):
			due = true
		case kinds[l.Kind].ReviewDays > 0 && l.PublishedAt.Valid &&
			l.PublishedAt.Time.Add(time.Duration(kinds[l.Kind].ReviewDays)*24*time.Hour).Before(now):
			due = true
		}
		if !due {
			continue
		}
		reason, err := s.requestReview(ctx, workspaceID, l, now)
		if err != nil {
			return nil, err
		}
		report.Counts["review_due"]++
		report.add(reason, linkFinding(bases, l))
	}
	return report, nil
}

// executiveOfficeSquadName is the starter company's Executive Office squad, the
// Librarian's home. It takes the review requests of documents no squad owns.
const executiveOfficeSquadName = "Executive Office"

// executiveOfficeSquad returns the workspace's Executive Office squad, or the
// zero id when the starter company has not installed one.
func (s *KnowledgeService) executiveOfficeSquad(ctx context.Context, workspaceID pgtype.UUID) (pgtype.UUID, error) {
	squads, err := s.Queries.ListSquads(ctx, workspaceID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("list squads: %w", err)
	}
	for _, sq := range squads {
		if sq.Name == executiveOfficeSquadName {
			return sq.ID, nil
		}
	}
	return pgtype.UUID{}, nil
}

// requestReview opens the review request of a due document, unless one stands,
// and returns the finding reason. The request goes to the document's owning
// squad, the squad of the agent that wrote it. A document with no owning squad
// goes to the Executive Office squad, which drafts a superseding update or asks
// the boss; with no Executive Office squad it is reported and not requested. A
// request stands while its issue is open, and for knowledgeReviewWindow after it
// was opened, so a closed request is not repeated at once.
func (s *KnowledgeService) requestReview(ctx context.Context, workspaceID pgtype.UUID, l db.KnowledgeLink, now time.Time) (string, error) {
	assignee := l.SquadID
	ownerless := !assignee.Valid
	if ownerless {
		exec, err := s.executiveOfficeSquad(ctx, workspaceID)
		if err != nil {
			return "", err
		}
		if !exec.Valid {
			slog.Info("knowledge: a due document has no owning squad and no Executive Office; not requested", "link_id", util.UUIDToString(l.ID), "workspace_id", util.UUIDToString(workspaceID))
			return "review_no_squad", nil
		}
		assignee = exec
	}
	if l.ReviewIssueID.Valid {
		prev, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: l.ReviewIssueID, WorkspaceID: workspaceID})
		switch {
		case err == nil:
			closed := prev.Status == "done" || prev.Status == "cancelled"
			if !closed || now.Sub(prev.CreatedAt.Time) < knowledgeReviewWindow {
				return "review_already_requested", nil
			}
		case errors.Is(err, pgx.ErrNoRows):
			// The issue was deleted, so the document is due for a new request.
		default:
			return "", fmt.Errorf("load review request: %w", err)
		}
	}
	owners, err := s.Queries.ListWorkspaceManagerUserIDs(ctx, workspaceID)
	if err != nil {
		return "", fmt.Errorf("list workspace owners: %w", err)
	}
	if len(owners) == 0 {
		return "", errors.New("knowledge: a workspace with knowledge has no owner to file a review request")
	}
	res, err := s.Issues.Create(ctx, IssueCreateParams{
		WorkspaceID:    workspaceID,
		Title:          "Review request: " + l.Title,
		Description:    pgtype.Text{String: reviewRequestBody(l, ownerless), Valid: true},
		Status:         "todo",
		Priority:       "none",
		AssigneeType:   pgtype.Text{String: "squad", Valid: true},
		AssigneeID:     assignee,
		CreatorType:    "member",
		CreatorID:      owners[0],
		AllowDuplicate: true,
	}, IssueCreateOpts{})
	if err != nil {
		return "", fmt.Errorf("create review request: %w", err)
	}
	if err := s.Queries.SetKnowledgeLinkReviewIssue(ctx, db.SetKnowledgeLinkReviewIssueParams{
		ID: l.ID, WorkspaceID: workspaceID, ReviewIssueID: res.Issue.ID,
	}); err != nil {
		return "", fmt.Errorf("record review request: %w", err)
	}
	return "review_requested", nil
}

// reviewRequestBody names the document the request is about, by its registry
// link and its WeKnora id, so the owning squad can find it with the CLI.
func reviewRequestBody(l db.KnowledgeLink, ownerless bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The Librarian's freshness review found the knowledge document %q due for review.\n\n", l.Title)
	fmt.Fprintf(&b, "- Kind: %s\n", l.Kind)
	if l.ValidUntil.Valid {
		fmt.Fprintf(&b, "- Valid until: %s\n", l.ValidUntil.Time.Format("2006-01-02"))
	}
	fmt.Fprintf(&b, "- Knowledge link: %s\n- WeKnora document: %s\n\n", util.UUIDToString(l.ID), l.WeknoraKnowledgeID)
	if ownerless {
		b.WriteString("No squad owns this document, so the request comes to the Executive Office. Draft an update that supersedes it, or ask the boss who should own it. Close this request when that is done.\n")
		return b.String()
	}
	b.WriteString("Check the document. Publish a version that supersedes it, or set a new valid_until date. Close this request when that is done.\n")
	return b.String()
}

// supersededSet is the link ids a published document replaces.
func (s *KnowledgeService) supersededSet(ctx context.Context, workspaceID pgtype.UUID) (map[string]bool, error) {
	ids, err := s.Queries.ListSupersededLinkIDs(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list superseded documents: %w", err)
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[util.UUIDToString(id)] = true
	}
	return out, nil
}

// TaxonomyAudit is the monthly routine. It finds published documents with no
// kind (reported only) and published links whose document WeKnora no longer
// holds. With repair, only those orphaned link rows are deleted: the document
// they described is gone, so the row points at nothing. Every other change,
// such as giving a document a kind or re-linking it, is a person's decision.
// A listing that fails leaves the audit unrun. A listing that stops at the page
// cap cannot tell a gone document from one past the cap, so nothing is
// repaired on it.
func (s *KnowledgeService) TaxonomyAudit(ctx context.Context, workspaceID pgtype.UUID, now time.Time, repair bool) (*LibrarianReport, error) {
	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	bases, err := s.baseKeys(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	links, err := s.Queries.ListPublishedKnowledgeLinks(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list published documents: %w", err)
	}
	report := newLibrarianReport(LibrarianJobAudit, now)

	present := map[string]map[string]bool{} // base id -> WeKnora document ids
	partial := false
	for baseID, b := range bases {
		ids, truncated, err := s.listWeKnoraDocumentIDs(ctx, ws, b.WeknoraKbID)
		if err != nil {
			return nil, err
		}
		if truncated {
			report.Truncated = true
			partial = true
		}
		present[baseID] = ids
	}
	for _, l := range links {
		f := linkFinding(bases, l)
		if l.Kind == "" {
			report.add("missing_kind", f)
		}
		baseID := util.UUIDToString(l.KnowledgeBaseID)
		if l.WeknoraKnowledgeID == "" || present[baseID] == nil || present[baseID][l.WeknoraKnowledgeID] {
			continue
		}
		report.add("orphaned", f)
		if !repair || partial {
			continue
		}
		if _, err := s.Queries.DeleteKnowledgeLink(ctx, db.DeleteKnowledgeLinkParams{ID: l.ID, WorkspaceID: workspaceID}); err != nil {
			return nil, fmt.Errorf("repair orphaned link: %w", err)
		}
		report.Counts["repaired"]++
	}
	return report, nil
}

// listWeKnoraDocumentIDs returns the ids of every document in a WeKnora base.
// truncated is true past the page cap.
func (s *KnowledgeService) listWeKnoraDocumentIDs(ctx context.Context, ws knowledge.Workspace, kbID string) (map[string]bool, bool, error) {
	ids := map[string]bool{}
	for page := 1; ; page++ {
		if page > librarianListMaxPages {
			return ids, true, nil
		}
		p, err := ws.ListDocuments(ctx, kbID, knowledge.ListOptions{Page: page, PageSize: librarianListPageSize})
		if err != nil {
			return nil, false, fmt.Errorf("list documents of a knowledge base: %w", err)
		}
		for _, d := range p.Items {
			ids[d.ID] = true
		}
		if len(p.Items) == 0 || page*librarianListPageSize >= p.Total {
			return ids, false, nil
		}
	}
}

// RecordLibrarianReport stores a routine's report as the workspace's latest
// run of that job.
func (s *KnowledgeService) RecordLibrarianReport(ctx context.Context, workspaceID pgtype.UUID, report *LibrarianReport) error {
	summary, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	if _, err := s.Queries.CreateKnowledgeHealthRun(ctx, db.CreateKnowledgeHealthRunParams{WorkspaceID: workspaceID, Job: report.Job, Summary: summary}); err != nil {
		return fmt.Errorf("record %s report: %w", report.Job, err)
	}
	return nil
}

// HealthRunView is the latest run of one Librarian job.
type HealthRunView struct {
	Job    string          `json:"job"`
	RanAt  time.Time       `json:"ran_at"`
	Report json.RawMessage `json:"report"`
}

// LatestHealth returns the newest run of each Librarian job of the workspace.
func (s *KnowledgeService) LatestHealth(ctx context.Context, workspaceID pgtype.UUID) ([]HealthRunView, error) {
	rows, err := s.Queries.ListLatestKnowledgeHealthRuns(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list health runs: %w", err)
	}
	out := make([]HealthRunView, 0, len(rows))
	for _, r := range rows {
		out = append(out, HealthRunView{Job: r.Job, RanAt: r.RanAt.Time, Report: json.RawMessage(r.Summary)})
	}
	return out, nil
}

// RunLibrarian runs one Librarian job for every workspace whose knowledge is
// ready. It returns how many workspaces completed. A WeKnora outage stops the
// pass, so the scheduler retries the whole job; any other failure is logged and
// the pass goes on, and the failures come back joined.
func (s *KnowledgeService) RunLibrarian(ctx context.Context, job string, now time.Time) (int, error) {
	if s == nil {
		return 0, nil
	}
	ids, err := s.Queries.ListReadyKnowledgeWorkspaceIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("list knowledge workspaces: %w", err)
	}
	var failures []error
	done := 0
	for _, id := range ids {
		err := s.runLibrarianFor(ctx, id, job, now)
		if err == nil {
			done++
			continue
		}
		if errors.Is(err, knowledge.ErrUnavailable) {
			return done, err
		}
		slog.Warn("knowledge: librarian job failed for a workspace", "job", job, "workspace_id", util.UUIDToString(id), "error", err)
		failures = append(failures, fmt.Errorf("workspace %s: %w", util.UUIDToString(id), err))
	}
	return done, errors.Join(failures...)
}

func (s *KnowledgeService) runLibrarianFor(ctx context.Context, workspaceID pgtype.UUID, job string, now time.Time) error {
	var (
		report *LibrarianReport
		err    error
	)
	switch job {
	case LibrarianJobTaxonomyBackfill:
		return s.backfillTaxonomy(ctx, workspaceID)
	case LibrarianJobTriage:
		report, err = s.DraftTriage(ctx, workspaceID, now)
	case LibrarianJobFreshness:
		report, err = s.FreshnessReview(ctx, workspaceID, now)
	case LibrarianJobAudit:
		report, err = s.TaxonomyAudit(ctx, workspaceID, now, true)
	case LibrarianJobEval:
		eval, evalErr := s.RunEval(ctx, workspaceID, now)
		if evalErr != nil || eval.State != KnowledgeStateReady {
			return evalErr
		}
		return nil
	default:
		return &KnowledgeInvalidError{"unknown librarian job " + job}
	}
	if err != nil {
		return err
	}
	return s.RecordLibrarianReport(ctx, workspaceID, report)
}

// backfillTaxonomy brings a workspace provisioned before phase 3 up to date: its
// missing default kinds, and the ventures base with its MCP access. It reuses
// the provisioning steps, which are idempotent. It takes the provisioning lock,
// and skips the workspace when an enable is running; the next pass retries.
func (s *KnowledgeService) backfillTaxonomy(ctx context.Context, workspaceID pgtype.UUID) error {
	kinds, err := s.Queries.ListKnowledgeKinds(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list knowledge kinds: %w", err)
	}
	if len(kinds) < len(defaultKnowledgeKinds) {
		if err := s.SeedKnowledgeKinds(ctx, workspaceID); err != nil {
			return err
		}
	}
	bases, err := s.Queries.ListKnowledgeBases(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list knowledge bases: %w", err)
	}
	for _, b := range bases {
		if b.Key == KnowledgeVentureBaseKey {
			return nil
		}
	}
	release, err := s.lock(ctx, workspaceID)
	if errors.Is(err, ErrKnowledgeBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()
	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return err
	}
	actor := pgtype.UUID{}
	if owners, err := s.Queries.ListWorkspaceManagerUserIDs(ctx, workspaceID); err == nil && len(owners) > 0 {
		actor = owners[0]
	}
	return s.provision(ctx, workspaceID, actor, ws)
}
