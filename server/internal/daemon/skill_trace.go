package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

const (
	SkillTraceEventInvoked = "skill_invoked"

	SkillTraceTriggerModel    = "model"
	SkillTraceTriggerExplicit = "explicit"

	SkillTraceObservedNativeTool = "native_skill_tool"
	SkillTraceObservedFileRead   = "skill_file_read"
)

type skillTraceMeta struct {
	Provider         string
	MachineID        string
	DeviceName       string
	DaemonProfile    string
	RuntimeProfileID string
}

// SkillTraceEvent is one append-only observation that the model selected a
// skill during a task. Merely mounting or advertising a skill is deliberately
// not an event: the stream measures behavior, not inventory.
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
	ObservedVia      string    `json:"observed_via"`
	ToolCallID       string    `json:"tool_call_id,omitempty"`
}

// SkillTraceRecorder appends one JSON object per observed invocation. It is
// local and opt-in because the records contain task and employee identifiers.
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

	// Marshal the whole batch before opening the file. Each append below is a
	// single Write call, so separate daemon profiles sharing a trace path cannot
	// interleave fragments of two JSON objects.
	lines := make([][]byte, len(events))
	for i, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal skill trace event: %w", err)
		}
		lines[i] = append(line, '\n')
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return fmt.Errorf("create skill trace directory: %w", err)
	}
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open skill trace file: %w", err)
	}
	defer f.Close()

	for _, line := range lines {
		if _, err := f.Write(line); err != nil {
			return fmt.Errorf("write skill trace event: %w", err)
		}
	}
	return nil
}

type skillTraceCandidate struct {
	skill    SkillData
	slug     string
	explicit bool
}

type skillInvocationTracker struct {
	recorder   *SkillTraceRecorder
	base       SkillTraceEvent
	candidates []skillTraceCandidate
	logger     *slog.Logger

	mu   sync.Mutex
	seen map[string]struct{}
}

func newSkillInvocationTracker(recorder *SkillTraceRecorder, task Task, skills []SkillData, meta skillTraceMeta, logger *slog.Logger) *skillInvocationTracker {
	if recorder == nil || !recorder.Enabled() || len(skills) == 0 {
		return nil
	}

	selected := explicitSkillSelections(task)

	slugs := execenv.ResolveSkillSlugs(convertSkillsForEnv(skills))
	candidates := make([]skillTraceCandidate, 0, len(skills))
	for i, skill := range skills {
		_, explicit := selected[skill.ID]
		candidates = append(candidates, skillTraceCandidate{
			skill:    skill,
			slug:     slugs[i],
			explicit: explicit,
		})
	}

	base := SkillTraceEvent{
		EventType:        SkillTraceEventInvoked,
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

	return &skillInvocationTracker{
		recorder:   recorder,
		base:       base,
		candidates: candidates,
		logger:     logger,
		seen:       make(map[string]struct{}),
	}
}

func (t *skillInvocationTracker) Observe(msg agent.Message) {
	if t == nil || msg.Type != agent.MessageToolUse {
		return
	}

	observedVia, matches := t.matches(msg)
	if len(matches) == 0 {
		return
	}

	events := make([]SkillTraceEvent, 0, len(matches))
	t.mu.Lock()
	for _, candidate := range matches {
		key := msg.CallID + "\x00" + candidate.skill.ID
		if msg.CallID != "" {
			if _, duplicate := t.seen[key]; duplicate {
				continue
			}
			t.seen[key] = struct{}{}
		}

		event := t.base
		event.SkillID = candidate.skill.ID
		event.SkillName = candidate.skill.Name
		event.SkillSource = candidate.skill.Source
		event.SkillHash = candidate.skill.Hash
		event.Trigger = SkillTraceTriggerModel
		if candidate.explicit {
			event.Trigger = SkillTraceTriggerExplicit
		}
		event.ObservedVia = observedVia
		event.ToolCallID = msg.CallID
		events = append(events, event)
	}
	t.mu.Unlock()

	if err := t.recorder.Record(events); err != nil && t.logger != nil {
		t.logger.Warn("skill invocation trace write failed (non-fatal)", "error", err)
	}
}

func (t *skillInvocationTracker) matches(msg agent.Message) (string, []skillTraceCandidate) {
	tool := normalizeSkillTraceToolName(msg.Tool)

	if tool == "skill" || tool == "skills_read" || tool == "skill_read" {
		matches := t.matchNativeSkillInputs(skillTraceNativeSkillIdentities(msg.Input))
		if len(matches) > 0 {
			return SkillTraceObservedNativeTool, matches
		}
	}

	if !skillTraceFileReadTool(tool) {
		return "", nil
	}
	inputs := skillTraceInputStrings(msg.Input)
	matches := make([]skillTraceCandidate, 0, 1)
	for _, candidate := range t.candidates {
		for _, input := range inputs {
			if inputMentionsSkillFile(input, candidate.slug) {
				matches = append(matches, candidate)
				break
			}
		}
	}
	if len(matches) == 0 {
		return "", nil
	}
	return SkillTraceObservedFileRead, matches
}

func skillTraceNativeSkillIdentities(input map[string]any) []string {
	identities := make([]string, 0, 2)
	for _, key := range []string{"skill", "skill_name", "name", "id"} {
		if value, ok := input[key].(string); ok {
			identities = append(identities, value)
		}
	}
	return identities
}

func (t *skillInvocationTracker) matchNativeSkillInputs(inputs []string) []skillTraceCandidate {
	matches := make([]skillTraceCandidate, 0, 1)
	for _, candidate := range t.candidates {
		for _, input := range inputs {
			identity := strings.ToLower(strings.Trim(strings.TrimSpace(input), "/"))
			if identity == strings.ToLower(candidate.skill.ID) ||
				identity == strings.ToLower(candidate.skill.Name) ||
				identity == strings.ToLower(candidate.slug) {
				matches = append(matches, candidate)
				break
			}
		}
	}
	return matches
}

func normalizeSkillTraceToolName(name string) string {
	return strings.Trim(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return '_'
	}, name), "_")
}

func skillTraceFileReadTool(tool string) bool {
	switch tool {
	case "exec", "exec_command", "functions_exec", "bash", "shell", "terminal", "run_shell_command",
		"read", "read_file", "readfile", "file_read", "open_file", "view_file":
		return true
	default:
		return false
	}
}

func skillTraceInputStrings(input map[string]any) []string {
	var values []string
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case string:
			values = append(values, v)
		case []any:
			for _, item := range v {
				visit(item)
			}
		case map[string]any:
			for _, item := range v {
				visit(item)
			}
		}
	}
	visit(input)
	return values
}

func inputMentionsSkillFile(input, slug string) bool {
	input = "/" + strings.ToLower(strings.ReplaceAll(input, `\`, "/"))
	slug = strings.ToLower(slug)
	for from := 0; ; {
		idx := strings.Index(input[from:], "/skill.md")
		if idx < 0 {
			return false
		}
		idx += from
		prefix := input[:idx]
		dirStart := strings.LastIndex(prefix, "/")
		if dirStart >= 0 {
			dir := prefix[dirStart+1:]
			underSkillsRoot := strings.Contains(prefix[:dirStart+1], "/skills/")
			if underSkillsRoot && dir == slug {
				return true
			}
		}
		from = idx + len("/skill.md")
	}
}

func explicitSkillSelections(task Task) map[string]struct{} {
	selected := make(map[string]struct{})
	inputs := []string{
		task.ChatMessage,
		task.TriggerCommentContent,
		task.QuickCreatePrompt,
		task.HandoffNote,
		task.AutopilotDescription,
	}
	for _, comment := range task.CoalescedComments {
		inputs = append(inputs, comment.Content)
	}
	for _, input := range inputs {
		for _, ref := range ExtractSlashSkills(input) {
			selected[ref.ID] = struct{}{}
		}
	}
	return selected
}

func populateSkillTraceEmployee(event *SkillTraceEvent, task Task) {
	if task.InitiatorName != "" || task.InitiatorID != "" || task.InitiatorType != "" {
		event.EmployeeID = task.InitiatorID
		event.EmployeeName = task.InitiatorName
		event.EmployeeType = task.InitiatorType
		return
	}
	if task.RequestingUserName != "" {
		event.EmployeeName = task.RequestingUserName
		event.EmployeeType = "runtime_owner"
	}
}

func (d *Daemon) skillTraceMetaForTask(task Task, provider string) skillTraceMeta {
	meta := skillTraceMeta{
		Provider:      provider,
		MachineID:     d.cfg.DaemonID,
		DeviceName:    d.cfg.DeviceName,
		DaemonProfile: d.cfg.Profile,
	}
	if runtime := d.findRuntime(task.RuntimeID); runtime != nil {
		meta.RuntimeProfileID = runtime.ProfileID
	}
	return meta
}

func (d *Daemon) trackSkillInvocations(task Task, skills []SkillData, provider string, logger *slog.Logger) func() {
	if d.skillTrace == nil || !d.skillTrace.Enabled() {
		return func() {}
	}
	tracker := newSkillInvocationTracker(d.skillTrace, task, skills, d.skillTraceMetaForTask(task, provider), logger)
	if tracker == nil {
		return func() {}
	}
	d.skillTraceTasks.Store(task.ID, tracker)
	return func() {
		d.skillTraceTasks.CompareAndDelete(task.ID, tracker)
	}
}

func (d *Daemon) observeSkillTraceMessage(taskID string, msg agent.Message) {
	tracker, ok := d.skillTraceTasks.Load(taskID)
	if !ok {
		return
	}
	tracker.(*skillInvocationTracker).Observe(msg)
}
