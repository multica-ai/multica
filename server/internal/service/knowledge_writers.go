package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// System writers: the records Multica itself puts in the knowledge base,
// published, under idempotency keys so a retry or a repeated call writes once.

// DecisionRecord is a decided proposal, as written to the decisions base.
type DecisionRecord struct {
	ProposalID     pgtype.UUID
	Title          string
	Squad          string
	Recommendation string
	// Decision is "approved" or "rejected".
	Decision  string
	Note      string
	DecidedAt time.Time
	// Link is where the proposal can be opened.
	Link string
}

// DecisionRecorder records decided proposals. ProposalService calls it after
// a decision commits.
type DecisionRecorder interface {
	WriteDecisionRecord(ctx context.Context, workspaceID pgtype.UUID, rec DecisionRecord) error
}

// DecisionIdempotencyKey is the key of a proposal's decision record.
func DecisionIdempotencyKey(proposalID pgtype.UUID) string {
	return "proposal:" + util.UUIDToString(proposalID) + ":decision"
}

// BriefIdempotencyKey is the key of a brief's archive record.
func BriefIdempotencyKey(briefID pgtype.UUID) string {
	return "brief:" + util.UUIDToString(briefID)
}

// Markdown renders the record the way it is stored and retrieved.
func (r DecisionRecord) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Decision: %s\n\n", r.Title)
	fmt.Fprintf(&b, "- Squad: %s\n", r.Squad)
	fmt.Fprintf(&b, "- Decision: %s\n", r.Decision)
	fmt.Fprintf(&b, "- Date: %s\n", r.DecidedAt.UTC().Format("2006-01-02"))
	if r.Link != "" {
		fmt.Fprintf(&b, "- Proposal: %s\n", r.Link)
	}
	b.WriteString("\n## Recommendation\n\n")
	b.WriteString(orNone(r.Recommendation))
	b.WriteString("\n\n## Note from the boss\n\n")
	b.WriteString(orNone(r.Note))
	b.WriteString("\n")
	return b.String()
}

func orNone(s string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return "(none)"
}

// WriteDecisionRecord publishes a decision record to the decisions base. A
// workspace that has not enabled knowledge is skipped without error. When
// WeKnora does not answer, the record stays as a failed link that the next
// call (for any decision of the workspace) retries, and the error is returned
// for the caller to log.
func (s *KnowledgeService) WriteDecisionRecord(ctx context.Context, workspaceID pgtype.UUID, rec DecisionRecord) error {
	if s == nil {
		return nil
	}
	s.RetryFailed(ctx, workspaceID)
	return s.writeSystem(ctx, workspaceID, KnowledgeDocumentInput{
		KB: DecisionsKnowledgeKey, Title: truncateRunes(rec.Title, knowledgeMaxTitle), Content: rec.Markdown(), Publish: true,
		SourceType: "proposal", SourceID: rec.ProposalID, IdempotencyKey: DecisionIdempotencyKey(rec.ProposalID),
	})
}

// WriteBriefArchive publishes a brief to the decisions base, keyed by the
// brief so publishing it again writes nothing new. Like WriteDecisionRecord
// it skips a workspace without knowledge and keeps a failed link when WeKnora
// is down.
func (s *KnowledgeService) WriteBriefArchive(ctx context.Context, workspaceID, briefID pgtype.UUID, title, body string) error {
	if s == nil {
		return nil
	}
	return s.writeSystem(ctx, workspaceID, KnowledgeDocumentInput{
		KB: DecisionsKnowledgeKey, Title: truncateRunes(title, knowledgeMaxTitle), Content: body, Publish: true,
		SourceType: "brief", SourceID: briefID, IdempotencyKey: BriefIdempotencyKey(briefID),
	})
}

func (s *KnowledgeService) writeSystem(ctx context.Context, workspaceID pgtype.UUID, in KnowledgeDocumentInput) error {
	_, _, err := s.CreateDocument(ctx, workspaceID, KnowledgeActor{Type: KnowledgeActorSystem}, in)
	var notReady *KnowledgeNotReadyError
	if errors.As(err, &notReady) && notReady.State == KnowledgeStateDisabled {
		return nil
	}
	return err
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
