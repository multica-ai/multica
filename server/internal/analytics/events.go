package analytics

import "strings"

// Event names. Keep in sync with docs/analytics.md.
const (
	EventSignup                        = "signup"
	EventWorkspaceCreated              = "workspace_created"
	EventRuntimeRegistered             = "runtime_registered"
	EventIssueExecuted                 = "issue_executed"
	EventTeamInviteSent                = "team_invite_sent"
	EventTeamInviteAccepted            = "team_invite_accepted"
	EventOnboardingQuestionnaireSubmit = "onboarding_questionnaire_submitted"
	EventAgentCreated                  = "agent_created"
	EventOnboardingCompleted           = "onboarding_completed"
	EventCloudWaitlistJoined           = "cloud_waitlist_joined"
	EventStarterContentDecided         = "starter_content_decided"
	EventFeedbackSubmitted             = "feedback_submitted"
)

// Onboarding completion paths. Keep in sync with docs/analytics.md.
const (
	OnboardingPathFull           = "full"            // reached first_issue end of flow
	OnboardingPathRuntimeSkipped = "runtime_skipped" // completed without connecting a runtime
	OnboardingPathCloudWaitlist  = "cloud_waitlist"  // completed via cloud waitlist soft exit
	OnboardingPathSkipExisting   = "skip_existing"   // "I've done this before" from welcome
	OnboardingPathUnknown        = "unknown"         // fallback when the server can't derive the path
)

// Starter content branches. Matches the server-authoritative decision in
// ImportStarterContent (hasAgent ? agent_guided : self_serve). DismissStarter
// carries the same branch so acceptance rates split cleanly.
const (
	StarterContentBranchAgentGuided = "agent_guided"
	StarterContentBranchSelfServe   = "self_serve"
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
func RuntimeRegistered(ownerID, workspaceID, runtimeID, provider, runtimeVersion, cliVersion string) Event {
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
		Properties: map[string]any{
			"runtime_id":      runtimeID,
			"provider":        provider,
			"runtime_version": runtimeVersion,
			"cli_version":     cliVersion,
		},
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
func IssueExecuted(actorID, workspaceID, issueID string, taskDurationMS int64) Event {
	return Event{
		Name:        EventIssueExecuted,
		DistinctID:  actorID,
		WorkspaceID: workspaceID,
		Properties: map[string]any{
			"issue_id":         issueID,
			"task_duration_ms": taskDurationMS,
		},
	}
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
// `user.onboarding_questionnaire` transitions from empty (or partial) to
// all three answers present. The handler drives this transition — we
// emit from PatchOnboarding so the single emission site stays honest
// even if the frontend retries.
//
// The three answers are also mirrored into person properties via $set
// so cohorting by role / use_case / team_size works across every event
// on the same user without re-joining back to the DB.
//
// teamSizeOther / roleOther / useCaseOther are presence booleans only —
// the free-text content is kept in the DB for product research but not
// broadcast via analytics (PII risk + low cardinality ask).
func OnboardingQuestionnaireSubmitted(userID, teamSize, role, useCase string, teamSizeOther, roleOther, useCaseOther bool) Event {
	return Event{
		Name:       EventOnboardingQuestionnaireSubmit,
		DistinctID: userID,
		Properties: map[string]any{
			"team_size":           teamSize,
			"role":                role,
			"use_case":            useCase,
			"team_size_has_other": teamSizeOther,
			"role_has_other":      roleOther,
			"use_case_has_other":  useCaseOther,
		},
		Set: map[string]any{
			"team_size": teamSize,
			"role":      role,
			"use_case":  useCase,
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
func AgentCreated(actorID, workspaceID, agentID, provider, template string, isFirstAgentInWorkspace bool) Event {
	return Event{
		Name:        EventAgentCreated,
		DistinctID:  actorID,
		WorkspaceID: workspaceID,
		Properties: map[string]any{
			"agent_id":                    agentID,
			"provider":                    provider,
			"template":                    template,
			"is_first_agent_in_workspace": isFirstAgentInWorkspace,
		},
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
func OnboardingCompleted(userID, completionPath, onboardedAt string, joinedCloudWaitlist bool) Event {
	return Event{
		Name:       EventOnboardingCompleted,
		DistinctID: userID,
		Properties: map[string]any{
			"completion_path":       completionPath,
			"joined_cloud_waitlist": joinedCloudWaitlist,
		},
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
		Properties: map[string]any{
			"has_reason": hasReason,
		},
	}
}

// StarterContentDecided fires on the atomic NULL -> terminal state
// transition in both ImportStarterContent and DismissStarterContent.
// branch carries agent_guided / self_serve for BOTH decisions — the
// dismiss handler resolves it from the current ListAgents state so
// acceptance rates split cleanly by branch.
func StarterContentDecided(userID, workspaceID, decision, branch string) Event {
	return Event{
		Name:        EventStarterContentDecided,
		DistinctID:  userID,
		WorkspaceID: workspaceID,
		Properties: map[string]any{
			"decision": decision,
			"branch":   branch,
		},
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
		Properties:  props,
	}
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
