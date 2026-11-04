package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// The built-in metrics (spec 4.1), computed nightly into readings. A window
// metric reads one UTC day and is dated at the start of that day. A snapshot
// metric reads the state now and is dated at the start of today, so a second run
// the same day replaces it rather than adding one.
//
// A metric with no data in its window has no reading: a rate with no denominator
// is not written as zero.

// costTicksPerUSD converts provider cost ticks (1e-10 USD) to dollars.
const costTicksPerUSD = 1e10

// builtinScope is one scope the nightly job reads: the company, a floor, a
// squad or an initiative. squadIDs are the squads whose work the scope covers;
// nil means the whole workspace.
type builtinScope struct {
	scopeType string
	scopeID   pgtype.UUID
	squadIDs  []pgtype.UUID
	// initiativeID is set for an initiative scope.
	initiativeID pgtype.UUID
}

// builtinReading is one value to write.
type builtinReading struct {
	key        string
	scope      builtinScope
	value      float64
	observedAt time.Time
}

// RollupBuiltinMetrics computes the built-in readings of one workspace at every
// scope, as of now, in one transaction. It returns how many readings it wrote.
func (s *MetricService) RollupBuiltinMetrics(ctx context.Context, ws pgtype.UUID, now time.Time) (int, error) {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)

	if err := s.EnsureBuiltinMetrics(ctx, q, ws); err != nil {
		return 0, err
	}
	pipeline, err := NewDigestService(q).computePipeline(ctx, ws, now)
	if err != nil {
		return 0, err
	}
	scopes, err := s.builtinScopes(ctx, q, ws, pipeline)
	if err != nil {
		return 0, err
	}
	routines, err := RoutineComplianceStates(ctx, q, ws, pgtype.UUID{}, now)
	if err != nil {
		return 0, err
	}

	knowledge, err := q.BuiltinKnowledgeBySquad(ctx, db.BuiltinKnowledgeBySquadParams{
		WorkspaceID:     ws,
		Since:           pgtype.Timestamptz{Time: yesterday, Valid: true},
		Until:           pgtype.Timestamptz{Time: today, Valid: true},
		PublishedBefore: pgtype.Timestamptz{Time: now.Add(-DigestKnowledgeStaleAfter), Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("knowledge by squad metric: %w", err)
	}
	knowledgeBySquad := make(map[string]db.BuiltinKnowledgeBySquadRow, len(knowledge))
	for _, k := range knowledge {
		knowledgeBySquad[util.UUIDToString(k.SquadID)] = k
	}

	var readings []builtinReading
	for _, sc := range scopes {
		readings = append(readings, s.windowReadings(ctx, q, ws, sc, yesterday, today)...)
		readings = append(readings, snapshotReadings(sc, pipeline, routines, today)...)
		readings = append(readings, knowledgeReadings(sc, knowledgeBySquad, yesterday, today)...)
	}
	companyReadings, err := s.companyOnlyReadings(ctx, q, ws, now, yesterday, today)
	if err != nil {
		return 0, err
	}
	readings = append(readings, companyReadings...)

	for _, r := range readings {
		if err := writeSystemReading(ctx, q, ws, r); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit built-in readings: %w", err)
	}
	return len(readings), nil
}

// builtinScopes lists the company, every floor with squads, every live squad and
// every initiative of the pipeline reader, which is the one place that decides
// what "active" means.
func (s *MetricService) builtinScopes(ctx context.Context, q *db.Queries, ws pgtype.UUID, pipeline []DigestInitiative) ([]builtinScope, error) {
	squads, err := q.ListLiveSquadScopes(ctx, ws)
	if err != nil {
		return nil, fmt.Errorf("list squads for metrics: %w", err)
	}
	scopes := []builtinScope{{scopeType: ScopeCompany}}

	floorSquads := map[string][]pgtype.UUID{}
	for _, sq := range squads {
		if sq.FloorID.Valid {
			key := util.UUIDToString(sq.FloorID)
			floorSquads[key] = append(floorSquads[key], sq.ID)
		}
	}
	floors, err := q.ListOfficeFloors(ctx, ws)
	if err != nil {
		return nil, fmt.Errorf("list floors for metrics: %w", err)
	}
	for _, f := range floors {
		ids := floorSquads[util.UUIDToString(f.ID)]
		if len(ids) == 0 {
			continue
		}
		scopes = append(scopes, builtinScope{scopeType: ScopeFloor, scopeID: f.ID, squadIDs: ids})
	}
	for _, sq := range squads {
		scopes = append(scopes, builtinScope{scopeType: ScopeSquad, scopeID: sq.ID, squadIDs: []pgtype.UUID{sq.ID}})
	}
	for _, ini := range pipeline {
		id, err := util.ParseUUID(ini.ID)
		if err != nil {
			continue
		}
		scopes = append(scopes, builtinScope{scopeType: ScopeInitiative, scopeID: id, initiativeID: id})
	}
	return scopes, nil
}

// windowReadings are the day's readings for one scope: runs, proposals, delivery.
// An initiative scope has none of them.
func (s *MetricService) windowReadings(ctx context.Context, q *db.Queries, ws pgtype.UUID, sc builtinScope, yesterday, today time.Time) []builtinReading {
	if sc.scopeType == ScopeInitiative {
		return nil
	}
	since := pgtype.Timestamptz{Time: yesterday, Valid: true}
	until := pgtype.Timestamptz{Time: today, Valid: true}
	var out []builtinReading
	add := func(key string, value float64, ok bool) {
		if ok {
			out = append(out, builtinReading{key: key, scope: sc, value: value, observedAt: yesterday})
		}
	}

	runs, err := q.BuiltinRunStats(ctx, db.BuiltinRunStatsParams{WorkspaceID: ws, Since: since, Until: until, SquadIds: sc.squadIDs})
	if err == nil {
		finished := runs.Completed + runs.Failed
		add("runs.count", float64(finished), true)
		rate, ok := ratePercent(float64(runs.Completed), float64(finished))
		add("runs.success_rate", rate, ok)
		add("runs.cost_usd", float64(runs.CostUsdTicks)/costTicksPerUSD, true)
	}

	props, err := q.BuiltinProposalStats(ctx, db.BuiltinProposalStatsParams{WorkspaceID: ws, Since: since, Until: until, SquadIds: sc.squadIDs})
	if err == nil {
		decided := float64(props.Approved + props.Rejected)
		rate, ok := ratePercent(float64(props.Approved), decided)
		add("proposals.acceptance_rate", rate, ok)
		if decided > 0 {
			add("proposals.decision_hours", props.DecisionSeconds/3600/decided, true)
		}
	}

	delivery, err := q.BuiltinIssueDeliveryStats(ctx, db.BuiltinIssueDeliveryStatsParams{WorkspaceID: ws, Since: since, Until: until, SquadIds: sc.squadIDs})
	if err == nil {
		add("delivery.issues_done", float64(delivery.Done), true)
		if delivery.Done > 0 {
			add("delivery.cycle_time_days", delivery.CycleDaysTotal/float64(delivery.Done), true)
			// Rework is issues reopened out of done over issues done, in the same window.
			reopened, err := q.BuiltinIssuesReopened(ctx, db.BuiltinIssuesReopenedParams{WorkspaceID: ws, Since: since, Until: until, SquadIds: sc.squadIDs})
			if err == nil {
				rate, ok := ratePercent(float64(reopened), float64(delivery.Done))
				add("delivery.rework_rate", rate, ok)
			}
		}
	}

	if sc.scopeType == ScopeFloor {
		usefulness, err := q.BuiltinFloorBriefUsefulness(ctx, db.BuiltinFloorBriefUsefulnessParams{
			WorkspaceID: ws, FloorID: sc.scopeID, Since: since, Until: until,
		})
		if err == nil {
			rate, ok := ratePercent(float64(usefulness.Useful), float64(usefulness.Total))
			add("brief.usefulness", rate, ok)
		}
	}
	return out
}

// knowledgeReadings are the knowledge metrics of a floor or a squad: the sum of
// its squads' published documents, attributed by the agent that published them
// (see BuiltinKnowledgeBySquad). The company scope and initiatives get none here;
// the company's readings come from companyOnlyReadings.
func knowledgeReadings(sc builtinScope, bySquad map[string]db.BuiltinKnowledgeBySquadRow, yesterday, today time.Time) []builtinReading {
	if sc.scopeType == ScopeCompany || sc.scopeType == ScopeInitiative {
		return nil
	}
	var inWindow, total, stale int32
	for _, id := range sc.squadIDs {
		k, ok := bySquad[util.UUIDToString(id)]
		if !ok {
			continue
		}
		inWindow += k.PublishedInWindow
		total += k.PublishedTotal
		stale += k.StaleTotal
	}
	out := []builtinReading{{key: "knowledge.docs_published", scope: sc, value: float64(inWindow), observedAt: yesterday}}
	if rate, ok := ratePercent(float64(total-stale), float64(total)); ok {
		out = append(out, builtinReading{key: "knowledge.fresh_ratio", scope: sc, value: rate, observedAt: today})
	}
	return out
}

// snapshotReadings are the state-now readings for one scope: routines and the
// pipeline. They are dated today.
func snapshotReadings(sc builtinScope, pipeline []DigestInitiative, routines []RoutineCompliance, today time.Time) []builtinReading {
	var out []builtinReading
	add := func(key string, value float64) {
		out = append(out, builtinReading{key: key, scope: sc, value: value, observedAt: today})
	}

	if sc.scopeType == ScopeInitiative {
		for _, ini := range pipeline {
			if ini.ID != util.UUIDToString(sc.initiativeID) {
				continue
			}
			add("pipeline.stage_days", float64(ini.DaysInStage))
			stalled := 0.0
			if ini.Stalled {
				stalled = 1
			}
			add("pipeline.stalled", stalled)
		}
		return out
	}

	onTime, due := 0, 0
	for _, r := range routines {
		if !r.Required || r.State == RoutineStatePaused || !inScopeSquad(sc, &r.SquadID) {
			continue
		}
		due++
		if r.State == RoutineStateOnTime {
			onTime++
		}
	}
	if rate, ok := ratePercent(float64(onTime), float64(due)); ok {
		add("routines.compliance_rate", rate)
	}

	active, stalled, stageDays, n := 0, 0, 0, 0
	for _, ini := range pipeline {
		if !inScopeSquad(sc, ini.OwnerSquadID) {
			continue
		}
		active++
		if ini.Stalled {
			stalled++
		}
		stageDays += ini.DaysInStage
		n++
	}
	add("pipeline.initiatives_active", float64(active))
	add("pipeline.stalled", float64(stalled))
	if n > 0 {
		add("pipeline.stage_days", float64(stageDays)/float64(n))
	}
	return out
}

// companyOnlyReadings are the company's knowledge and brief readings, over every
// document and rating in the workspace. The retrieval hit rate is company-only
// because an eval run covers the whole knowledge base, not a squad. Floor and
// squad knowledge readings come from knowledgeReadings; a squad has no brief of
// its own, so brief.usefulness is written for the company and for floors only.
func (s *MetricService) companyOnlyReadings(ctx context.Context, q *db.Queries, ws pgtype.UUID, now, yesterday, today time.Time) ([]builtinReading, error) {
	company := builtinScope{scopeType: ScopeCompany}
	var out []builtinReading

	published, err := q.BuiltinKnowledgePublished(ctx, db.BuiltinKnowledgePublishedParams{
		WorkspaceID: ws,
		Since:       pgtype.Timestamptz{Time: yesterday, Valid: true},
		Until:       pgtype.Timestamptz{Time: today, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge published metric: %w", err)
	}
	out = append(out, builtinReading{key: "knowledge.docs_published", scope: company, value: float64(published.PublishedInWindow), observedAt: yesterday})

	if published.PublishedTotal > 0 {
		know, err := NewDigestService(q).computeKnowledge(ctx, ws, now, yesterday)
		if err != nil {
			return nil, fmt.Errorf("knowledge freshness metric: %w", err)
		}
		fresh := float64(published.PublishedTotal) - float64(know.StalePublished.Count)
		rate, _ := ratePercent(fresh, float64(published.PublishedTotal))
		out = append(out, builtinReading{key: "knowledge.fresh_ratio", scope: company, value: rate, observedAt: today})
	}

	runs, err := q.ListRetrievalHitRateRuns(ctx, db.ListRetrievalHitRateRunsParams{WorkspaceID: ws, RowLimit: 1})
	if err != nil {
		return nil, fmt.Errorf("retrieval metric: %w", err)
	}
	if len(runs) > 0 {
		out = append(out, builtinReading{key: "knowledge.retrieval_hit_rate", scope: company, value: runs[0].HitRate * 100, observedAt: today})
	}

	usefulness, err := q.BuiltinBriefUsefulness(ctx, db.BuiltinBriefUsefulnessParams{
		WorkspaceID: ws,
		Since:       pgtype.Timestamptz{Time: yesterday, Valid: true},
		Until:       pgtype.Timestamptz{Time: today, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("brief usefulness metric: %w", err)
	}
	if rate, ok := ratePercent(float64(usefulness.Useful), float64(usefulness.Total)); ok {
		out = append(out, builtinReading{key: "brief.usefulness", scope: company, value: rate, observedAt: yesterday})
	}
	return out, nil
}

// writeSystemReading replaces the system reading of one series at one time, so
// a rerun the same day leaves one row, and writes the new value.
func writeSystemReading(ctx context.Context, q *db.Queries, ws pgtype.UUID, r builtinReading) error {
	observed := pgtype.Timestamptz{Time: r.observedAt, Valid: true}
	if err := q.DeleteSystemMetricReadingsAt(ctx, db.DeleteSystemMetricReadingsAtParams{
		WorkspaceID: ws, MetricKey: r.key, ScopeType: r.scope.scopeType, ScopeID: r.scope.scopeID, ObservedAt: observed,
	}); err != nil {
		return fmt.Errorf("replace reading %s: %w", r.key, err)
	}
	value, err := numericFromFloat(r.value)
	if err != nil {
		return fmt.Errorf("reading %s value: %w", r.key, err)
	}
	if _, err := q.InsertMetricReading(ctx, db.InsertMetricReadingParams{
		ID: dbid.NewV7(), WorkspaceID: ws, MetricKey: r.key, ScopeType: r.scope.scopeType,
		ScopeID: r.scope.scopeID, Value: value, ObservedAt: observed, ActorType: ReadingActorSystem,
	}); err != nil {
		return fmt.Errorf("write reading %s: %w", r.key, err)
	}
	return nil
}

// ratePercent is numerator over denominator as a percentage. ok is false when
// there is nothing to divide by, so the caller writes no reading.
func ratePercent(numerator, denominator float64) (float64, bool) {
	if denominator <= 0 {
		return 0, false
	}
	return numerator / denominator * 100, true
}

// inScope reports whether a squad id belongs to the scope's squads.
func inScope(sc builtinScope, squadID pgtype.UUID) bool {
	if sc.squadIDs == nil {
		return true
	}
	for _, id := range sc.squadIDs {
		if id == squadID {
			return true
		}
	}
	return false
}

// inScopeSquad is inScope for an initiative's owning squad, given as the
// pipeline reader's string; an initiative with no owner squad belongs to the
// company only.
func inScopeSquad(sc builtinScope, owner *string) bool {
	if sc.squadIDs == nil {
		return true
	}
	if owner == nil {
		return false
	}
	for _, id := range sc.squadIDs {
		if util.UUIDToString(id) == *owner {
			return true
		}
	}
	return false
}
