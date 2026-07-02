package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	SkillTraceEventLoaded  = "skill_loaded"
	SkillTraceEventInvoked = "skill_invoked"

	SkillTraceTriggerImplicit = "implicit"
	SkillTraceTriggerExplicit = "explicit"
)

type skillTraceMeta struct {
	Provider         string
	MachineID        string
	DeviceName       string
	DaemonProfile    string
	RuntimeProfileID string
}

// SkillTraceEvent is one append-only skill behavior record. The stream is
// intentionally local and gated; external curator jobs can consume the JSONL
// without coupling skill adoption measurement to token billing.
type SkillTraceEvent struct {
	EventType        string    `json:"event_type"`
	TS               time.Time `json:"ts"`
	WorkspaceID      string    `json:"workspace_id,omitempty"`
	TaskID           string    `json:"task_id,omitempty"`
	IssueID          string    `json:"issue_id,omitempty"`
	ChatSessionID    string    `json:"chat_session_id,omitempty"`
	AutopilotRunID   string    `json:"autopilot_run_id,omitempty"`
	AgentID          string    `json:"agent_id,omitempty"`
	AgentName        string    `json:"agent_name,omitempty"`
	RuntimeID        string    `json:"runtime_id,omitempty"`
	Provider         string    `json:"provider,omitempty"`
	RuntimeProfileID string    `json:"runtime_profile_id,omitempty"`
	DaemonProfile    string    `json:"daemon_profile,omitempty"`
	MachineID        string    `json:"machine_id,omitempty"`
	DeviceName       string    `json:"device_name,omitempty"`
	EmployeeID       string    `json:"employee_id,omitempty"`
	EmployeeName     string    `json:"employee_name,omitempty"`
	EmployeeType     string    `json:"employee_type,omitempty"`
	SkillID          string    `json:"skill_id,omitempty"`
	SkillName        string    `json:"skill_name,omitempty"`
	SkillSource      string    `json:"skill_source,omitempty"`
	SkillHash        string    `json:"skill_hash,omitempty"`
	Trigger          string    `json:"trigger"`
}

type SkillTraceRecorder struct {
	enabled bool
	path    string
	mu      sync.Mutex
}

func NewSkillTraceRecorder(cfg Config) *SkillTraceRecorder {
	return &SkillTraceRecorder{
		enabled: cfg.SkillTraceEnabled,
		path:    cfg.SkillTracePath,
	}
}

func (r *SkillTraceRecorder) Enabled() bool {
	return r != nil && r.enabled
}

func (r *SkillTraceRecorder) Record(events []SkillTraceEvent) error {
	if !r.Enabled() || len(events) == 0 {
		return nil
	}
	if r.path == "" {
		return fmt.Errorf("skill trace path is empty")
	}

	now := time.Now().UTC()
	for i := range events {
		if events[i].TS.IsZero() {
			events[i].TS = now
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return fmt.Errorf("create skill trace directory: %w", err)
	}
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open skill trace file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			return fmt.Errorf("write skill trace event: %w", err)
		}
	}
	return nil
}

func buildSkillTraceEvents(task Task, skills []SkillData, meta skillTraceMeta) []SkillTraceEvent {
	if len(skills) == 0 {
		return nil
	}

	base := SkillTraceEvent{
		WorkspaceID:      task.WorkspaceID,
		TaskID:           task.ID,
		IssueID:          task.IssueID,
		ChatSessionID:    task.ChatSessionID,
		AutopilotRunID:   task.AutopilotRunID,
		RuntimeID:        task.RuntimeID,
		Provider:         meta.Provider,
		RuntimeProfileID: meta.RuntimeProfileID,
		DaemonProfile:    meta.DaemonProfile,
		MachineID:        meta.MachineID,
		DeviceName:       meta.DeviceName,
	}
	if task.Agent != nil {
		base.AgentID = task.Agent.ID
		base.AgentName = task.Agent.Name
	}
	populateSkillTraceEmployee(&base, task)

	events := make([]SkillTraceEvent, 0, len(skills))
	byID := make(map[string]SkillData, len(skills))
	for _, skill := range skills {
		byID[skill.ID] = skill
		events = append(events, skillTraceEventForSkill(base, skill, SkillTraceEventLoaded, SkillTraceTriggerImplicit))
	}

	for _, ref := range ExtractSlashSkills(task.ChatMessage) {
		skill, ok := byID[ref.ID]
		if !ok {
			continue
		}
		events = append(events, skillTraceEventForSkill(base, skill, SkillTraceEventInvoked, SkillTraceTriggerExplicit))
	}

	return events
}

func skillTraceEventForSkill(base SkillTraceEvent, skill SkillData, eventType, trigger string) SkillTraceEvent {
	ev := base
	ev.EventType = eventType
	ev.SkillID = skill.ID
	ev.SkillName = skill.Name
	ev.SkillSource = skill.Source
	ev.SkillHash = skill.Hash
	ev.Trigger = trigger
	return ev
}

func populateSkillTraceEmployee(ev *SkillTraceEvent, task Task) {
	if task.InitiatorName != "" || task.InitiatorID != "" || task.InitiatorType != "" {
		ev.EmployeeID = task.InitiatorID
		ev.EmployeeName = task.InitiatorName
		ev.EmployeeType = task.InitiatorType
		return
	}
	if task.RequestingUserName != "" {
		ev.EmployeeName = task.RequestingUserName
		ev.EmployeeType = "runtime_owner"
	}
}
