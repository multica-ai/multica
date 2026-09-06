package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type secretaryCase struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Area    string `json:"area"`
	Summary string `json:"summary"`
}

type secretaryItem struct {
	ClosureMode     string  `json:"closure_mode"`
	ReportedStatus  string  `json:"reported_status"`
	ActionID        string  `json:"action_id"`
	BusinessStatus  string  `json:"business_status"`
	Key             string  `json:"key"`
	IssueID         *string `json:"issue_id"`
	CaseID          string  `json:"case_id"`
	Kind            string  `json:"kind"`
	Stage           string  `json:"stage"`
	Owner           string  `json:"owner"`
	Title           string  `json:"title"`
	Situation       string  `json:"situation"`
	NextStep        string  `json:"next_step"`
	Recommendation  string  `json:"recommendation"`
	WhyNow          string  `json:"why_now"`
	Completion      string  `json:"completion"`
	DueDate         *string `json:"due_date"`
	FollowUpOn      *string `json:"follow_up_on"`
	FollowUpTrigger string  `json:"follow_up_trigger"`
}

type secretaryProjection struct {
	Version      int             `json:"version"`
	StateVersion int             `json:"state_version"`
	SourceAsOf   string          `json:"source_as_of"`
	Cases        []secretaryCase `json:"cases"`
	Items        []secretaryItem `json:"items"`
}

func validSecretaryDate(value *string) bool {
	if value == nil {
		return true
	}
	_, err := time.Parse("2006-01-02", *value)
	return err == nil
}

func validateSecretaryProjection(raw json.RawMessage) (secretaryProjection, error) {
	var p secretaryProjection
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, err
	}
	if p.Version != 1 || p.StateVersion < 1 || p.Items == nil || p.Cases == nil {
		return p, fmt.Errorf("invalid projection envelope")
	}
	if _, err := time.Parse(time.RFC3339, p.SourceAsOf); err != nil {
		return p, fmt.Errorf("invalid source date")
	}
	cases, keys, issueIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, c := range p.Cases {
		if c.ID == "" || strings.TrimSpace(c.Title) == "" || cases[c.ID] {
			return p, fmt.Errorf("invalid matter")
		}
		cases[c.ID] = true
	}
	for _, item := range p.Items {
		if item.Key == "" || keys[item.Key] || !cases[item.CaseID] || strings.TrimSpace(item.Title) == "" || strings.TrimSpace(item.Situation) == "" || strings.TrimSpace(item.NextStep) == "" {
			return p, fmt.Errorf("invalid or duplicate item")
		}
		keys[item.Key] = true
		if item.ClosureMode != "" && item.ClosureMode != "self_report" && item.ClosureMode != "verified_result" {
			return p, fmt.Errorf("invalid closure mode")
		}
		if item.ClosureMode == "self_report" && (item.ActionID == "" || item.Kind != "action") {
			return p, fmt.Errorf("personal completion requires a canonical action")
		}
		if item.IssueID != nil {
			if issueIDs[*item.IssueID] {
				return p, fmt.Errorf("duplicate source issue")
			}
			issueIDs[*item.IssueID] = true
		}
		switch item.Kind {
		case "action", "reference", "operation":
		default:
			return p, fmt.Errorf("invalid kind")
		}
		switch item.Stage {
		case "ready", "preparing", "waiting", "later", "history":
		default:
			return p, fmt.Errorf("invalid stage")
		}
		switch item.Owner {
		case "chairman", "secretary", "external":
		default:
			return p, fmt.Errorf("invalid owner")
		}
		if !validSecretaryDate(item.DueDate) || !validSecretaryDate(item.FollowUpOn) {
			return p, fmt.Errorf("invalid item date")
		}
		if item.Stage == "ready" && (item.Kind != "action" || item.Owner != "chairman" || item.Recommendation == "" || item.WhyNow == "" || item.Completion == "") {
			return p, fmt.Errorf("personal action is not prepared")
		}
	}
	return p, nil
}

type secretaryInstructionRequest struct {
	RequestID        string  `json:"request_id"`
	ItemKey          string  `json:"item_key"`
	Kind             string  `json:"kind"`
	ExpectedRevision int64   `json:"expected_revision"`
	ScheduledOn      *string `json:"scheduled_on"`
	EstimateMinutes  *int    `json:"estimate_minutes"`
	CapacityMinutes  *int    `json:"capacity_minutes"`
	Note             string  `json:"note"`
}

func validateSecretaryInstruction(req secretaryInstructionRequest) error {
	if !validSecretaryDate(req.ScheduledOn) || len(req.Note) > 4000 || len(req.ItemKey) > 160 {
		return fmt.Errorf("invalid instruction")
	}
	if req.EstimateMinutes != nil && (*req.EstimateMinutes < 1 || *req.EstimateMinutes > 1440) {
		return fmt.Errorf("invalid estimate")
	}
	switch req.Kind {
	case "plan":
	case "complete", "cancel", "prepare", "wait", "later":
		if strings.TrimSpace(req.Note) == "" {
			return fmt.Errorf("please describe the result or next follow-up")
		}
		if (req.Kind == "later" || req.Kind == "wait") && req.ScheduledOn == nil {
			return fmt.Errorf("follow-up date required")
		}
	case "capacity":
		if req.ItemKey != "day" || req.ScheduledOn == nil || req.CapacityMinutes == nil || *req.CapacityMinutes < 0 || *req.CapacityMinutes > 1440 {
			return fmt.Errorf("invalid daily capacity")
		}
	default:
		return fmt.Errorf("invalid instruction kind")
	}
	return nil
}
