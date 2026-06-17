package cerebro

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// LearningSweeper periodically turns observation aggregates that have crossed
// the learning threshold (>=10 runs, >=70% consistency) into additive skill
// change-request proposals. It only acts for workspaces with the
// cerebro_skill_learning feature flag ON; everything else is skipped silently.
//
// One per process, owned by main.go via NewLearningSweeper(...) and started
// with Run(ctx, interval). The recording side (skill_record_observation tool +
// RecordObservation) feeds the aggregates this sweeper reads.
type LearningSweeper struct {
	Pool    *pgxpool.Pool
	Cerebro *cerebrodb.Queries
	Main    *db.Queries

	// nowFunc is overridden in tests so the sweeper can run at a known
	// instant without mocking time globally.
	nowFunc func() time.Time
}

// NewLearningSweeper builds a LearningSweeper.
func NewLearningSweeper(pool *pgxpool.Pool, cerebro *cerebrodb.Queries, main *db.Queries) *LearningSweeper {
	return &LearningSweeper{Pool: pool, Cerebro: cerebro, Main: main, nowFunc: time.Now}
}

// Run blocks on ctx and ticks at the requested interval. Returns when ctx is
// cancelled. Mirrors the note-types / sprints sweepers.
func (s *LearningSweeper) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("cerebro skill-learning sweeper: initial tick failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("cerebro skill-learning sweeper: tick failed", "error", err)
			}
		}
	}
}

// Tick runs one pass over every aggregate above the learning threshold,
// proposing a change request for each — but only for workspaces whose
// cerebro_skill_learning flag is ON.
func (s *LearningSweeper) Tick(ctx context.Context) error {
	flagCache := map[[16]byte]bool{}
	proposed, err := LearnFromObservations(ctx, s.Cerebro, func(ctx context.Context, _ *cerebrodb.Queries, row cerebrodb.ListAggregatesAboveThresholdRow) error {
		enabled, ferr := s.isFlagEnabled(ctx, row.WorkspaceID, flagCache)
		if ferr != nil {
			return ferr
		}
		if !enabled {
			// Flag OFF for this workspace — leave the aggregate untouched so
			// it is reconsidered if the workspace later opts in. Not an error.
			return errSkipProposal
		}
		return s.proposeChangeRequest(ctx, row)
	})
	if err != nil {
		return err
	}
	if proposed > 0 {
		slog.Info("cerebro skill-learning sweeper: proposed change requests", "count", proposed)
	}
	return nil
}

// errSkipProposal signals that a candidate aggregate was intentionally skipped
// (flag OFF) so LearnFromObservations does not count or mark it.
var errSkipProposal = errors.New("skill-learning: proposal skipped")

// proposeChangeRequest builds and persists an additive skill change request for
// one threshold-crossing aggregate, then marks the aggregate as proposed.
func (s *LearningSweeper) proposeChangeRequest(ctx context.Context, row cerebrodb.ListAggregatesAboveThresholdRow) error {
	skill, err := s.Main.GetSkill(ctx, row.SkillID)
	if err != nil {
		return fmt.Errorf("load skill: %w", err)
	}

	proposer, err := s.resolveProposer(ctx, skill)
	if err != nil {
		return err
	}
	if !proposer.Valid {
		// No human owner/admin to attribute the proposal to — skip silently.
		return errSkipProposal
	}

	proposedVersion := bumpPatchVersion(skill.CurrentVersion)
	proposedContent := appendLearnedSuggestion(skill.Content, row)

	if _, err := s.Main.CreateSkillChangeRequest(ctx, db.CreateSkillChangeRequestParams{
		SkillID:         row.SkillID,
		Title:           BuildLearnedChangeTitle(row),
		Description:     BuildLearnedChangeRequestBody(row),
		BaseVersion:     skill.CurrentVersion,
		ProposedVersion: proposedVersion,
		ProposedContent: proposedContent,
		ProposedFiles:   []byte("[]"),
		ProposedBy:      proposer,
		WorkSessionID:   pgtype.UUID{},
	}); err != nil {
		return fmt.Errorf("create change request: %w", err)
	}

	return MarkProposed(ctx, s.Cerebro, row.SkillID, row.DeltaType, row.DeltaValue)
}

// resolveProposer picks the user the auto-learned proposal is attributed to.
// proposed_by is NOT NULL REFERENCES "user"(id), so it must be a real user who
// can also review it: the skill owner when set, else the first workspace
// owner/admin.
func (s *LearningSweeper) resolveProposer(ctx context.Context, skill db.Skill) (pgtype.UUID, error) {
	if skill.OwnerID.Valid {
		return skill.OwnerID, nil
	}
	admins, err := s.Main.ListWorkspaceAdminUserIDs(ctx, skill.WorkspaceID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("list workspace admins: %w", err)
	}
	if len(admins) > 0 {
		return admins[0], nil
	}
	return pgtype.UUID{}, nil
}

// isFlagEnabled reports whether the workspace cerebro_skill_learning flag is ON,
// caching per Tick so a workspace with several aggregates pays one round-trip.
func (s *LearningSweeper) isFlagEnabled(ctx context.Context, workspaceID pgtype.UUID, cache map[[16]byte]bool) (bool, error) {
	if !workspaceID.Valid {
		return false, nil
	}
	if v, ok := cache[workspaceID.Bytes]; ok {
		return v, nil
	}
	enabled, err := s.Cerebro.GetCerebroSkillLearningFlagForWorkspace(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			cache[workspaceID.Bytes] = false
			return false, nil
		}
		return false, err
	}
	cache[workspaceID.Bytes] = enabled
	return enabled, nil
}

// appendLearnedSuggestion returns the skill content with the auto-learned
// suggestion appended. The proposal is purely additive — existing content is
// never altered, matching the "can only add, never remove" promise.
func appendLearnedSuggestion(content string, row cerebrodb.ListAggregatesAboveThresholdRow) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimRight(content, "\n"))
	sb.WriteString("\n\n## [Auto-lært tilføjelse]\n\n")
	sb.WriteString(buildSuggestedDiff(row.DeltaType, row.DeltaValue))
	sb.WriteString("\n")
	return sb.String()
}

// bumpPatchVersion increments the patch component of a semver string. Falls
// back to "0.0.1" when the current version cannot be parsed, so the proposed
// version is always strictly greater than an unparseable base.
func bumpPatchVersion(current string) string {
	parts := strings.SplitN(strings.TrimSpace(current), ".", 3)
	if len(parts) != 3 {
		return "0.0.1"
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return "0.0.1"
	}
	return fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+1)
}
