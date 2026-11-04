package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The org map says who the people are: floors and their chiefs, squads and their
// leaders, members and skills, each squad's project and workflow, and how much
// open work each squad holds. It is read-only and bounded, so a large company
// cannot make it expensive for the Secretary to read.
const (
	// OrgMapMaxSquads caps the squads returned. SquadsTruncated says when there were more.
	OrgMapMaxSquads = 200
	// OrgMapMaxMembersPerSquad caps the member list of each squad. MemberCount is the true total.
	OrgMapMaxMembersPerSquad = 50
)

// OrgMapAgent is an agent as the map shows it. RuntimeOnline is true only when
// the agent's runtime reports "online".
type OrgMapAgent struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Archived      bool   `json:"archived"`
	RuntimeStatus string `json:"runtime_status"`
	RuntimeOnline bool   `json:"runtime_online"`
}

// OrgMapFloor is a floor of the digital company with its chief, if one is seated.
type OrgMapFloor struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Position int32        `json:"position"`
	Chief    *OrgMapAgent `json:"chief"`
}

// OrgMapProject is the project a squad holds, with its workflow name.
type OrgMapProject struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	WorkflowID   *string `json:"workflow_id"`
	WorkflowName string  `json:"workflow_name"`
}

// OrgMapMember is one member of a squad. Type is "agent" or "member" (a human).
// Skills is set for agents only.
type OrgMapMember struct {
	Type     string   `json:"type"`
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Role     string   `json:"role"`
	IsLeader bool     `json:"is_leader"`
	Archived bool     `json:"archived"`
	Skills   []string `json:"skills"`
}

// OrgMapSquad is a live squad with its leader, charter, project, open work and members.
type OrgMapSquad struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	FloorID          *string        `json:"floor_id"`
	FloorName        string         `json:"floor_name"`
	Leader           *OrgMapAgent   `json:"leader"`
	Mission          string         `json:"mission"`
	Intake           string         `json:"intake"`
	ResponseHours    int            `json:"response_hours"`
	HasCharter       bool           `json:"has_charter"`
	Project          *OrgMapProject `json:"project"`
	OpenIssues       int64          `json:"open_issues"`
	MemberCount      int64          `json:"member_count"`
	Members          []OrgMapMember `json:"members"`
	MembersTruncated bool           `json:"members_truncated"`
}

// OrgMap is the response of GET /api/company/org-map.
type OrgMap struct {
	Floors          []OrgMapFloor `json:"floors"`
	Squads          []OrgMapSquad `json:"squads"`
	SquadLimit      int           `json:"squad_limit"`
	SquadsTruncated bool          `json:"squads_truncated"`
}

// CapRows keeps at most max rows. The caller fetches max+1 rows, so a longer
// input means the list was cut short.
func CapRows[T any](rows []T, max int) ([]T, bool) {
	if len(rows) > max {
		return rows[:max], true
	}
	return rows, false
}

// CapOrgMapMembers keeps the first max members (leader first, as the query orders
// them) and reports whether any were dropped.
func CapOrgMapMembers(members []OrgMapMember, max int) ([]OrgMapMember, bool) {
	return CapRows(members, max)
}

// OrgRuntimeOnline reports whether a runtime status counts as online.
func OrgRuntimeOnline(status string) bool {
	return status == "online"
}

// LoadOrgMap reads the workspace's org map. floorID, when valid, narrows the
// floors and squads to one floor. Every query is scoped to workspaceID.
func LoadOrgMap(ctx context.Context, q *db.Queries, workspaceID, floorID pgtype.UUID) (OrgMap, error) {
	floors, err := q.ListCompanyFloors(ctx, workspaceID)
	if err != nil {
		return OrgMap{}, fmt.Errorf("list floors: %w", err)
	}
	chiefRows, err := q.ListOrgMapFloorChiefs(ctx, workspaceID)
	if err != nil {
		return OrgMap{}, fmt.Errorf("list floor chiefs: %w", err)
	}
	chiefs := make(map[string]*OrgMapAgent, len(chiefRows))
	for _, c := range chiefRows {
		chiefs[util.UUIDToString(c.FloorID)] = agentFromFields(c.AgentID, c.AgentName, c.AgentArchived, c.RuntimeStatus)
	}

	out := OrgMap{Floors: []OrgMapFloor{}, Squads: []OrgMapSquad{}, SquadLimit: OrgMapMaxSquads}
	floorFilter := floorID.Valid
	for _, f := range floors {
		id := util.UUIDToString(f.ID)
		if floorFilter && id != util.UUIDToString(floorID) {
			continue
		}
		out.Floors = append(out.Floors, OrgMapFloor{ID: id, Name: f.Name, Position: f.Position, Chief: chiefs[id]})
	}

	squadRows, err := q.ListOrgMapSquads(ctx, db.ListOrgMapSquadsParams{
		WorkspaceID: workspaceID,
		FloorID:     floorID,
		RowLimit:    int32(OrgMapMaxSquads + 1),
	})
	if err != nil {
		return OrgMap{}, fmt.Errorf("list squads: %w", err)
	}
	squadRows, out.SquadsTruncated = CapRows(squadRows, OrgMapMaxSquads)

	squadIDs := make([]pgtype.UUID, 0, len(squadRows))
	for _, s := range squadRows {
		squadIDs = append(squadIDs, s.SquadID)
	}
	memberRows := []db.ListOrgMapSquadMembersRow{}
	skillsByAgent := map[string][]string{}
	if len(squadIDs) > 0 {
		memberRows, err = q.ListOrgMapSquadMembers(ctx, db.ListOrgMapSquadMembersParams{
			WorkspaceID: workspaceID,
			SquadIds:    squadIDs,
		})
		if err != nil {
			return OrgMap{}, fmt.Errorf("list squad members: %w", err)
		}
		agentIDs := make([]pgtype.UUID, 0)
		for _, m := range memberRows {
			if m.MemberType == "agent" {
				agentIDs = append(agentIDs, m.MemberID)
			}
		}
		if len(agentIDs) > 0 {
			skillRows, err := q.ListOrgMapAgentSkills(ctx, db.ListOrgMapAgentSkillsParams{
				WorkspaceID: workspaceID,
				AgentIds:    agentIDs,
			})
			if err != nil {
				return OrgMap{}, fmt.Errorf("list agent skills: %w", err)
			}
			for _, sk := range skillRows {
				key := util.UUIDToString(sk.AgentID)
				skillsByAgent[key] = append(skillsByAgent[key], sk.Name)
			}
		}
	}

	membersBySquad := map[string][]OrgMapMember{}
	for _, m := range memberRows {
		key := util.UUIDToString(m.SquadID)
		member := OrgMapMember{
			Type:     m.MemberType,
			ID:       util.UUIDToString(m.MemberID),
			Name:     m.Name,
			Role:     m.Role,
			IsLeader: m.IsLeader,
			Archived: m.Archived,
		}
		if m.MemberType == "agent" {
			member.Skills = skillsByAgent[member.ID]
			if member.Skills == nil {
				member.Skills = []string{}
			}
		}
		membersBySquad[key] = append(membersBySquad[key], member)
	}

	for _, s := range squadRows {
		id := util.UUIDToString(s.SquadID)
		members, truncated := CapOrgMapMembers(membersBySquad[id], OrgMapMaxMembersPerSquad)
		if members == nil {
			members = []OrgMapMember{}
		}
		squad := OrgMapSquad{
			ID:               id,
			Name:             s.Name,
			FloorID:          optionalUUID(s.FloorID),
			FloorName:        s.FloorName,
			Mission:          s.Mission,
			Intake:           s.Intake,
			ResponseHours:    int(s.ResponseHours),
			HasCharter:       s.HasCharter,
			OpenIssues:       s.OpenIssues,
			MemberCount:      s.MemberCount,
			Members:          members,
			MembersTruncated: truncated,
		}
		if s.LeaderID.Valid {
			squad.Leader = agentFromFields(s.LeaderID, s.LeaderName, s.LeaderArchived, s.LeaderRuntimeStatus)
		}
		if s.ProjectID.Valid {
			squad.Project = &OrgMapProject{
				ID:           util.UUIDToString(s.ProjectID),
				Title:        s.ProjectTitle,
				WorkflowID:   optionalUUID(s.WorkflowID),
				WorkflowName: s.WorkflowName,
			}
		}
		out.Squads = append(out.Squads, squad)
	}
	return out, nil
}

func agentFromFields(id pgtype.UUID, name string, archived bool, runtimeStatus string) *OrgMapAgent {
	return &OrgMapAgent{
		ID:            util.UUIDToString(id),
		Name:          name,
		Archived:      archived,
		RuntimeStatus: runtimeStatus,
		RuntimeOnline: OrgRuntimeOnline(runtimeStatus),
	}
}

func optionalUUID(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := util.UUIDToString(u)
	return &s
}
