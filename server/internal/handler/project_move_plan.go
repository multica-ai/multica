package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ProjectMoveBlocker struct {
	Kind       string  `json:"kind"`
	IssueID    *string `json:"issue_id"`
	ResourceID string  `json:"resource_id"`
	Identifier string  `json:"identifier,omitempty"`
}

type ProjectMoveLabel struct {
	SourceID    string `json:"source_id"`
	TargetID    string `json:"target_id,omitempty"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
	Action      string `json:"action"`
}

type ProjectMoveProperty struct {
	SourceID       string   `json:"source_id"`
	TargetID       string   `json:"target_id,omitempty"`
	Name           string   `json:"name"`
	Action         string   `json:"action"`
	DroppedOptions []string `json:"dropped_options"`
}

type ProjectMoveIssue struct {
	ID              string                     `json:"id"`
	Identifier      string                     `json:"identifier"`
	Number          int32                      `json:"number"`
	TargetNumber    int32                      `json:"target_number"`
	Status          string                     `json:"status"`
	TargetStatus    string                     `json:"target_status"`
	AssigneeCleared bool                       `json:"assignee_cleared"`
	Properties      map[string]json.RawMessage `json:"properties"`
	PropertyMapping []ProjectMoveProperty      `json:"property_mapping"`
}

type ProjectMovePlan struct {
	ProjectID          string               `json:"project_id"`
	SourceWorkspaceID  string               `json:"source_workspace_id"`
	TargetWorkspaceID  string               `json:"target_workspace_id"`
	Counts             map[string]int       `json:"counts"`
	Blockers           []ProjectMoveBlocker `json:"blockers"`
	Labels             []ProjectMoveLabel   `json:"labels"`
	Issues             []ProjectMoveIssue   `json:"issues"`
	LeadCleared        bool                 `json:"lead_cleared"`
	SubscribersCleared int                  `json:"subscribers_cleared"`
	SideEffects        map[string]int       `json:"side_effects"`
	VisibilityDelta    map[string]int       `json:"visibility_delta"`
	RevisionHash       string               `json:"revision_hash"`
	PlanHash           string               `json:"plan_hash,omitempty"`
	// Private execution state never appears in the API response.
	freshNumbers  int32
	sourcePrefix  string
	targetCounter int32
}

type moveWorkspace struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	IssuePrefix  string `json:"issue_prefix"`
	IssueCounter int32  `json:"issue_counter"`
}
type moveStoredIssue struct {
	ID           string                     `json:"id"`
	Number       int32                      `json:"number"`
	Status       string                     `json:"status"`
	Properties   map[string]json.RawMessage `json:"properties"`
	AssigneeType *string                    `json:"assignee_type"`
	AssigneeID   *string                    `json:"assignee_id"`
}
type moveStoredProperty struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	ArchivedAt  *string `json:"archived_at"`
	Config      struct {
		Options []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"options"`
	} `json:"config"`
}
type moveStoredStatus struct {
	WorkspaceID string  `json:"workspace_id"`
	Key         string  `json:"key"`
	Category    string  `json:"category"`
	ArchivedAt  *string `json:"archived_at"`
}
type moveState struct {
	Project struct {
		ID       string  `json:"id"`
		LeadType *string `json:"lead_type"`
		LeadID   *string `json:"lead_id"`
	} `json:"project"`
	Source  moveWorkspace     `json:"source"`
	Target  moveWorkspace     `json:"target"`
	Issues  []moveStoredIssue `json:"issues"`
	Members []struct {
		WorkspaceID string `json:"workspace_id"`
		UserID      string `json:"user_id"`
	} `json:"members"`
	Statuses     []moveStoredStatus   `json:"statuses"`
	Properties   []moveStoredProperty `json:"properties"`
	Labels       []ProjectMoveLabel   `json:"labels"`
	TargetLabels []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"target_labels"`
	Aliases []struct {
		WorkspaceID string `json:"source_workspace_id"`
		Prefix      string `json:"prefix"`
		Number      int32  `json:"number"`
		IssueID     string `json:"issue_id"`
	} `json:"aliases"`
	TargetNumbers []int32              `json:"target_numbers"`
	Blockers      []ProjectMoveBlocker `json:"blockers"`
	Subscribers   []struct {
		UserType string `json:"user_type"`
		UserID   string `json:"user_id"`
	} `json:"subscribers"`
}

func buildProjectMovePlan(ctx context.Context, q *db.Queries, project db.Project, target string) (ProjectMovePlan, error) {
	rawText, err := q.GetProjectMoveState(ctx, db.GetProjectMoveStateParams{ProjectID: project.ID, SourceWorkspaceID: project.WorkspaceID, TargetWorkspaceID: parseUUID(target)})
	if err != nil {
		return ProjectMovePlan{}, err
	}
	raw := []byte(rawText)
	var state moveState
	if err := json.Unmarshal(raw, &state); err != nil {
		return ProjectMovePlan{}, err
	}
	if state.Project.ID == "" || state.Source.ID == "" || state.Target.ID == "" {
		return ProjectMovePlan{}, fmt.Errorf("project or workspace no longer exists")
	}
	target = state.Target.ID
	var rows map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return ProjectMovePlan{}, err
	}
	// encoding/json sorts object keys; SQL sorts every array. Hash the complete
	// snapshot as well as the displayed plan, including issue revisions.
	canonical, err := json.Marshal(rows)
	if err != nil {
		return ProjectMovePlan{}, err
	}
	digest := sha256.Sum256(canonical)
	sourceMembers, targetMembers := map[string]bool{}, map[string]bool{}
	for _, m := range state.Members {
		if m.WorkspaceID == state.Source.ID {
			sourceMembers[m.UserID] = true
		} else {
			targetMembers[m.UserID] = true
		}
	}
	plan := ProjectMovePlan{ProjectID: state.Project.ID, SourceWorkspaceID: state.Source.ID, TargetWorkspaceID: state.Target.ID, Counts: map[string]int{}, Blockers: state.Blockers, Labels: []ProjectMoveLabel{}, Issues: []ProjectMoveIssue{}, SideEffects: map[string]int{}, VisibilityDelta: map[string]int{"lose_access": 0, "gain_access": 0}, RevisionHash: hex.EncodeToString(digest[:]), sourcePrefix: state.Source.IssuePrefix, targetCounter: state.Target.IssueCounter}
	if plan.sourcePrefix == "" {
		plan.sourcePrefix = legacyIssuePrefixFromName(state.Source.Name)
	}
	if plan.Blockers == nil {
		plan.Blockers = []ProjectMoveBlocker{}
	}
	for id := range sourceMembers {
		if !targetMembers[id] {
			plan.VisibilityDelta["lose_access"]++
		}
	}
	for id := range targetMembers {
		if !sourceMembers[id] {
			plan.VisibilityDelta["gain_access"]++
		}
	}
	for _, key := range []string{"issues", "comments", "attachments", "runs"} {
		var a []json.RawMessage
		_ = json.Unmarshal(rows[key], &a)
		plan.Counts[key] = len(a)
	}
	for _, key := range []string{"pins", "views", "view_preferences", "inbox_items", "unread_marks", "chat_contexts", "wakeups", "wakeup_receipts", "pr_links", "vcs_pr_links"} {
		var a []json.RawMessage
		_ = json.Unmarshal(rows[key], &a)
		plan.SideEffects[key] = len(a)
	}
	needsClear := func(kind, id *string) bool {
		return id != nil && (kind == nil || *kind != "member" || !targetMembers[*id])
	}
	plan.LeadCleared = needsClear(state.Project.LeadType, state.Project.LeadID)
	for _, s := range state.Subscribers {
		if s.UserType != "member" || !targetMembers[s.UserID] {
			plan.SubscribersCleared++
		}
	}
	// Source label snapshots include id rather than source_id.
	var labels []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Color       string `json:"color"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(rows["labels"], &labels); err != nil {
		return plan, err
	}
	for _, l := range labels {
		mapped := ProjectMoveLabel{SourceID: l.ID, Name: l.Name, Color: l.Color, Description: l.Description, Action: "create"}
		for _, t := range state.TargetLabels {
			if strings.EqualFold(t.Name, l.Name) {
				mapped.TargetID = t.ID
				mapped.Action = "reuse"
				break
			}
		}
		plan.Labels = append(plan.Labels, mapped)
	}
	sort.Slice(plan.Labels, func(i, j int) bool { return plan.Labels[i].SourceID < plan.Labels[j].SourceID })
	occupied := map[int32]bool{}
	for _, n := range state.TargetNumbers {
		occupied[n] = true
	}
	sort.Slice(state.Issues, func(i, j int) bool { return state.Issues[i].Number < state.Issues[j].Number })
	for _, issue := range state.Issues {
		item := ProjectMoveIssue{ID: issue.ID, Identifier: fmt.Sprintf("%s-%d", plan.sourcePrefix, issue.Number), Number: issue.Number, Status: issue.Status, TargetStatus: issue.Status, AssigneeCleared: needsClear(issue.AssigneeType, issue.AssigneeID), Properties: map[string]json.RawMessage{}, PropertyMapping: []ProjectMoveProperty{}}
		// A return move consumes the alias it restores; occupied numbers never
		// overwrite a live target issue, even if legacy data broke the invariant.
		for _, alias := range state.Aliases {
			if alias.WorkspaceID == target && alias.IssueID == issue.ID && !occupied[alias.Number] {
				item.TargetNumber = alias.Number
				break
			}
		}
		if item.TargetNumber == 0 {
			plan.freshNumbers++
			item.TargetNumber = state.Target.IssueCounter + plan.freshNumbers
		}
		occupied[item.TargetNumber] = true
		kept := issuestatus.IsBuiltIn(issue.Status)
		category := "unstarted"
		for _, s := range state.Statuses {
			if s.WorkspaceID == target && s.Key == issue.Status && s.ArchivedAt == nil {
				kept = true
			}
			if s.WorkspaceID == state.Source.ID && s.Key == issue.Status {
				category = s.Category
			}
		}
		if !kept {
			switch category {
			case "started":
				item.TargetStatus = "in_progress"
			case "done":
				item.TargetStatus = "done"
			case "closed":
				item.TargetStatus = "cancelled"
			default:
				item.TargetStatus = "todo"
			}
		}
		keys := make([]string, 0, len(issue.Properties))
		for id := range issue.Properties {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		for _, id := range keys {
			change := ProjectMoveProperty{SourceID: id, Action: "drop", DroppedOptions: []string{}}
			var sourceDef *moveStoredProperty
			for i := range state.Properties {
				if state.Properties[i].ID == id && state.Properties[i].WorkspaceID == state.Source.ID {
					sourceDef = &state.Properties[i]
					break
				}
			}
			if sourceDef != nil {
				change.Name = sourceDef.Name
				for _, def := range state.Properties {
					if def.WorkspaceID == target && def.ArchivedAt == nil && def.Name == sourceDef.Name && def.Type == sourceDef.Type {
						value, dropped, ok := mapMoveProperty(*sourceDef, def, issue.Properties[id], targetMembers)
						change.TargetID = def.ID
						change.DroppedOptions = dropped
						if ok {
							item.Properties[def.ID] = value
							change.Action = "carry"
						}
						break
					}
				}
			}
			item.PropertyMapping = append(item.PropertyMapping, change)
		}
		plan.Issues = append(plan.Issues, item)
	}
	for i := range plan.Blockers {
		if plan.Blockers[i].IssueID != nil {
			for _, issue := range plan.Issues {
				if issue.ID == *plan.Blockers[i].IssueID {
					plan.Blockers[i].Identifier = issue.Identifier
				}
			}
		}
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return plan, err
	}
	hash := sha256.Sum256(encoded)
	plan.PlanHash = hex.EncodeToString(hash[:])
	return plan, nil
}

func mapMoveProperty(source, target moveStoredProperty, value json.RawMessage, members map[string]bool) (json.RawMessage, []string, bool) {
	dropped := []string{}
	switch source.Type {
	case "select", "multi_select":
		options := map[string]string{}
		for _, s := range source.Config.Options {
			for _, t := range target.Config.Options {
				if s.Name == t.Name {
					options[s.ID] = t.ID
					break
				}
			}
		}
		if source.Type == "select" {
			var id string
			if json.Unmarshal(value, &id) != nil {
				return nil, dropped, false
			}
			if mapped, ok := options[id]; ok {
				v, _ := json.Marshal(mapped)
				return v, dropped, true
			}
			return nil, []string{id}, false
		}
		var ids []string
		if json.Unmarshal(value, &ids) != nil {
			return nil, dropped, false
		}
		mapped := []string{}
		for _, id := range ids {
			if v, ok := options[id]; ok {
				mapped = append(mapped, v)
			} else {
				dropped = append(dropped, id)
			}
		}
		v, _ := json.Marshal(mapped)
		return v, dropped, len(mapped) > 0
	case "actor", "multi_actor":
		valid := func(ref string) bool {
			kind, id, ok := strings.Cut(ref, ":")
			return ok && kind == "member" && members[id]
		}
		if source.Type == "actor" {
			var ref string
			if json.Unmarshal(value, &ref) != nil || !valid(ref) {
				return nil, dropped, false
			}
			return value, dropped, true
		}
		var refs []string
		if json.Unmarshal(value, &refs) != nil {
			return nil, dropped, false
		}
		kept := []string{}
		for _, ref := range refs {
			if valid(ref) {
				kept = append(kept, ref)
			} else {
				dropped = append(dropped, ref)
			}
		}
		v, _ := json.Marshal(kept)
		return v, dropped, len(kept) > 0
	default:
		return value, dropped, true
	}
}
