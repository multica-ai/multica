package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Scale (spec 4.8): above a size the Secretary reads floor briefs instead of
// every squad. The company digest then carries each floor's latest brief and a
// flag for the floors that need the boss; the Secretary drills into a flagged
// floor with the floor-scoped digest.

const (
	// DigestModeAuto is the setting that picks the source by size.
	DigestModeAuto = "auto"
	// DigestModeSquads reads every squad.
	DigestModeSquads = "squads"
	// DigestModeFloors reads the floor briefs.
	DigestModeFloors = "floors"
	// DigestAutoFloorThreshold is the number of active squads above which auto
	// reads floor briefs. Exactly 30 squads reads squads; 31 reads floors.
	DigestAutoFloorThreshold = 30
	// DigestFloorBriefMaxAge is how old a floor brief may be and still count as
	// the floor's latest.
	DigestFloorBriefMaxAge = 36 * time.Hour
	// DigestFloorBriefBodyMax bounds a floor brief's body in the digest, in
	// characters. The brief itself may be longer; the digest is cut.
	DigestFloorBriefBodyMax = 4000
)

// ResolveDigestMode returns the source the digest reads. An explicit setting
// wins. Auto reads floor briefs when there are more than
// DigestAutoFloorThreshold active squads.
func ResolveDigestMode(setting string, activeSquads int) string {
	switch setting {
	case DigestModeSquads:
		return DigestModeSquads
	case DigestModeFloors:
		return DigestModeFloors
	}
	if activeSquads > DigestAutoFloorThreshold {
		return DigestModeFloors
	}
	return DigestModeSquads
}

// computeFloors builds one line per floor for a floors-mode digest. all is the
// company's squad rows, already read for the digest, so the flag costs nothing
// extra.
func (s *DigestService) computeFloors(ctx context.Context, workspaceID pgtype.UUID, now time.Time, all []DigestSquad) ([]DigestFloor, error) {
	floors, err := s.Queries.ListCompanyFloors(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("digest floors: %w", err)
	}
	chiefs, err := s.Queries.ListFloorChiefs(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("digest floor chiefs: %w", err)
	}
	briefs, err := s.Queries.ListLatestFloorBriefs(ctx, db.ListLatestFloorBriefsParams{
		WorkspaceID: workspaceID,
		Since:       pgtype.Timestamptz{Time: now.Add(-DigestFloorBriefMaxAge), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("digest floor briefs: %w", err)
	}
	chiefOf := make(map[string]string, len(chiefs))
	for _, c := range chiefs {
		chiefOf[util.UUIDToString(c.FloorID)] = util.UUIDToString(c.AgentID)
	}
	briefOf := make(map[string]db.Brief, len(briefs))
	for _, b := range briefs {
		briefOf[util.UUIDToString(b.ScopeID)] = b
	}
	type floorStats struct {
		squads  int
		flagged bool
	}
	stats := map[string]*floorStats{}
	for i := range all {
		if all[i].FloorID == nil {
			continue
		}
		st := stats[*all[i].FloorID]
		if st == nil {
			st = &floorStats{}
			stats[*all[i].FloorID] = st
		}
		st.squads++
		if !all[i].quiet() {
			st.flagged = true
		}
	}
	out := make([]DigestFloor, 0, len(floors))
	for _, f := range floors {
		id := util.UUIDToString(f.ID)
		line := DigestFloor{ID: id, Name: f.Name}
		if agent, ok := chiefOf[id]; ok {
			line.ChiefAgentID = &agent
		}
		if st := stats[id]; st != nil {
			line.SquadCount = st.squads
			line.Flagged = st.flagged
		}
		if b, ok := briefOf[id]; ok {
			line.Brief = &DigestFloorBrief{
				ID: util.UUIDToString(b.ID), Title: b.Title,
				CreatedAt: b.CreatedAt.Time.UTC().Format(time.RFC3339),
				Body:      clipDigestBody(b.Body, DigestFloorBriefBodyMax),
			}
		}
		out = append(out, line)
	}
	return out, nil
}

// systemRisks returns one risk line per stuck scheduled job (spec 4.9). A failure
// to read the registry is logged and reports no risk: the digest still answers.
func (s *DigestService) systemRisks(ctx context.Context, now time.Time) []string {
	statuses, err := ReadJobStatuses(ctx, s.Queries, now)
	if err != nil {
		slog.Warn("digest: job status registry unreadable", "error", err)
		return []string{}
	}
	out := []string{}
	for _, st := range StuckJobs(statuses) {
		out = append(out, fmt.Sprintf("Scheduled job %s is stuck: %s.", st.Label, st.StuckReason))
	}
	return out
}

// filterPipelineToSquads keeps the initiatives whose owning squad is in the set.
func filterPipelineToSquads(list []DigestInitiative, squads map[string]bool) []DigestInitiative {
	out := make([]DigestInitiative, 0, len(list))
	for _, ini := range list {
		if ini.OwnerSquadID != nil && squads[*ini.OwnerSquadID] {
			out = append(out, ini)
		}
	}
	return out
}

// filterOverdueToSquads keeps the overdue requests addressed to a squad in the set.
func filterOverdueToSquads(list []DigestOverdueRequest, squads map[string]bool) []DigestOverdueRequest {
	out := make([]DigestOverdueRequest, 0, len(list))
	for _, r := range list {
		if squads[r.SquadID] {
			out = append(out, r)
		}
	}
	return out
}

// clipDigestBody cuts s to at most max characters.
func clipDigestBody(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// floorRoutineRefs narrows the workspace's off-track routines to a floor. The
// list is capped company-wide, so the count comes from the floor's own squads
// and the items are the capped ones that belong to the floor.
func floorRoutineRefs(list DigestRoutineRefList, squads map[string]bool, count int) DigestRoutineRefList {
	out := DigestRoutineRefList{Count: count, Items: []DigestRoutineRef{}}
	for _, r := range list.Items {
		if squads[r.SquadID] {
			out.Items = append(out.Items, r)
		}
	}
	return out
}
