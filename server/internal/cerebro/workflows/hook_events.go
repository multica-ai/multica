package workflows

const (
	HookBeforeSessionStart   HookEventType = "before.session.start"
	HookAfterSessionStart    HookEventType = "after.session.start"
	HookBeforeSessionEnd     HookEventType = "before.session.end"
	HookAfterSessionEnd      HookEventType = "after.session.end"
	HookBeforePromptAssemble HookEventType = "before.prompt.assemble"
	HookBeforeToolCall       HookEventType = "before.tool.call"
	HookAfterToolCall        HookEventType = "after.tool.call"
	HookOnToolFailure        HookEventType = "on.tool.failure"
	HookBeforeTaskComplete   HookEventType = "before.task.complete"
	HookBeforeAgentStop      HookEventType = "before.agent.stop"
	HookBeforeSubagentStart  HookEventType = "before.subagent.start"
	HookAfterSubagentStop    HookEventType = "after.subagent.stop"
	HookOnError              HookEventType = "on.error"
	HookOnTaskFailure        HookEventType = "on.task.failure"
	HookBeforeWakeupCreate   HookEventType = "before.wakeup.create"
	HookOnWakeupFireFailure  HookEventType = "on.wakeup.fire_failure"
	HookBeforeIssueStatus    HookEventType = "before.issue.status_change"
	// HookBeforeIssueAssigned fires when an issue is handed to an agent and a
	// task is about to be enqueued for it (FIR-4183). It is the seam where the
	// platform can look for an issue on the same matter before work starts.
	HookBeforeIssueAssigned HookEventType = "before.issue.assigned"
	HookBeforeMessageSend    HookEventType = "before.message.send"
	HookAfterWorkflowStep    HookEventType = "after.workflow.step_completed"
)

type HookEventDefinition struct {
	Label     string `json:"label"`
	Phase     string `json:"phase"`
	CanBlock  bool   `json:"can_block"`
	CanModify bool   `json:"can_modify"`
	Advanced  bool   `json:"advanced"`
}

var HookEventCatalog = map[HookEventType]HookEventDefinition{
	HookBeforeSessionStart:   {Label: "Before session starts", Phase: "before", CanBlock: true, Advanced: true},
	HookAfterSessionStart:    {Label: "After session starts", Phase: "after", Advanced: true},
	HookBeforeSessionEnd:     {Label: "Before session ends", Phase: "before", CanBlock: true, Advanced: true},
	HookAfterSessionEnd:      {Label: "After session ends", Phase: "after", Advanced: true},
	HookBeforePromptAssemble: {Label: "Before prompt is assembled", Phase: "before", CanBlock: true, CanModify: true, Advanced: true},
	HookBeforeToolCall:       {Label: "Before tool call", Phase: "before", CanBlock: true, CanModify: true, Advanced: true},
	HookAfterToolCall:        {Label: "After tool call", Phase: "after", Advanced: true},
	HookOnToolFailure:        {Label: "When a tool fails", Phase: "failure", CanBlock: true, CanModify: true, Advanced: true},
	HookBeforeTaskComplete:   {Label: "Before task completes", Phase: "before", CanBlock: true},
	HookBeforeAgentStop:      {Label: "Before agent stops", Phase: "before", CanBlock: true, Advanced: true},
	HookBeforeSubagentStart:  {Label: "Before subagent starts", Phase: "before", CanBlock: true, Advanced: true},
	HookAfterSubagentStop:    {Label: "After subagent stops", Phase: "after", Advanced: true},
	HookOnError:              {Label: "When an error occurs", Phase: "failure", Advanced: true},
	HookOnTaskFailure:        {Label: "When a task fails", Phase: "failure"},
	HookBeforeWakeupCreate:   {Label: "Before wakeup is created", Phase: "before", CanBlock: true},
	HookOnWakeupFireFailure:  {Label: "When a wakeup fails", Phase: "failure"},
	HookBeforeIssueStatus:    {Label: "Before issue status changes", Phase: "before", CanBlock: true},
	HookBeforeIssueAssigned:  {Label: "Before an issue is handed to an agent", Phase: "before", CanBlock: true},
	HookBeforeMessageSend:    {Label: "Before message is sent", Phase: "before", CanBlock: true, CanModify: true},
	HookAfterWorkflowStep:    {Label: "After workflow step completes", Phase: "after"},
}
