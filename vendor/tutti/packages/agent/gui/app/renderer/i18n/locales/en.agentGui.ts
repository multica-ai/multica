import { enAgentGuiAddContent } from "./en.agentGuiAddContent.ts";
import { enAgentGuiCollaboration } from "./en.agentGuiCollaboration.ts";
import { enAgentGuiComposer } from "./en.agentGuiComposer.ts";
import { enAgentGuiHomeSuggestions } from "./en.agentGuiHomeSuggestions.ts";
import { enAgentGuiModelPlans } from "./en.agentGuiModelPlans.ts";
import { enAgentGuiOrchestration } from "./en.agentGuiOrchestration.ts";
import { enAgentGuiProjectLaunch } from "./en.agentGuiProjectLaunch.ts";
import { enAgentGuiProviderIdentity } from "./en.agentGuiProviderIdentity.ts";
import { enAgentGuiQuickPrompts } from "./en.agentGuiQuickPrompts.ts";
import { enAgentGuiReferencePicker } from "./en.agentGuiReferencePicker.ts";
import { enAgentGuiRuntimeNotices } from "./en.agentGuiRuntimeNotices.ts";
import { enAgentGuiSessionActions } from "./en.agentGuiSessionActions.ts";
import { enAgentGuiSide } from "./en.agentGuiSide.ts";
import { enAgentGuiSlashPalette } from "./en.agentGuiSlashPalette.ts";
import { enAgentGuiUsageStatus } from "./en.agentGuiUsageStatus.ts";

export const enAgentGui = {
  imageDownloaded: "Image downloaded",
  imageLoadFailed: "Image failed to load",
  imageTemporarilyUnavailable: "Image temporarily unavailable",
  retryImage: "Retry",
  initialPlaceholder: "Type @ to reference sessions, tasks, and agents",
  followupPlaceholder: "Request follow-up changes from {{provider}}",
  installRequiredPlaceholder: "Connect {{provider}} to send messages",
  installRequiredAction: "Connect",
  providerGateCheckingTitle: "Checking your agent",
  providerGateCheckingDescription:
    "One moment while we check whether {{provider}} is ready.",
  providerGateCheckingAgentsDescription:
    "One moment while we check whether agents are ready.",
  providerGateInstallTitle: "Connect {{provider}} first",
  providerGateInstallDescription:
    "{{provider}} needs to be connected before you can start a new chat here.",
  providerGateInstallAction: "Connect",
  providerGateLoginTitle: "Log in to {{provider}}",
  providerGateLoginDescription:
    "Log in with your account to start chatting with {{provider}}.",
  providerGateLoginAction: "Log in",
  providerGateModelPlanAction: "Use your own model",
  providerGateComingSoonTitle: "{{provider}} is coming soon",
  providerGateComingSoonDescription:
    "{{provider}} is not available yet. We will enable this agent when it is ready.",
  providerGateComingSoonAction: "coming soon",
  providerGateUnavailableTitle: "{{provider}} is not ready yet",
  providerGateUnavailableDescription:
    "We could not confirm that {{provider}} is ready. Try checking again.",
  providerGateRetryAction: "Check again",
  providerGateRuntimeSelectionTitle: "Choose which {{provider}} to use",
  providerGateRuntimeSelectionDescription:
    "Multiple {{provider}} installations were found — pick one to continue.",
  providerGateRuntimeSelectionAction: "Choose",
  providerGatePendingInstall: "Connecting…",
  providerGatePendingLogin: "Opening sign in…",
  providerGatePendingRefresh: "Checking…",
  targetSetupTitle: "Set up {{provider}}",
  targetSetupDescription:
    "Already have {{provider}} installed? Tutti can use it — or set it up for you.",
  targetSetupAuthRequired:
    "Runtime is installed and responds over ACP, but authentication is required.",
  targetSetupReady:
    "Runtime detected. You can check it again or sign in again.",
  targetSetupOpen: "Open setup",
  targetSetupRemaining: "Complete the remaining steps to use {{provider}}.",
  targetSetupComplete: "{{provider}} is ready.",
  targetSetupLoggedInAccount: "Signed-in account",
  targetSetupStage: {
    detect: "Detect runtime",
    install: "Install runtime",
    login: "Sign in"
  },
  targetSetupChecking: "Checking local and Tutti-managed runtimes…",
  targetSetupInstall: "Install runtime",
  targetSetupReinstall: "Reinstall runtime",
  targetSetupStarting: "Starting installation…",
  targetSetupAuthMethod: "Sign-in method",
  targetSetupAuthenticate: "Continue to sign in",
  targetSetupReauthenticate: "Sign in again",
  targetSetupAuthStarting: "Opening sign in…",
  targetSetupAuthFailed: "Authentication did not complete",
  targetSetupNoAuthMethods:
    "No supported sign-in method was reported. Check the runtime again.",
  targetSetupTerminalAuthHint:
    "This sign-in method must be completed in a terminal. Run the command below, finish the sign-in flow, then come back and check again.",
  targetSetupCopyCommand: "Copy command",
  targetSetupCommandCopied: "Copied",
  targetSetupTerminalLoginStart: "Start sign in",
  targetSetupTerminalLoginWaiting:
    "A terminal has been opened in the workspace. Finish signing in there; this page checks the status automatically.",
  targetSetupTerminalLoginCancel: "Cancel",
  targetSetupTerminalLoginTimedOut:
    "Timed out waiting for sign in to finish. Try again.",
  targetSetupTerminalLoginUnavailable:
    "A terminal could not be opened in this window. Copy the command and run it in your own terminal.",
  targetSetupRetry: "Check again",
  targetSetupFailed: "Runtime setup failed",
  targetSetupPhase: {
    preparing: "Preparing installation…",
    installing: "Installing pinned runtime…",
    verifying: "Verifying runtime version…",
    probing: "Checking ACP compatibility…",
    activating: "Activating managed runtime…",
    authenticating: "Waiting for authentication…",
    complete: "Installation complete"
  },
  collaboratorSessionReadOnlyPlaceholder:
    "This session belongs to another user and cannot be replied to directly",
  send: "Send",
  modelLabel: "Model",
  modelSelectionLabel: "Model selection",
  defaultModel: "Default model",
  ...enAgentGuiComposer,
  reasoningLabel: "Reasoning",
  reasoningDegreeLabel: "Reasoning level",
  reasoningOptionDefault: "Default",
  reasoningOptionMinimal: "Minimal",
  reasoningOptionLow: "Low",
  reasoningOptionMedium: "Medium",
  reasoningOptionHigh: "High",
  reasoningOptionXHigh: "X-High",
  reasoningOptionMax: "Max",
  reasoningOptionUltra: "Ultra",
  speedLabel: "Speed",
  speedSelectionLabel: "Speed",
  speedOptionStandard: "Standard",
  speedOptionStandardDescription: "Standard speed",
  speedOptionFast: "Fast",
  speedOptionFastDescription: "1.5x speed, increased usage",
  permissionModeReadOnly: "Ask for approval",
  permissionModeAuto: "Approve for me",
  permissionModeFullAccess: "Full access",
  permissionModeChangeUnavailableDuringTurn:
    "Permissions can’t be changed while a conversation is running",
  fullAccessWarning: {
    title: "Enable full access?",
    description:
      "Full access lets Codex act on your computer without asking for approval.",
    filesTitle: "Files and folders",
    filesDescription:
      "Read, create, modify, or delete files anywhere on this computer.",
    commandsTitle: "Terminal commands",
    commandsDescription: "Run commands and change system settings.",
    internetTitle: "Internet access",
    internetDescription: "Access websites and send data over the internet.",
    riskDescription:
      "Recent Codex releases, especially when using GPT-5.6 models, may go beyond your intent and accidentally delete or overwrite files. Only continue if you understand and accept this risk.",
    learnMore: "Learn more",
    cancel: "Cancel",
    confirm: "Enable full access"
  },
  fullAccessRestoredWarning: {
    title: "Full access is on",
    description:
      "Codex can run commands, use the internet, and create, modify, upload, or delete files anywhere on this computer without asking. This can cause data loss and expose you to prompt-injection attacks.",
    dontShowAgain: "Don't show again",
    dismissLabel: "Dismiss full access warning"
  },
  permissionSemantics: {
    "ask-before-write": {
      label: "Ask for approval",
      description: "Always ask to edit external files and use the internet"
    },
    "accept-edits": {
      label: "Accept edits",
      description:
        "Allows file edits, but still asks before higher-risk actions"
    },
    "locked-down": {
      label: "Don't ask",
      description: "Won't prompt. Unapproved actions are rejected"
    },
    auto: {
      label: "Approve for me",
      description: "Only ask for actions detected as potentially unsafe"
    },
    "full-access": {
      label: "Full access",
      description:
        "Unrestricted access to the internet and any file on your computer"
    },
    unconfigurable: {
      label: "Fixed mode",
      description:
        "This provider does not support changing permission mode here"
    }
  },
  permissionModes: {
    codex: {
      "read-only": {
        label: "Ask for approval",
        description: "Always ask to edit external files and use the internet"
      },
      auto: {
        label: "Approve for me",
        description: "Only ask for actions detected as potentially unsafe"
      },
      "full-access": {
        label: "Full access",
        description:
          "Unrestricted access to the internet and any file on your computer"
      }
    },
    cursor: {
      "read-only": {
        label: "Read-only",
        description:
          "Cursor plans and reads only. Proposes changes without making them."
      },
      agent: {
        label: "Ask for approval",
        description:
          "Full tool access. Cursor asks before running commands or other risky actions."
      },
      "full-access": {
        label: "Full access",
        description:
          "Runs commands without asking, unless explicitly denied by your Cursor permission rules."
      }
    },
    opencode: {
      "read-only": {
        label: "Read-only",
        description:
          "Reads and searches the local project, but denies edits, commands, network access, and other approval-gated actions."
      },
      ask: {
        label: "Ask",
        description:
          "Reads and searches directly, then asks before edits, commands, network access, and other gated actions."
      },
      "full-access": {
        label: "Full access",
        description:
          "Automatically allows gated actions without changing OpenCode's separate Plan mode restrictions."
      }
    },
    nexight: {
      "read-only": {
        label: "Ask for approval",
        description: "Always ask to edit external files and use the internet"
      },
      auto: {
        label: "Approve for me",
        description: "Only ask for actions detected as potentially unsafe"
      },
      "full-access": {
        label: "Full access",
        description:
          "Unrestricted access to the internet and any file on your computer"
      }
    },
    "claude-code": {
      default: {
        label: "Default",
        description:
          "Starts conservative. Asks before edits or higher-risk actions."
      },
      acceptEdits: {
        label: "Accept edits",
        description:
          "Allows direct file edits. Still asks before higher-risk actions."
      },
      dontAsk: {
        label: "Don't ask",
        description:
          "Won't prompt for approval. Actions not already allowed are rejected."
      },
      bypassPermissions: {
        label: "Bypass permissions",
        description:
          "Minimizes permission checks. Best for trusted tasks that need uninterrupted execution."
      }
    },
    hermes: {
      yolo: {
        label: "Fixed mode",
        description:
          "This provider doesn't support changing permission mode here."
      }
    }
  },
  modelContextWindowSuffix: "context window",
  modelTooltipVersionLabel: "Version",
  modelDescriptions: {
    frontierComplexCoding:
      "Frontier model for complex coding, research, and real-world work",
    everydayCoding: "Strong model for everyday coding",
    smallFastCostEfficient:
      "Small, fast, and cost-efficient model for simpler coding tasks",
    codingOptimized: "Coding-optimized model",
    ultraFastCoding: "Ultra-fast coding model",
    professionalLongRunning:
      "Optimized for professional work and long-running agents"
  },
  permissionLabel: "Run permissions",
  ...enAgentGuiOrchestration,
  queuedLabel: "Queued",
  queuePausedByUserLabel: "Queue paused",
  sendQueuedPromptNext: "Send next",
  editQueuedPrompt: "Edit",
  deleteQueuedPrompt: "Delete",
  queuedPromptMoreActions: "More queued prompt actions",
  stop: "Stop",
  stopping: "Stopping...",
  ...enAgentGuiUsageStatus,
  planCardTitle: "Plan",
  planCardCopy: "Copy plan",
  copyCode: "Copy code",
  mermaidLoading: "Rendering Mermaid diagram",
  mermaidRenderFailed: "Unable to render Mermaid diagram",
  mermaidExpand: "Expand Mermaid diagram",
  mermaidViewer: "Mermaid diagram viewer",
  mermaidPanHint: "Hold Space and drag to pan",
  mermaidZoomIn: "Zoom in",
  mermaidZoomOut: "Zoom out",
  mermaidResetZoom: "Reset view",
  mermaidZoomPercent: "Diagram zoom {{percent}}%",
  mermaidCloseViewer: "Close diagram viewer",
  planCardExpand: "Expand plan",
  planCardCollapse: "Collapse plan",
  planImplementationLead: "Implement this plan?",
  planImplementationConfirm: "Yes, implement this plan",
  planImplementationFeedbackPlaceholder:
    "No — tell the agent how to adjust the approach",
  planImplementationSend: "Send",
  planImplementationSkip: "Stay in Plan Mode",
  noRunningResponse: "No running response to stop.",
  composerTextMenu: "Composer text actions",
  pastedTextFilesHeader: "Referenced pasted text files:",
  pastedTextFileLine:
    '- pasted text file "{{preview}}": {{path}}. Read this file before continuing.',
  pastedTextAttachmentTitle: "Pasted text",
  pastedTextAttachmentFailed: "Pasted text couldn't be saved",
  pastedTextRestoreToComposer: "Show in text field",
  copyMessage: "Copy message",
  selectedTextFragment: "1 selected text fragment",
  selectedTextFragments: "{{count}} selected text fragments",
  forkThroughTurn: "Fork through this turn",
  forkThroughTurnPending: "Forking through this turn",
  continuedFromTask: "Continued from task",
  sourceConversationNotFound: "Original conversation not found",
  copyImage: "Copy image",
  editRetryEditMessage: "Edit message",
  editRetryCancel: "Cancel",
  editRetrySubmit: "Save and retry",
  editRetryProcessing: "Updating conversation history and retrying…",
  editRetryNeedsAction:
    "Conversation history was updated, but the edited message still needs recovery",
  editRetryReconcile: "Reconcile",
  editRetryRetryReplacement: "Retry message",
  messageCopied: "Copied",
  promptTipsPrefix: "Tips: ",
  reviewPicker: {
    title: "Code review",
    targetLabel: "What to review",
    searchPlaceholder: "Search",
    noResults: "No matches",
    uncommitted: "Uncommitted changes",
    baseBranch: "Compare against a branch",
    commit: "A specific commit",
    custom: "Custom instructions",
    branchLabel: "Base branch",
    branchPlaceholder: "Select a branch",
    branchLoading: "Loading branches…",
    branchEmpty: "No branches found",
    commitPlaceholder: "Commit SHA",
    customPlaceholder: "Describe what to review",
    submit: "Start review",
    cancel: "Cancel"
  },
  promptTips: {
    setWorkspace: {
      label: "Set the workspace",
      prompt:
        "Let the Agent know where to read files, run commands, and understand code."
    },
    useIssue: {
      label: "Use Tasks well",
      prompt:
        "Put requirements, constraints, and acceptance criteria in a Task so the Agent can work toward a clear target."
    },
    mapCurrentState: {
      label: "Map the current state",
      prompt:
        "When the next step is unclear, ask the Agent to summarize status, risks, and what to do next."
    },
    continueRecentSession: {
      label: "Continue recent work",
      prompt:
        "When resuming, have the Agent recap recent progress, unfinished work, and blockers first."
    },
    referenceOtherAgents: {
      label: "Reference other Agent conversations",
      prompt: "Make context handoffs more complete and reduce lost details."
    },
    controlPermissions: {
      label: "Control permissions",
      prompt:
        "Use Ask for approval when you want caution, then switch to higher access once file changes are expected."
    }
  },
  empty: "What can {{provider}} help you with?",
  ...enAgentGuiHomeSuggestions,
  conversations: "Sessions",
  newConversation: "New session",
  agentConfig: "Check & Settings",
  agentSettingsMenu: "Settings",
  agentEnvSetup: "Environment Check",
  agentsEmpty: "No agents are available",
  conversationFilterAll: "All",
  ...enAgentGuiProviderIdentity,
  providerSwitchLabel: "Switch provider",
  sharedAgentOwnerSeparator: "'s ",
  handoffConversation: "Handoff",
  handoffConversationTooltip: "Hand off to another agent",
  handoffConversationMenu: "Choose an agent for handoff",
  handoffTargetDeviceSource: "From {{device}}",
  handoffTargetSelf: "My Agent",
  handoffTargetShared: "Shared Agent",
  startConversation: "Start session",
  selectConversation: "Select a session",
  loadingConversation: "Loading session...",
  scrollToBottom: "Scroll to bottom",
  conversationUnavailable: "Session unavailable.",
  contextPickerBrowseHint: "Search workspace files based on your input",
  contextPickerBrowseFileHint:
    "No opened or Agent-generated files yet. Type a file name to search your computer.",
  contextPickerBrowseAgentHint: "Type to search agents",
  contextPickerBrowseAppHint: "Type to search apps",
  contextPickerBrowseSessionHint:
    "Type to search agent sessions that I started",
  contextPickerBrowseCollabHint: "Type to search teammate and agent sessions",
  contextPickerBrowseIssueHint: "Type to search tasks in the current room",
  workspaceAppFactoryMentionFallback: "Create app",
  contextPickerExpandMore: "Show {{count}} more",
  contextPickerLoadMoreLoading: "Loading…",
  contextPickerLoadMoreRetry: "Load failed. Retry",
  contextPickerCategoryFileDescription: "Search Files and folders",
  contextPickerCategorySessionDescription: "Find agent sessions that I started",
  contextPickerCategoryCollabDescription:
    "Browse sessions between teammates and agents",
  contextPickerCategoryTaskDescription: "Find tasks in the current room",
  searchPlaceholder: "Search sessions",
  sectionToday: "Today",
  sectionYesterday: "Yesterday",
  sectionEarlier: "Earlier",
  projectSectionEdit: "New session",
  projectSectionMoreActions: "Project actions",
  projectSectionViewFiles: "Open folder",
  pinProject: "Pin project",
  unpinProject: "Unpin project",
  pinnedProjectAccessibleName: "Pinned project: {{project}}",
  projectRailCreateProject: "New project",
  projectRailLinkExistingProject: "Link existing project folder",
  removeProject: "Remove",
  removeProjectConfirmDescription:
    "This will remove “{{project}}” and all its sessions from this list. Local files are not deleted. Continue?",
  removeProjectConfirmTitle: "Remove project?",
  batchDeleteProjectSessions: "Batch delete sessions",
  batchDeleteProjectSessionsTitle: "Delete project sessions?",
  batchDeleteProjectSessionsBody:
    "This will delete {{count}} sessions in “{{project}}”. Deleted sessions cannot be recovered.",
  batchDeleteProjectSessionsConfirm: "Delete sessions",
  conversationsSectionMoreActions: "Conversation actions",
  batchDeleteConversations: "Batch delete conversations",
  batchDeleteConversationsTitle: "Delete conversations?",
  batchDeleteConversationsBody:
    "This will delete {{count}} conversations. Deleted conversations cannot be recovered.",
  batchDeleteConversationsConfirm: "Delete conversations",
  runtimeSessionOnly: "Only runtime sessions appear here.",
  approvalRequired: "{{provider}} requests your authorization.",
  fileChangeApprovalRequired:
    "{{provider}} requests your authorization to edit files.",
  approvalUnavailable: "No choices are available.",
  approvalOptions: {
    allowOnce: "Yes, proceed",
    allowForSession: "Yes, for this session",
    allowAlways: "Yes, and don't ask again",
    allowAlwaysForCommandPrefix:
      "Yes, and don't ask again for commands that start with `{{command}}`",
    allowAlwaysForCommandPrefixLead:
      "Yes, and don't ask again for commands that start with",
    allowAlwaysForScope: "Yes, and don't ask again for {{scope}}",
    alwaysAllowScope: "Always allow {{scope}}",
    bypassPermissions: "Yes, and bypass permissions",
    autoMode: 'Yes, and use "auto" mode',
    acceptEdits: "Yes, and auto-accept edits",
    manualApproval: "Yes, and manually approve edits",
    rejectOnce: "No, don't run",
    rejectAlways: "No, and don't ask again",
    rejectAndStopTurn: "Deny and stop the current response",
    rejectWithFollowUp: "No, then send new instructions"
  },
  authRequired: "Authentication required",
  authLogin: "Sign in",
  activatingSession: "Connecting session...",
  cancellingSession: "Cancelling...",
  retryActivation: "Retry",
  continueInNewConversation: "New session",
  processing: "Planning next moves",
  turnProcessedSeconds: "Processed for {{seconds}}s",
  turnProcessedMinutes: "Processed for {{minutes}}m",
  turnProcessedMinutesSeconds: "Processed for {{minutes}}m {{seconds}}s",
  turnPeerDeviceOfflinePendingSync:
    "Other device offline · Progress pending sync",
  turnPeerDeviceProgressSynchronizing:
    "Synchronizing progress from other device…",
  turnTotalSeconds: "Total {{seconds}}s",
  turnTotalMinutes: "Total {{minutes}}m",
  turnTotalMinutesSeconds: "Total {{minutes}}m {{seconds}}s",
  expandTurnWork: "Expand task details",
  collapseTurnWork: "Collapse task details",
  agentTargetRequired:
    "Select an available agent target before starting a session.",
  sessionActivationFailed: "The agent session could not be started.",
  sessionNoLongerAvailable:
    "The previous agent session is no longer available.",
  promptImagesUnsupported:
    "This agent does not support image input with the current model.",
  ...enAgentGuiRuntimeNotices,
  tuttiModeCheckpointWakeTaskSettled: "A task finished — review needed",
  tuttiModeCheckpointWakeTaskFailed: "A task failed — review needed",
  tuttiModeCheckpointWakeTaskCanceled: "A task was canceled — review needed",
  tuttiModeCheckpointWakeGoalReview: "Final goal review needed",
  tuttiModeCheckpointWakeInitialSchedule: "Ready to schedule the next tasks",
  tuttiModeCheckpointWakeDefault: "Execution checkpoint needs your review",
  tuttiModeCheckpointWakeIssue: "Issue {{issue}}",
  tuttiModeCheckpointWakeExpand: "Show full prompt",
  tuttiModeCheckpointWakeCollapse: "Hide full prompt",
  tuttiModePlanIssueLinkCreated: "Issue created from this plan",
  ...enAgentGuiModelPlans,
  turnSummary: "Changed files",
  userMessageLocator: "User messages",
  turnSummaryFilesChanged: "{{count}} files changed",
  turnSummaryModified: "{{count}} modified",
  turnSummaryCreated: "{{count}} new",
  turnSummaryModifiedTag: "Modified",
  turnSummaryCreatedTag: "New",
  turnSummaryViaTool: "via {{tool}}",
  turnSummaryBefore: "Before",
  turnSummaryAfter: "After",
  codeBlockEmptyContent: "(empty)",
  turnSummaryOpenFile: "Open",
  turnSummaryUndo: "Undo",
  turnSummaryReapply: "Reapply",
  turnSummaryCheckingGit: "Checking Git repository...",
  turnSummaryGitRequired: "This directory is not a Git repository",
  turnSummaryPatchUnavailable:
    "No reversible patch data is available for this change",
  turnSummaryInvalidPatch:
    "The recorded patch is invalid and cannot be safely applied",
  turnSummaryPatchDoesNotApply:
    "The file changed after this edit and cannot be safely restored",
  turnSummaryUndoFailed: "Failed to undo changes",
  turnSummaryReapplyFailed: "Failed to reapply changes",
  turnSummaryShowMoreFiles: "Show {{count}} more file",
  turnSummaryShowFewerFiles: "Show fewer files",
  planLead:
    "Exit planning and start implementing. How should permissions work?",
  planModes: {
    acceptEdits: {
      label: "Accept edits",
      description: "Auto-approve file edits"
    },
    askFirst: {
      label: "Ask for approval",
      description: "Prompt before each tool"
    },
    allowAll: {
      label: "Allow all",
      description: "Do not prompt for tools"
    },
    auto: {
      label: "Auto",
      description: "Let the agent choose when to ask"
    }
  },
  stayInPlan: "Keep planning",
  sendFeedback: "Send feedback and keep planning",
  feedbackPlaceholder: "Give feedback to refine the plan...",
  previousQuestion: "Back",
  nextQuestion: "Next",
  submitAnswers: "Submit answers",
  answerPlaceholder: "Add details for the agent...",
  waitingForAnswer: "Waiting for your answer...",
  shortcutEnter: "Enter",
  shortcutCmdEnter: "Cmd + Enter",
  shortcutCtrEnter: "Ctr + Enter",
  openConversationWindow: "Open session in new window",
  showMoreConversations: "Show more",
  showLessConversations: "Show less",
  deleteSession: "Delete session",
  pinSession: "Pin session",
  ...enAgentGuiSessionActions,
  renameSession: "Rename session",
  renameSessionTitle: "Rename conversation",
  renameSessionDescription: "Keep it short and easy to recognize.",
  renameSessionPlaceholder: "Conversation title",
  renameSessionSave: "Save",
  unpinSession: "Unpin session",
  deleteSessionTitle: "Delete session?",
  deleteSessionBody:
    "This session cannot be recovered after deletion. It will no longer appear in the session list, session timeline, room timeline, or room status.",
  deleteSessionConfirm: "Delete session",
  collapseConversationRail: "Hide sidebar",
  expandConversationRail: "Show sidebar",
  relativeTimeJustNow: "just now",
  relativeTimeMinutes: "{{count}} min",
  relativeTimeHours: "{{count}} h",
  relativeTimeDays: "{{count}} d",
  relativeTimeMonths: "{{count}} mo",
  relativeTimeYears: "{{count}} y",
  ...enAgentGuiSlashPalette,
  slashCommandCompactLabel: "compact",
  slashCommandContextLabel: "context",
  slashCommandFastLabel: "fast",

  slashCommandInitLabel: "init",
  slashCommandPlanLabel: "plan",
  slashCommandReviewLabel: "review",
  slashCommandStatusLabel: "status",
  slashCommandUsageLabel: "usage",
  slashCommandCompactDescription: "Compact the conversation context.",
  slashCommandContextDescription: "Show the current context snapshot.",
  slashCommandFastDescription: "Toggle fast response mode.",

  slashCommandInitDescription: "Initialize repository guidance files.",
  slashCommandPlanDescription: "Toggle plan mode.",
  slashCommandReviewDescription: "Run a code review.",
  slashCommandStatusDescription: "Show session status and context usage.",
  slashCommandUsageDescription: "Show context and quota usage.",
  browserUseCapabilityLabel: "Browser",
  browserUseCapabilityDescription: "Let the agent use a browser.",
  browserUseCapabilityDescriptionAutoConnect:
    "Current mode: reuse your signed-in Chrome.",
  browserUseCapabilityDescriptionIsolated:
    "Current mode: use an isolated browser.",
  browserUseCapabilitySettingsLabel: "Browser settings",
  browserUseCapabilitySettingsDescription:
    "Configure the browser the agent can use.",
  capabilityInlineSettingsLabel: "Settings",
  computerUseCapabilityLabel: "Computer",
  computerUseCapabilityDescription: "Let the agent control your desktop.",
  computerUseCapabilitySetupRequiredDescription:
    "Not installed. Press Enter to open setup.",
  computerUseCapabilityAuthorizationRequiredDescription:
    "Authorization required. Press Enter to open setup.",
  computerUseCapabilityAuthorizationUnknownDescription:
    "Authorization status unknown. Press Enter to open setup.",
  computerUseCapabilitySettingsLabel: "Computer use setup",
  computerUseCapabilitySettingsDescription:
    "Install, remove, or grant computer access.",
  fileMentionPalette: "References and agents",
  fileMentionLoading: "Searching...",
  fileMentionEmpty: "Search workspace files based on your input",
  fileMentionError: "Unable to search Files.",
  fileMentionTabHint: "Tab / ←→ switch category | ↑↓ switch selection",
  fileDropHint: "Drop files to add them to the session",
  composerFileFolderUnsupported: "Folders cannot be attached here",
  composerFileTooLarge: "File is too large",
  composerFilePreparationFailed: "File preparation failed",
  composerFileStillPreparing:
    "This attachment is still being prepared. Open it after preparation finishes.",
  composerFileOpenFailed:
    "This attachment failed to prepare. Remove it and add the file again.",
  composerFileOpenUnavailable:
    "This attachment has no openable path yet. Remove it and add the file again.",
  mentionPalette: "Reference or Invoke",
  addReference: "Add reference",
  addContent: "Add files and more",
  ...enAgentGuiAddContent,
  quickPrompts: enAgentGuiQuickPrompts,
  referenceWorkspaceFiles: "Reference workspace files",
  ...enAgentGuiReferencePicker,
  ...enAgentGuiProjectLaunch,
  fileMentionEnterFolder: "Enter folder",
  fileMentionSwitchCategory: "Switch category",
  fileMentionNavigateHierarchy: "Enter/leave folder",
  fileMentionSwitchSelection: "Switch selection",
  mentionFilterFile: "Files",
  mentionFilterApp: "Apps",
  mentionFilterAgent: "Agents",
  provenanceFilterAllAgents: "All agents",
  provenanceFilterAllMembers: "All members",
  provenanceFilterAllSources: "All sources",
  provenanceFilterAgents: "Agents",
  provenanceFilterFilteredSources: "Filtered sources",
  provenanceFilterMembers: "Members",
  mentionFilterSession: "Sessions",
  mentionFilterIssue: "Tasks",
  mentionKindAgent: "Agent",
  mentionKindApp: "App",
  mentionKindAppFactory: "App Factory",
  mentionKindFile: "File",
  mentionKindIssue: "Task",
  mentionKindReference: "Reference",
  mentionKindSession: "Session",
  mentionGroupFiles: "Files",
  mentionGroupOpenedFiles: "Files I opened",
  mentionGroupAgentGeneratedFiles: "Recent files generated by Agent",
  mentionGroupApps: "Apps",
  mentionGroupAgents: "Agents",
  mentionGroupMySessions: "My sessions",
  mentionGroupIssues: "Tasks",
  mentionEmptyMySessions: "No sessions yet",
  mentionEmptyApps: "No apps yet",
  mentionEmptyAgents: "No agents available",
  mentionEmptyIssues: "No tasks yet",
  mentionEmptyDockFiles:
    "No open files in the dock yet. Type to search workspace files.",
  mentionEmptyAgentGeneratedFiles: "No files generated by Agent yet",
  mentionFolderBack: "Back",
  mentionFolderChildCount: "Contains {{count}} items",
  mentionAgentTargetAvailable: "Available",
  mentionAgentTargetUnavailable: "Unavailable",
  mentionNoMatchingFiles: "No matching files",
  mentionOpenReferences: "View output",
  issueRunPrompt: {
    currentWorkingDirectoryLabel: "Current working directory",
    executionRequirementsLabel: "Execution requirements",
    intro: "You are handling a task.",
    issueContentLabel: "Task content",
    issueTitleLabel: "Task title",
    missingContent: "(No additional content)",
    noReferences: "- (No references)",
    referencesLabel: "References",
    requirementNoOtherOutputDir:
      "3. Do not write final deliverables to any other directory.",
    requirementStayInWorkspace:
      "1. Work under {{workspaceRoot}}; do not switch to unrelated directories.",
    requirementSummaryOutput:
      "2. Unless the user specifies another location, write at least docs/tutti/task_summary_{{issueId}}.md with the result, changes, and conclusion.",
    taskContentLabel: "Task content",
    taskTitleLabel: "Task title"
  },
  syncPending: "Saved locally, syncing to cloud",
  syncSynced: "Synced to cloud",
  syncFailed: "Cloud sync failed",
  ...enAgentGuiSide,
  ...enAgentGuiCollaboration
} as const;
