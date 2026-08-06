package workflows

import (
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

const (
	ManagedRequirementMessageRecipient = "comment must mention a recipient — a person, another agent, or a squad. A bare issue link and mentioning yourself do not count; address someone else or schedule a wakeup."
	ManagedRequirementSubIssueOwner    = "Comments on sub-issues must not mention the user this task was started for directly — post on the parent issue instead."
	ManagedRequirementSubIssueAgent    = "Comments on sub-issues must mention the parent agent to keep them in the loop."
	ManagedRequirementSubIssueSplit    = "This task session already posted on the parent issue — do not split a single conversation across both parent and sub-issue."
	ManagedRequirementNoAction         = "The task recorded no_action; comments are not allowed for this task."
	ManagedRequirementNoPromise        = "Schedule a wakeup before promising that work will continue, or report only the result already delivered."
)

var managedHookNamespace = uuid.MustParse("d93e09ce-4b44-43ca-80fd-5fb548ef9248")

type managedHookPolicyDefinition struct {
	Key    string
	Policy HookPolicy
}

type managedTaskFailureRoute struct {
	Action       string
	FreshSession bool
	RetryLimit   int32
	UserMessage  string
}

func managedTaskFailureRoutes() map[string]managedTaskFailureRoute {
	return map[string]managedTaskFailureRoute{
		taskfailure.ReasonQueuedExpired.String():                    {Action: "surface"},
		taskfailure.ReasonRuntimeOffline.String():                   {Action: "retry"},
		taskfailure.ReasonRuntimeRecovery.String():                  {Action: "retry"},
		taskfailure.ReasonTimeout.String():                          {Action: "retry"},
		taskfailure.ReasonIterationLimit.String():                   {Action: "surface"},
		taskfailure.ReasonAgentBlocked.String():                     {Action: "surface"},
		taskfailure.ReasonAPIInvalidRequest.String():                {Action: "surface"},
		taskfailure.ReasonAgentProviderAuthOrAccess.String():        {Action: "pause"},
		taskfailure.ReasonAgentProviderQuotaLimit.String():          {Action: "pause"},
		taskfailure.ReasonAgentProviderCapacityOrRateLimit.String(): {Action: "pause"},
		taskfailure.ReasonAgentProviderServerError.String():         {Action: "retry"},
		taskfailure.ReasonAgentProviderNetwork.String():             {Action: "retry"},
		taskfailure.ReasonAgentProcessFailure.String():              {Action: "retry", RetryLimit: 1},
		taskfailure.ReasonAgentEmptyOrUnparseableOutput.String():    {Action: "retry", FreshSession: true, RetryLimit: 1},
		taskfailure.ReasonAgentTimeout.String():                     {Action: "surface"},
		taskfailure.ReasonAgentContextOverflow.String():             {Action: "retry", FreshSession: true, RetryLimit: 1},
		taskfailure.ReasonAgentMissingConfig.String():               {Action: "surface"},
		taskfailure.ReasonAgentModelNotFoundOrUnavailable.String():  {Action: "surface"},
		taskfailure.ReasonAgentRuntimeVersionUnsupported.String(): {
			Action:      "alert",
			UserMessage: "The runtime version is unsupported. The runtime owner needs to upgrade it, then retry this task.",
		},
		taskfailure.ReasonAgentRuntimeMissingExecutable.String(): {
			Action:      "alert",
			UserMessage: "The runtime is missing a required executable. The runtime owner needs to install it, then retry this task.",
		},
		taskfailure.ReasonAgentUnknown.String(): {Action: "surface"},
		"codex_semantic_inactivity":             {Action: "retry", FreshSession: true},
		"idle_watchdog":                         {Action: "retry", FreshSession: true, RetryLimit: 1},
	}
}

func managedHookPolicies(workspaceID string) []managedHookPolicyDefinition {
	binding := HookBinding{Kind: HookScopeWorkspace, ID: workspaceID}
	managed := func(key, name, description string, events []HookEventType, conditions []Condition, decision HookDecision, requirement string, modifications map[string]any) managedHookPolicyDefinition {
		id := uuid.NewSHA1(managedHookNamespace, []byte(workspaceID+":"+key)).String()
		return managedHookPolicyDefinition{
			Key: key,
			Policy: HookPolicy{
				ID: id, Version: 1, Name: name, Description: description,
				Mode: HookModeManaged, FailMode: HookFailClosed,
				Events: events, Bindings: []HookBinding{binding},
				Conditions: conditions,
				Handlers: []HookHandler{{
					ID:       uuid.NewSHA1(managedHookNamespace, []byte(id+":handler")).String(),
					Decision: decision, Requirement: requirement, Modifications: modifications,
				}},
			},
		}
	}
	boolCondition := func(field string, value bool) Condition {
		return Condition{Field: field, Op: "eq", Value: value}
	}
	messageEvent := []HookEventType{HookBeforeMessageSend}
	definitions := []managedHookPolicyDefinition{
		managed("message.recipient", "Require a recipient on agent comments",
			"Requires an agent-authored comment to address another person, agent, squad, or @all unless an active wakeup is the continuation.",
			messageEvent,
			[]Condition{
				boolCondition("message.agent_authored", true),
				boolCondition("message.has_recipient", false),
				boolCondition("message.has_active_wakeup", false),
			}, HookRequire, ManagedRequirementMessageRecipient, nil),
		managed("message.backed_continuation", "Require evidence for promised continuation",
			"Prevents an agent comment from promising future work unless an active wakeup will resume the task.",
			messageEvent,
			[]Condition{
				boolCondition("message.agent_authored", true),
				boolCondition("message.promises_continuation", true),
				boolCondition("message.has_active_wakeup", false),
			}, HookRequire, ManagedRequirementNoPromise, nil),
		managed("message.correct_thread", "Keep agent replies in the triggering thread",
			"Moves an agent reply to the exact comment that triggered its current task.",
			messageEvent,
			[]Condition{
				boolCondition("message.agent_authored", true),
				boolCondition("message.thread_required", true),
				boolCondition("message.correct_thread", false),
			}, HookModify, "", map[string]any{"parent_id": "$event.message.required_parent_id"}),
		managed("message.no_action", "Block comments after no_action",
			"Prevents a task that recorded no_action from posting a comment in the same run.",
			messageEvent,
			[]Condition{
				boolCondition("message.agent_authored", true),
				boolCondition("message.no_action", true),
			}, HookBlock, ManagedRequirementNoAction, nil),
		managed("message.sub_issue_owner", "Keep sub-issue updates on the parent",
			"Prevents a sub-issue agent from addressing the initiating user directly.",
			messageEvent,
			[]Condition{
				boolCondition("message.agent_authored", true),
				boolCondition("message.is_sub_issue", true),
				boolCondition("message.mentions_initiator", true),
			}, HookRequire, ManagedRequirementSubIssueOwner, nil),
		managed("message.sub_issue_agent", "Keep the parent agent in sub-issue updates",
			"Requires a sub-issue comment to mention an agent other than its author.",
			messageEvent,
			[]Condition{
				boolCondition("message.agent_authored", true),
				boolCondition("message.is_sub_issue", true),
				boolCondition("message.mentions_agent", false),
			}, HookRequire, ManagedRequirementSubIssueAgent, nil),
		managed("message.sub_issue_split", "Keep one task session on one issue",
			"Prevents one task session from splitting its comments between a parent and sub-issue.",
			messageEvent,
			[]Condition{
				boolCondition("message.agent_authored", true),
				boolCondition("message.is_sub_issue", true),
				boolCondition("message.posted_on_parent", true),
			}, HookRequire, ManagedRequirementSubIssueSplit, nil),
		managed("task.actual_continuation", "Require evidence before an agent run stops",
			"Prevents a non-terminal task from completing without a persisted wakeup, member request, dispatch, Handoff, or blocked issue.",
			[]HookEventType{HookBeforeAgentStop, HookBeforeTaskComplete},
			[]Condition{
				boolCondition("issue.terminal", false),
				boolCondition("continuation.present", false),
			}, HookRequire, "Create a wakeup, member request, dispatch, Handoff, or mark the issue blocked before completing the task.", nil),
		managed("chain.status_record", "Record Chain v2 status changes",
			"Creates a Workflow trace before every status change on an issue with an active Chain v2 run.",
			[]HookEventType{HookBeforeIssueStatus}, nil,
			HookAllow, "", nil),
		managed("chain.done_approval", "Require final Chain v2 approval before Done",
			"Prevents an active Chain v2 issue from moving to Done until every phase is complete and its final required step is approved.",
			[]HookEventType{HookBeforeIssueStatus},
			[]Condition{
				{Field: "status.to", Op: "eq", Value: "done"},
				boolCondition("chain.active", true),
				boolCondition("chain.approved_for_done", false),
			},
			HookRequire, "Complete and approve the final required Workflow step before moving the issue to Done.", nil),
		managed("chain.step_record", "Record completed Chain v2 steps",
			"Creates a Workflow trace after every durable Chain v2 step completion.",
			[]HookEventType{HookAfterWorkflowStep}, nil,
			HookAllow, "", nil),
		managed("wakeup.trigger_enabled", "Require an enabled wakeup trigger",
			"Prevents creation when the selected wakeup trigger is disabled for the workspace.",
			[]HookEventType{HookBeforeWakeupCreate},
			[]Condition{boolCondition("wakeup.trigger_enabled", false)},
			HookRequire, "The selected wakeup trigger is disabled for this workspace.", nil),
		managed("wakeup.active_limit", "Limit active wakeups per agent and issue",
			"Prevents one agent from stacking more active wakeups on an issue than the workspace permits.",
			[]HookEventType{HookBeforeWakeupCreate},
			[]Condition{{Field: "wakeup.active_count", Op: "gte", Value: "$event.wakeup.max_active"}},
			HookRequire, "The active wakeup limit for this agent and issue has been reached.", nil),
		managed("wakeup.minimum_notice", "Require enough notice for time wakeups",
			"Prevents a time wakeup from being scheduled closer than the workspace interval.",
			[]HookEventType{HookBeforeWakeupCreate},
			[]Condition{
				{Field: "wakeup.trigger_type", Op: "eq", Value: "time"},
				{Field: "wakeup.seconds_until_fire", Op: "lt", Value: "$event.wakeup.min_interval_seconds"},
			},
			HookRequire, "The wakeup is scheduled too soon.", nil),
		managed("wakeup.minimum_spacing", "Space time wakeups apart",
			"Prevents one agent from stacking time wakeups closer than the workspace interval.",
			[]HookEventType{HookBeforeWakeupCreate},
			[]Condition{
				{Field: "wakeup.trigger_type", Op: "eq", Value: "time"},
				boolCondition("wakeup.has_last_fire", true),
				{Field: "wakeup.seconds_after_last_fire", Op: "lt", Value: "$event.wakeup.min_interval_seconds"},
			},
			HookRequire, "Time wakeups for this agent and issue must be spaced farther apart.", nil),
		managed("wakeup.progress_required", "Require progress between wakeups",
			"Stops a self-wakeup chain after the workspace limit is reached without objective progress.",
			[]HookEventType{HookBeforeWakeupCreate},
			[]Condition{
				boolCondition("wakeup.loop_limit_enabled", true),
				{Field: "wakeup.since_member_reply", Op: "gte", Value: "$event.wakeup.max_without_progress"},
				{Field: "wakeup.since_status_change", Op: "gte", Value: "$event.wakeup.max_without_progress"},
				{Field: "wakeup.since_progress_update", Op: "gte", Value: "$event.wakeup.max_without_progress"},
			},
			HookRequire, "Record a result or ask a person before scheduling another wakeup.", nil),
		managed("wakeup.fire_failure_record", "Record every wakeup fire failure",
			"Creates a Workflow trace for every failed wakeup dispatch before the failure action is applied.",
			[]HookEventType{HookOnWakeupFireFailure}, nil,
			HookAllow, "", nil),
		managed("wakeup.fire_retry", "Postpone recoverable wakeup failures",
			"Postpones wakeups while the agent is busy, offline, or missing a runtime and notifies a person after repeated postpones.",
			[]HookEventType{HookOnWakeupFireFailure},
			[]Condition{{Field: "failure.reason", Op: "in", Values: []any{"active_task", "offline", "no_runtime", "trigger_disabled"}}},
			HookModify, "", map[string]any{
				"failure_action":   "postpone",
				"postpone_seconds": 300,
				"notify_after":     3,
			}),
	}
	routes := managedTaskFailureRoutes()
	reasons := make([]string, 0, len(routes))
	for reason := range routes {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		route := routes[reason]
		definitions = append(definitions, managed(
			"failure.task."+reason,
			"Route task failure: "+reason,
			"Selects the task failure action, retry boundary, session handling, and user-facing message.",
			[]HookEventType{HookOnTaskFailure},
			[]Condition{{Field: "failure.reason", Op: "eq", Value: reason}},
			HookModify,
			"",
			map[string]any{
				"failure_action": route.Action,
				"fresh_session":  route.FreshSession,
				"retry_limit":    int(route.RetryLimit),
				"user_message":   route.UserMessage,
			},
		))
	}
	return definitions
}

func isBuiltInManagedHookPolicy(workspaceID, policyID string) bool {
	for _, definition := range managedHookPolicies(workspaceID) {
		if definition.Policy.ID == policyID {
			return true
		}
	}
	return false
}

func managedPolicyJSON(policy HookPolicy) (events, conditions, modifications, actions []byte) {
	events, _ = json.Marshal(policy.Events)
	conditions, _ = json.Marshal(policy.Conditions)
	if len(policy.Handlers) > 0 {
		handler := policy.Handlers[0]
		if handler.Modifications == nil {
			handler.Modifications = map[string]any{}
		}
		if handler.Actions == nil {
			handler.Actions = []HookAction{}
		}
		modifications, _ = json.Marshal(handler.Modifications)
		actions, _ = json.Marshal(handler.Actions)
	}
	return
}
