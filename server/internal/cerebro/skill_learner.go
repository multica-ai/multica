package cerebro

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// Learning thresholds: a pattern must appear in at least MinRuns runs with
// at least MinConsistency fraction being successful outcomes before a change
// request is auto-proposed.
const (
	LearningMinRuns        = 10
	LearningMinConsistency = 0.70
	LearningMaxProposalAge = 7 * 24 * time.Hour // one per skill per week
)

// LearnFromObservations scans all aggregates that have crossed the threshold
// and generates a skill change-request for each. Safe to call repeatedly —
// already-proposed aggregates are skipped via auto_proposed_at.
func LearnFromObservations(ctx context.Context, q *cerebrodb.Queries, proposeChangeFn ProposeFn) (int, error) {
	rows, err := q.ListAggregatesAboveThreshold(ctx)
	if err != nil {
		return 0, err
	}

	proposed := 0
	for _, row := range rows {
		if err := proposeChangeFn(ctx, q, row); err != nil {
			// Log and continue — a single failure shouldn't stop the batch.
			continue
		}
		proposed++
	}
	return proposed, nil
}

// ProposeFn is the callback that creates a change request. Injected so tests
// can stub it without a full handler stack.
type ProposeFn func(ctx context.Context, q *cerebrodb.Queries, row cerebrodb.ListAggregatesAboveThresholdRow) error

// BuildLearnedChangeRequestBody generates the human-readable body for an
// auto-learned change request proposal.
func BuildLearnedChangeRequestBody(row cerebrodb.ListAggregatesAboveThresholdRow) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**[Auto-lært forslag]** — genereret af pattern extraction\n\n")
	fmt.Fprintf(&sb, "**Skill:** %s\n", row.SkillName)
	fmt.Fprintf(&sb, "**Delta-type:** %s\n", row.DeltaType)
	fmt.Fprintf(&sb, "**Observeret adfærd:** `%s`\n\n", row.DeltaValue)
	fmt.Fprintf(&sb, "**Bevis:**\n")
	fmt.Fprintf(&sb, "- %d runs observeret\n", row.RunCount)
	fmt.Fprintf(&sb, "- %.0f%% konsistens (threshold: %d%%)\n\n", row.ConsistencyPct*100, int(LearningMinConsistency*100))
	fmt.Fprintf(&sb, "**Foreslået tilføjelse til skill:**\n")
	fmt.Fprintf(&sb, "%s\n\n", buildSuggestedDiff(row.DeltaType, row.DeltaValue))
	fmt.Fprintf(&sb, "*Dette forslag kan kun tilføje til skillen — aldrig fjerne. Godkend eller afvis.*\n")
	return sb.String()
}

// BuildLearnedChangeTitle returns a concise title for an auto-learning proposal.
func BuildLearnedChangeTitle(row cerebrodb.ListAggregatesAboveThresholdRow) string {
	switch row.DeltaType {
	case "extra_table_access":
		return fmt.Sprintf("[Auto-lært] Tilføj %s til data_domains i %s", row.DeltaValue, row.SkillName)
	case "skill_chain_addition":
		return fmt.Sprintf("[Auto-lært] Tilføj %s til called_skills i %s", row.DeltaValue, row.SkillName)
	case "extra_verification_step":
		return fmt.Sprintf("[Auto-lært] Tilføj verificeringstrin til %s", row.SkillName)
	case "output_format_deviation":
		return fmt.Sprintf("[Auto-lært] Opdatér output-format i %s", row.SkillName)
	default:
		return fmt.Sprintf("[Auto-lært] Forslag til %s fra %d observationer", row.SkillName, row.RunCount)
	}
}

func buildSuggestedDiff(deltaType, deltaValue string) string {
	switch deltaType {
	case "extra_table_access":
		return fmt.Sprintf("Tilføj til data_domains frontmatter:\n```yaml\ndata_domains:\n  - project: %s\n```", extractProject(deltaValue))
	case "skill_chain_addition":
		return fmt.Sprintf("Tilføj til instrukserne:\nDenne skill kalder typisk også: `%s`", deltaValue)
	case "extra_verification_step":
		return fmt.Sprintf("Tilføj verifikationstrin observeret i praksis:\n`%s`", deltaValue)
	case "output_format_deviation":
		return fmt.Sprintf("Output-format observeret i praksis:\n`%s`", deltaValue)
	default:
		return fmt.Sprintf("Observeret delta: `%s`", deltaValue)
	}
}

func extractProject(tableRef string) string {
	// 'firtal_dwh.table' → 'firtal_dwh' ; 'firtal-dwh.table' → 'firtal-dwh'
	if idx := strings.Index(tableRef, "."); idx > 0 {
		return tableRef[:idx]
	}
	return tableRef
}

// MarkProposed marks an aggregate as having had a change request generated.
func MarkProposed(ctx context.Context, q *cerebrodb.Queries, skillID pgtype.UUID, deltaType, deltaValue string) error {
	return q.MarkAggregateProposed(ctx, cerebrodb.MarkAggregateProposedParams{
		SkillID:    skillID,
		DeltaType:  deltaType,
		DeltaValue: deltaValue,
	})
}
