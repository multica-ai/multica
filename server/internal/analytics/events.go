package analytics

import "strings"

// Event names. Keep in sync with docs/analytics.md.
const (
	EventSignup                        = "signup"
	EventWorkspaceCreated              = "workspace_created"
	EventRuntimeRegistered             = "runtime_registered"
	EventRuntimeReady                  = "runtime_ready"
	EventRuntimeFailed                 = "runtime_failed"
	EventRuntimeOffline                = "runtime_offline"
	EventIssueExecuted                 = "issue_executed"
	EventIssueCreated                  = "issue_created"
	EventChatMessageSent               = "chat_message_sent"
	EventAgentTaskQueued               = "agent_task_queued"
	EventAgentTaskDispatched           = "agent_task_dispatched"
	EventAgentTaskStarted              = "agent_task_started"
	EventAgentTaskCompleted            = "agent_task_completed"
	EventAgentTaskFailed               = "agent_task_failed"
	EventAgentTaskCancelled            = "agent_task_cancelled"
	EventAutopilotRunStarted           = "autopilot_run_started"
	EventAutopilotRunCompleted         = "autopilot_run_completed"
	EventAutopilotRunFailed            = "autopilot_run_failed"
	EventTeamInviteSent                = "team_invite_sent"
	EventTeamInviteAccepted            = "team_invite_accepted"
	EventOnboardingStarted             = "onboarding_started"
	EventOnboardingQuestionnaireSubmit = "onboarding_questionnaire_submitted"
	EventAgentCreated                  = "agent_created"
	EventOnboardingCompleted           = "onboarding_completed"
	EventCloudWaitlistJoined           = "cloud_waitlist_joined"
	EventFeedbackSubmitted             = "feedback_submitted"
	EventContactSalesSubmitted         = "contact_sales_submitted"
)

const EventSchemaVersion = 2

const (
	SourceOnboarding = "onboarding"
	SourceManual     = "manual"
	SourceChat       = "chat"
	SourceAutopilot  = "autopilot"
	SourceAPI        = "api"
)

// CoreProperties are the shared join and segmentation fields used by the
// canonical PostHog events. Empty values are omitted, except is_demo which is
// always stamped so dashboards can filter demo data without sparse-property
// edge cases.
type CoreProperties struct {
	UserID         string
	WorkspaceID    string
	AgentID        string
	TaskID         string
	IssueID        string
	ChatSessionID  string
	AutopilotRunID string
	Source         string
	RuntimeMode    string
	Provider       string
	IsDemo         bool
}

type TaskContext = CoreProperties

// Onboarding completion paths. Keep in sync with docs/analytics.md.
const (
	OnboardingPathFull           = "full"            // reached first_issue end of flow
	OnboardingPathRuntimeSkipped = "runtime_skipped" // completed without connecting a runtime
	OnboardingPathCloudWaitlist  = "cloud_waitlist"  // completed via cloud waitlist soft exit
	OnboardingPathSkipExisting   = "skip_existing"   // "I've done this before" from welcome
	OnboardingPathInviteAccept   = "invite_accept"   // accepted at least one invitation from /invitations
	OnboardingPathUnknown        = "unknown"         // fallback when the server can't derive the path
)

// Platform is used as the "platform" event property so funnels can split by
// web / desktop / cli. Request-path events use PlatformServer as a fallback
// when the caller is a server-originating action (e.g. auto-created user);
// otherwise the frontend passes the real platform via a header / body field
// in later iterations.
const (
	PlatformServer  = "server"
	PlatformWeb     = "web"
	PlatformDesktop = "desktop"
	PlatformCLI     = "cli"
)

// Signup builds the signup event. signupSource is populated from the
// frontend's stored UTM/referrer cookie if present; leave empty otherwise.
func Signup(userID, email, signupSource string) Event {
	return Event{
		Name:       EventSignup,
		DistinctID: userID,
		Properties: map[string]any{
			"email_domain":  emailDomain(email),
			"signup_source": signupSource,
		},
		SetOnce: map[string]any{
			"email":         email,
			"signup_source": signupSource,
		},
	}
}

// WorkspaceCreated builds the workspace_created event. "Is this the user's
// first workspace?" is deliberately not stamped here — it's derived in
// PostHog by checking whether the user has a prior workspace_created event.
func WorkspaceCreated(userID, workspaceID string) Event {
	return Event{
		Name:        EventWorkspaceCreated,
		DistinctID:  userID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(nil, CoreProperties{
			UserID:      userID,
			WorkspaceID: workspaceID,
			Source:      SourceManual,
		}),
	}
}

// RuntimeRegistered fires on the first time a (workspace, daemon, provider)
// triple is upserted. The handler uses a `xmax = 0` flag returned from the
// upsert query to distinguish inserts from updates — heartbeats and repeat
// registrations never emit this event.
//
// ownerID may be empty when the daemon authenticates via a daemon token
// (no user context); downstream funnels that need per-user attribution
// fall back to `workspace_id` as the grouping key.
func RuntimeRegistered(ownerID, workspaceID, runtimeID, daemonID, provider, runtimeVersion, cliVersion string) Event {
	distinct := ownerID
	if distinct == "" {
		// A per-workspace synthetic id keeps PostHog from merging unrelated
		// daemon registrations across workspaces under a single "anonymous"
		// person. It's stable within a workspace so repeat heartbeats (which
		// don't emit anyway) would at least group correctly.
		distinct = "workspace:" + workspaceID
	}
	return Event{
		Name:        EventRuntimeRegistered,
		DistinctID:  distinct,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(map[string]any{
			"runtime_id":      runtimeID,
			"daemon_id":       daemonID,
			"provider":        provider,
			"runtime_mode":    "local",
			"runtime_version": runtimeVersion,
			"cli_version":     cliVersion,
		}, CoreProperties{
			UserID:      ownerID,
			WorkspaceID: workspaceID,
			Source:      SourceManual,
			RuntimeMode: "local",
			Provider:    provider,
		}),
	}
}

func RuntimeReady(ownerID, workspaceID, runtimeID, daemonID, provider string, readyDurationMS int64) Event {
	distinct := ownerID
	if distinct == "" {
		distinct = "workspace:" + workspaceID
	}
	props := map[string]any{
		"runtime_id": runtimeID,
		"daemon_id":  daemonID,
	}
	if readyDurationMS > 0 {
		props["ready_duration_ms"] = readyDurationMS
	}
	return Event{
		Name:        EventRuntimeReady,
		DistinctID:  distinct,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(props, CoreProperties{
			UserID:      ownerID,
			WorkspaceID: workspaceID,
			Source:      SourceManual,
			RuntimeMode: "local",
			Provider:    provider,
		}),
	}
}

func RuntimeFailed(ownerID, workspaceID, daemonID, provider, failureReason, errorType string, recoverable bool) Event {
	distinct := ownerID
	if distinct == "" && workspaceID != "" {
		distinct = "workspace:" + workspaceID
	}
	return Event{
		Name:        EventRuntimeFailed,
		DistinctID:  distinct,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(map[string]any{
			"daemon_id":      daemonID,
			"failure_reason": failureReason,
			"error_type":     errorType,
			"recoverable":    recoverable,
		}, CoreProperties{
			UserID:      ownerID,
			WorkspaceID: workspaceID,
			Source:      SourceManual,
			RuntimeMode: "local",
			Provider:    provider,
		}),
	}
}

func RuntimeOffline(ownerID, workspaceID, runtimeID, daemonID, provider string) Event {
	distinct := ownerID
	if distinct == "" {
		distinct = "workspace:" + workspaceID
	}
	return Event{
		Name:        EventRuntimeOffline,
		DistinctID:  distinct,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(map[string]any{
			"runtime_id": runtimeID,
			"daemon_id":  daemonID,
		}, CoreProperties{
			UserID:      ownerID,
			WorkspaceID: workspaceID,
			Source:      SourceManual,
			RuntimeMode: "local",
			Provider:    provider,
		}),
	}
}

// IssueExecuted fires at most once per issue lifetime — on the first task
// completion that flips `issues.first_executed_at` from NULL via an atomic
// UPDATE. Retries, re-assignments, and comment-triggered follow-ups never
// re-emit, which is what keeps the ≥1/≥2/≥5/≥10 funnel buckets honest.
//
// Deliberately not stamped here: the workspace's Nth-issue ordinal.
// Computing it at emit time is not atomic (two concurrent first-completions
// both read count=1, both emit n=1), and PostHog derives the same number
// exactly at query time from the event stream.
func IssueExecuted(actorID, workspaceID, issueID, taskID, agentID, source, runtimeMode, provider string, taskDurationMS int64) Event {
	return Event{
		Name:        EventIssueExecuted,
		DistinctID:  actorID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(map[string]any{
			"issue_id":         issueID,
			"task_id":          taskID,
			"agent_id":         agentID,
			"task_duration_ms": taskDurationMS,
			"duration_ms":      taskDurationMS,
		}, CoreProperties{
			UserID:      nonAgentUserID(actorID),
			WorkspaceID: workspaceID,
			AgentID:     agentID,
			TaskID:      taskID,
			IssueID:     issueID,
			Source:      source,
			RuntimeMode: runtimeMode,
			Provider:    provider,
		}),
	}
}

func IssueCreated(actorID, workspaceID, issueID, agentID, taskID, autopilotRunID, source string) Event {
	return Event{
		Name:        EventIssueCreated,
		DistinctID:  actorID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(nil, CoreProperties{
			UserID:         nonAgentUserID(actorID),
			WorkspaceID:    workspaceID,
			AgentID:        agentID,
			TaskID:         taskID,
			IssueID:        issueID,
			AutopilotRunID: autopilotRunID,
			Source:         source,
		}),
	}
}

func ChatMessageSent(userID, workspaceID, chatSessionID, taskID, agentID, runtimeMode, provider string) Event {
	return Event{
		Name:        EventChatMessageSent,
		DistinctID:  userID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(nil, CoreProperties{
			UserID:        userID,
			WorkspaceID:   workspaceID,
			AgentID:       agentID,
			TaskID:        taskID,
			ChatSessionID: chatSessionID,
			Source:        SourceChat,
			RuntimeMode:   runtimeMode,
			Provider:      provider,
		}),
	}
}

func AgentTaskQueued(ctx TaskContext) Event {
	return agentTaskEvent(EventAgentTaskQueued, ctx, nil)
}

func AgentTaskDispatched(ctx TaskContext) Event {
	return agentTaskEvent(EventAgentTaskDispatched, ctx, nil)
}

func AgentTaskStarted(ctx TaskContext) Event {
	return agentTaskEvent(EventAgentTaskStarted, ctx, nil)
}

func AgentTaskCompleted(ctx TaskContext, durationMS int64) Event {
	return agentTaskEvent(EventAgentTaskCompleted, ctx, map[string]any{
		"duration_ms": durationMS,
	})
}

func AgentTaskFailed(ctx TaskContext, durationMS int64, failureReason, errorType string, willRetry bool) Event {
	return agentTaskEvent(EventAgentTaskFailed, ctx, map[string]any{
		"duration_ms":    durationMS,
		"failure_reason": failureReason,
		"error_type":     errorType,
		"will_retry":     willRetry,
	})
}

func AgentTaskCancelled(ctx TaskContext, durationMS int64) Event {
	return agentTaskEvent(EventAgentTaskCancelled, ctx, map[string]any{
		"duration_ms": durationMS,
	})
}

// AutopilotAssignee describes the autopilot's configured target. agent_id is
// always the agent that will actually execute the work (the squad leader for
// squad autopilots) so funnels grouping by agent stay consistent. assignee_*
// fields record the original configuration so reports can tell a solo-agent
// autopilot apart from a squad one without joining back to the autopilot row.
type AutopilotAssignee struct {
	AgentID      string // executing agent — leader for squad autopilots
	AssigneeType string // "agent" or "squad"
	SquadID      string // empty when AssigneeType != "squad"
}

func AutopilotRunStarted(actorID, workspaceID, autopilotID, runID string, assignee AutopilotAssignee, triggerSource string) Event {
	return autopilotRunEvent(EventAutopilotRunStarted, actorID, workspaceID, autopilotID, runID, assignee, triggerSource, nil)
}

func AutopilotRunCompleted(actorID, workspaceID, autopilotID, runID string, assignee AutopilotAssignee, triggerSource string, durationMS int64) Event {
	return autopilotRunEvent(EventAutopilotRunCompleted, actorID, workspaceID, autopilotID, runID, assignee, triggerSource, map[string]any{
		"duration_ms": durationMS,
	})
}

func AutopilotRunFailed(actorID, workspaceID, autopilotID, runID string, assignee AutopilotAssignee, triggerSource, failureReason, errorType string, willRetry bool, durationMS int64) Event {
	return autopilotRunEvent(EventAutopilotRunFailed, actorID, workspaceID, autopilotID, runID, assignee, triggerSource, map[string]any{
		"duration_ms":    durationMS,
		"failure_reason": failureReason,
		"error_type":     errorType,
		"will_retry":     willRetry,
	})
}

// TeamInviteSent fires when a workspace admin creates an invitation.
// inviteMethod is "email" for now; future non-email invite flows can pass
// their own value to keep this stable.
func TeamInviteSent(inviterID, workspaceID, invitedEmail, inviteMethod string) Event {
	return Event{
		Name:        EventTeamInviteSent,
		DistinctID:  inviterID,
		WorkspaceID: workspaceID,
		Properties: map[string]any{
			"invited_email_domain": emailDomain(invitedEmail),
			"invite_method":        inviteMethod,
		},
	}
}

// TeamInviteAccepted fires when the invitee accepts and joins the workspace.
// daysSinceInvite lets us segment fast-acceptance (warm) from long-tail
// acceptance (someone dug through old email).
func TeamInviteAccepted(inviteeID, workspaceID string, daysSinceInvite int64) Event {
	return Event{
		Name:        EventTeamInviteAccepted,
		DistinctID:  inviteeID,
		WorkspaceID: workspaceID,
		Properties: map[string]any{
			"days_since_invite": daysSinceInvite,
		},
	}
}

// OnboardingQuestionnaireSubmitted fires the first time a user's
// `user.onboarding_questionnaire` transitions from "at least one slot
// unresolved" to "every slot has either an answer or a skip marker".
// The handler drives this transition — we emit from PatchOnboarding so
// the single emission site stays honest even if the frontend retries.
//
// `source` and `useCase` are multi-select (users can pick several);
// `role` stays single-select. Empty slice = no answer (skip is
// captured separately via the *Skipped booleans).
//
// The three answers are also mirrored into person properties via $set
// so cohorting by source / role / use_case works across every event
// on the same user without re-joining back to the DB. PostHog accepts
// array property values; breakdowns on a multi-value property treat
// each element as a separate group.
//
// `*Skipped` booleans capture per-question skip intent. `*HasOther`
// are presence booleans for the free-text "other" override; the
// free-text content is kept in the DB for product research but not
// broadcast via analytics (PII risk + low cardinality ask).
func OnboardingQuestionnaireSubmitted(userID string, source []string, role string, useCase []string, sourceSkipped, roleSkipped, useCaseSkipped, sourceHasOther, roleHasOther, useCaseHasOther bool) Event {
	// Normalize nil slices to [] so PostHog property values are stable
	// (avoids null vs [] mixing in property type inference).
	if source == nil {
		source = []string{}
	}
	if useCase == nil {
		useCase = []string{}
	}
	return Event{
		Name:       EventOnboardingQuestionnaireSubmit,
		DistinctID: userID,
		Properties: withCoreProperties(map[string]any{
			"source":             source,
			"role":               role,
			"use_case":           useCase,
			"source_skipped":     sourceSkipped,
			"role_skipped":       roleSkipped,
			"use_case_skipped":   useCaseSkipped,
			"source_has_other":   sourceHasOther,
			"role_has_other":     roleHasOther,
			"use_case_has_other": useCaseHasOther,
		}, CoreProperties{
			UserID: userID,
			Source: SourceOnboarding,
		}),
		Set: map[string]any{
			"source":   source,
			"role":     role,
			"use_case": useCase,
		},
	}
}

// AgentCreated fires whenever a new agent is added to a workspace — not
// just inside onboarding. `isFirstAgentInWorkspace` lets the funnel
// isolate the Step 4 signal from later agent additions.
//
// template is the template slug the frontend used to seed the agent
// (e.g. "coding", "planning", "writing", "assistant") — empty when the
// caller didn't come from a template picker.
func AgentCreated(actorID, workspaceID, agentID, provider, runtimeMode, template string, isFirstAgentInWorkspace bool) Event {
	return Event{
		Name:        EventAgentCreated,
		DistinctID:  actorID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(map[string]any{
			"agent_id":                    agentID,
			"provider":                    provider,
			"runtime_mode":                runtimeMode,
			"template":                    template,
			"is_first_agent_in_workspace": isFirstAgentInWorkspace,
		}, CoreProperties{
			UserID:      actorID,
			WorkspaceID: workspaceID,
			AgentID:     agentID,
			Source:      SourceManual,
			RuntimeMode: runtimeMode,
			Provider:    provider,
		}),
	}
}

// OnboardingCompleted fires from CompleteOnboarding. `completionPath`
// is derived server-side from the state the user arrived in (see the
// OnboardingPath* constants above). `joinedCloudWaitlist` is true when
// the user submitted the waitlist form at any point during the flow —
// it's orthogonal to `completion_path`; a user may submit the form and
// still pick CLI, so we keep both signals.
//
// onboardedAt is an RFC3339 timestamp set $set_once on the person so
// "onboarded before date X" cohorts are queryable directly from
// person_properties without re-emitting per-event.
func OnboardingCompleted(userID, workspaceID, completionPath, onboardedAt string, joinedCloudWaitlist bool) Event {
	return Event{
		Name:        EventOnboardingCompleted,
		DistinctID:  userID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(map[string]any{
			"completion_path":       completionPath,
			"joined_cloud_waitlist": joinedCloudWaitlist,
		}, CoreProperties{
			UserID:      userID,
			WorkspaceID: workspaceID,
			Source:      SourceOnboarding,
		}),
		SetOnce: map[string]any{
			"onboarded_at": onboardedAt,
		},
	}
}

// CloudWaitlistJoined fires when a user submits the Step 3 cloud
// waitlist form. `hasReason` is a presence bool — the free-text reason
// stays in the DB for product research.
func CloudWaitlistJoined(userID string, hasReason bool) Event {
	return Event{
		Name:       EventCloudWaitlistJoined,
		DistinctID: userID,
		Properties: withCoreProperties(map[string]any{
			"has_reason": hasReason,
		}, CoreProperties{
			UserID: userID,
			Source: SourceOnboarding,
		}),
	}
}

// FeedbackSubmitted fires after a feedback row is successfully inserted.
// The raw message is stored in the DB and never broadcast — we only emit a
// coarse length bucket, an image-presence flag, and the client platform /
// version so support can segment without leaking content.
func FeedbackSubmitted(userID, workspaceID string, messageLen int, hasImages bool, platform, appVersion string) Event {
	props := map[string]any{
		"message_length_bucket": feedbackLengthBucket(messageLen),
		"has_images":            hasImages,
	}
	if platform != "" {
		props["platform"] = platform
	}
	if appVersion != "" {
		props["app_version"] = appVersion
	}
	return Event{
		Name:        EventFeedbackSubmitted,
		DistinctID:  userID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(props, CoreProperties{
			UserID:      userID,
			WorkspaceID: workspaceID,
			Source:      "ops_feedback",
		}),
	}
}

// ContactSalesSubmitted fires after a contact-sales inquiry is recorded.
// The form is public and unauthenticated, so DistinctID is empty (PostHog
// will treat it as an anonymous event). We carry the coarse company size,
// country, and intended use case so sales / marketing can split inbound
// volume without having to query the operational DB.
func ContactSalesSubmitted(inquiryID, companySize, countryRegion, useCase string, hasGoals bool) Event {
	props := map[string]any{
		"inquiry_id":     inquiryID,
		"company_size":   companySize,
		"country_region": countryRegion,
		"use_case":       useCase,
		"has_goals":      hasGoals,
	}
	return Event{
		Name:       EventContactSalesSubmitted,
		DistinctID: inquiryID,
		Properties: withCoreProperties(props, CoreProperties{
			Source: "marketing_contact_sales",
		}),
	}
}

func agentTaskEvent(name string, ctx TaskContext, extra map[string]any) Event {
	props := withCoreProperties(extra, CoreProperties(ctx))
	return Event{
		Name:        name,
		DistinctID:  distinctID(ctx.UserID, ctx.WorkspaceID, ctx.AgentID),
		WorkspaceID: ctx.WorkspaceID,
		Properties:  props,
	}
}

func autopilotRunEvent(name, actorID, workspaceID, autopilotID, runID string, assignee AutopilotAssignee, triggerSource string, extra map[string]any) Event {
	if extra == nil {
		extra = map[string]any{}
	}
	extra["trigger_source"] = triggerSource
	props := withCoreProperties(extra, CoreProperties{
		UserID:         nonAgentUserID(actorID),
		WorkspaceID:    workspaceID,
		AgentID:        assignee.AgentID,
		AutopilotRunID: runID,
		Source:         SourceAutopilot,
	})
	props["autopilot_id"] = autopilotID
	if assignee.AssigneeType != "" {
		props["assignee_type"] = assignee.AssigneeType
	}
	if assignee.SquadID != "" {
		props["squad_id"] = assignee.SquadID
	}
	return Event{
		Name:        name,
		DistinctID:  actorID,
		WorkspaceID: workspaceID,
		Properties:  props,
	}
}

func withCoreProperties(props map[string]any, core CoreProperties) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	if core.UserID != "" {
		props["user_id"] = core.UserID
	}
	if core.AgentID != "" {
		props["agent_id"] = core.AgentID
	}
	if core.TaskID != "" {
		props["task_id"] = core.TaskID
	}
	if core.IssueID != "" {
		props["issue_id"] = core.IssueID
	}
	if core.ChatSessionID != "" {
		props["chat_session_id"] = core.ChatSessionID
	}
	if core.AutopilotRunID != "" {
		props["autopilot_run_id"] = core.AutopilotRunID
	}
	if core.Source != "" {
		props["source"] = core.Source
	}
	if core.RuntimeMode != "" {
		props["runtime_mode"] = core.RuntimeMode
	}
	if core.Provider != "" {
		props["provider"] = core.Provider
	}
	props["is_demo"] = core.IsDemo
	return props
}

func distinctID(userID, workspaceID, agentID string) string {
	if userID != "" {
		return userID
	}
	// Synthetic PostHog distinct IDs are namespace-prefixed; user UUIDs are not.
	if agentID != "" {
		return "agent:" + agentID
	}
	if workspaceID != "" {
		return "workspace:" + workspaceID
	}
	return ""
}

func nonAgentUserID(distinct string) string {
	if distinct == "" || strings.Contains(distinct, ":") {
		return ""
	}
	return distinct
}

func feedbackLengthBucket(n int) string {
	switch {
	case n < 100:
		return "0-100"
	case n < 500:
		return "100-500"
	case n < 2000:
		return "500-2000"
	default:
		return "2000+"
	}
}

func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}
