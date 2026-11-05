import type {
  AgentActivityMessage,
  AgentActivitySession,
  AgentActivitySnapshot,
  CanonicalAgentSession
} from "@tutti-os/agent-activity-core";
import { selectRootAgentActivitySessions } from "@tutti-os/agent-activity-core";
import type { AgentConversationVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import { projectAgentConversationVM } from "../../../shared/agentConversation/projection/agentConversationProjection";
import { projectAgentSessionEventsToTimelineItems } from "../../../shared/agentConversation/projection/agentSessionEventProjection";
import {
  attachChildSessionLanesToConversationVM,
  buildChildSessionLanesByParentToolCallId
} from "../../../shared/agentConversation/projection/childSessionLanes";
import {
  resolveAgentGUIConversationTitle,
  resolveAgentGUIExplicitConversationTitle,
  resolveAgentGUIExplicitConversationTitleFromMessages,
  resolveAgentGUIProviderIdentity,
  type AgentGUIConversationTitleFallback
} from "../../../shared/agentConversationTitleProjection.ts";
import type { AgentSessionEvent } from "../../../shared/agentSessionTypes";
import {
  buildWorkspaceAgentActivityListViewModel,
  type WorkspaceAgentActivityCard,
  type WorkspaceAgentActivityStatus
} from "../../../shared/workspaceAgentActivityListViewModel";
import {
  buildWorkspaceAgentSessionDetailViewModel,
  type WorkspaceAgentSessionDetailViewModel
} from "../../../shared/workspaceAgentSessionDetailViewModel";
import { resolveWorkspaceAgentSessionSortTimeUnixMs } from "../../../shared/workspaceAgentSessionSortTime";
import type { WorkspaceAgentActivityTimelineItem } from "../../../shared/workspaceAgentTimelineTypes";
import {
  filterAgentGUIConversationSummaries,
  normalizeAgentGUIConversationFilter
} from "./agentGuiConversationFilter";
import {
  createAgentGUIConversationProjectResolver,
  type AgentGUIConversationProjectResolver,
  type AgentGUIConversationProjectSummary,
  type AgentGUIConversationUserProject
} from "./agentGuiConversationProjectResolver";
import type {
  AgentGUIConversationProjectResolutionContext,
  AgentGUIConversationProjectionSource,
  AgentGUIConversationStatus,
  AgentGUIConversationSummary,
  AgentGUITimelineRow,
  BuildAgentGUIConversationsInput
} from "./agentGuiConversationTypes";
import { sortTimelineRows } from "./agentGuiInteractiveProjection";
import { mergeTimelineItems } from "./agentGuiTimelineMerge";
import {
  firstUserMessageTitleFromTimelineItems,
  timelineRowsFromActivityTimelineItems,
  timelineSessionFromItems,
  workspaceAgentMessagesFromTimelineItems
} from "./agentGuiTimelineProjection";

export {
  resolveAgentGUIConversationProjectBySectionKey,
  resolveAgentGUISelectedUserProject
} from "./agentGuiConversationProjectResolver";
export type {
  AgentGUIConversationProjectSummary,
  AgentGUIConversationUserProject
} from "./agentGuiConversationProjectResolver";
export {
  AGENT_GUI_RUNTIME_SESSION_ORIGIN,
  resolveAgentGUIConversationSortTimeUnixMs
} from "./agentGuiConversationTypes";
export type {
  AgentGUIApprovalOption,
  AgentGUIApprovalRequest,
  AgentGUIConversationProjectionSource,
  AgentGUIConversationStatus,
  AgentGUIConversationSummary,
  AgentGUIInteractivePrompt,
  AgentGUIInteractiveQuestion,
  AgentGUIInteractiveQuestionOption,
  AgentGUITimelineRow,
  BuildAgentGUIConversationsInput
} from "./agentGuiConversationTypes";
export function buildAgentGUIConversationSummaries({
  conversationFilter,
  snapshot,
  provider,
  sessionMessagesById,
  userProjects = []
}: BuildAgentGUIConversationsInput): AgentGUIConversationSummary[] {
  const rootSessionIdsAwaitingUserAction = new Set(
    snapshot.sessions.flatMap((session) => {
      if (session.pendingInteractions.length === 0) return [];
      const rootSessionId =
        session.kind === "child"
          ? session.rootAgentSessionId
          : session.agentSessionId;
      return rootSessionId ? [rootSessionId] : [];
    })
  );
  const runtimeSnapshot = filterConversationServicesSnapshot(snapshot);
  const sessionsById = new Map(
    selectRootAgentActivitySessions(runtimeSnapshot).map((session) => [
      session.agentSessionId,
      session
    ])
  );
  const projectResolver =
    createAgentGUIConversationProjectResolver(userProjects);
  const conversations = buildWorkspaceAgentActivityListViewModel(
    runtimeSnapshot,
    {
      sessionMessagesById
    }
  ).activities.map((activity) =>
    conversationSummaryFromActivity(
      activity,
      sessionsById.get(activity.sessionId),
      { projectResolver },
      rootSessionIdsAwaitingUserAction
    )
  );
  if (conversationFilter) {
    return filterAgentGUIConversationSummaries(
      conversations,
      normalizeAgentGUIConversationFilter(conversationFilter)
    );
  }
  return conversations.filter(
    (conversation) =>
      conversation.provider === provider || conversation.provider === "unknown"
  );
}

export function selectAgentGUIConversationId(
  conversations: readonly AgentGUIConversationSummary[],
  preferredSessionId: string | null | undefined
): string | null {
  const preferred = preferredSessionId?.trim();
  if (
    preferred &&
    conversations.some((conversation) => conversation.id === preferred)
  ) {
    return preferred;
  }
  return conversations[0]?.id ?? null;
}

export function buildAgentGUITimelineRows(
  timelineItems: readonly WorkspaceAgentActivityTimelineItem[]
): AgentGUITimelineRow[] {
  return timelineRowsFromActivityTimelineItems(
    mergeTimelineItems([], timelineItems)
  );
}

export function buildAgentGUITimelineItems(
  timelineItems: readonly WorkspaceAgentActivityTimelineItem[]
): WorkspaceAgentActivityTimelineItem[] {
  return mergeTimelineItems([], timelineItems);
}

export function buildAgentGUITimelineRowsFromSessionEvents(
  events: readonly AgentSessionEvent[]
): AgentGUITimelineRow[] {
  const timelineItems = buildAgentGUITimelineItemsFromSessionEvents(events);
  return timelineRowsFromActivityTimelineItems(timelineItems);
}

export function buildAgentGUITimelineItemsFromSessionEvents(
  events: readonly AgentSessionEvent[]
): WorkspaceAgentActivityTimelineItem[] {
  return projectAgentSessionEventsToTimelineItems(events);
}

export function mergeAgentGUITimelineRows(
  left: readonly AgentGUITimelineRow[],
  right: readonly AgentGUITimelineRow[]
): AgentGUITimelineRow[] {
  const byID = new Map<string, AgentGUITimelineRow>();
  for (const row of [...left, ...right]) {
    byID.set(row.id, row);
  }
  return sortTimelineRows([...byID.values()]);
}

export function mergeAgentGUITimelineItems(
  left: readonly WorkspaceAgentActivityTimelineItem[],
  right: readonly WorkspaceAgentActivityTimelineItem[]
): WorkspaceAgentActivityTimelineItem[] {
  return mergeTimelineItems(left, right);
}

export function buildAgentGUIConversationDetail({
  timelineItems,
  conversation,
  canonicalSession = null,
  workspaceRoot = null
}: {
  timelineItems: readonly WorkspaceAgentActivityTimelineItem[];
  conversation: AgentGUIConversationProjectionSource;
  canonicalSession?: AgentActivitySession | null;
  workspaceRoot?: string | null;
}): WorkspaceAgentSessionDetailViewModel | null {
  const session =
    canonicalSession?.agentSessionId === conversation.id
      ? canonicalSession
      : timelineSessionFromItems(timelineItems, conversation);
  const activity =
    buildWorkspaceAgentActivityListViewModel(
      {
        presences: [],
        sessions: [session]
      },
      timelineItems.length > 0
        ? {
            sessionMessagesById: {
              [session.agentSessionId]:
                workspaceAgentMessagesFromTimelineItems(timelineItems)
            }
          }
        : {}
    ).activities[0] ?? null;
  if (!activity) {
    return null;
  }
  const resolvedActivity = activityWithExplicitConversationTitle(
    activity,
    conversation
  );
  const detail = buildWorkspaceAgentSessionDetailViewModel({
    activity: resolvedActivity,
    session,
    sessionTurns: conversation.sessionTurns,
    timelineItems: [...timelineItems],
    workspaceRoot
  });
  return detail;
}

export function buildAgentGUIConversationModels({
  timelineItems,
  conversation,
  canonicalSession = null,
  childSessions = [],
  childMessagesBySessionId = {},
  workspaceRoot = null,
  avoidGroupingEdits = false
}: {
  timelineItems: readonly WorkspaceAgentActivityTimelineItem[];
  conversation: AgentGUIConversationProjectionSource;
  canonicalSession?: AgentActivitySession | null;
  childSessions?: readonly AgentActivitySession[];
  childMessagesBySessionId?: Readonly<
    Record<string, readonly AgentActivityMessage[] | undefined>
  >;
  workspaceRoot?: string | null;
  avoidGroupingEdits?: boolean;
}): {
  conversation: AgentConversationVM | null;
  detail: WorkspaceAgentSessionDetailViewModel | null;
} {
  const detail = buildAgentGUIConversationDetail({
    timelineItems,
    conversation,
    canonicalSession,
    workspaceRoot
  });
  if (!detail) {
    return { conversation: null, detail: null };
  }
  const childSessionLanesByParentToolCallId =
    buildChildSessionLanesByParentToolCallId({
      rootSession: detail.session,
      rootTimelineItems: timelineItems,
      childSessions,
      messagesBySessionId: childMessagesBySessionId
    });
  return {
    conversation: attachChildSessionLanesToConversationVM(
      projectAgentConversationVM(detail, { avoidGroupingEdits }),
      childSessionLanesByParentToolCallId
    ),
    detail
  };
}

export function buildAgentGUIConversationVM({
  timelineItems,
  conversation,
  workspaceRoot = null,
  avoidGroupingEdits = false
}: {
  timelineItems: readonly WorkspaceAgentActivityTimelineItem[];
  conversation: AgentGUIConversationProjectionSource;
  workspaceRoot?: string | null;
  avoidGroupingEdits?: boolean;
}): AgentConversationVM | null {
  return buildAgentGUIConversationModels({
    timelineItems,
    conversation,
    workspaceRoot,
    avoidGroupingEdits
  }).conversation;
}

export function mergeTimelineItemsByEventID(
  previous: readonly WorkspaceAgentActivityTimelineItem[],
  incoming: readonly WorkspaceAgentActivityTimelineItem[]
): WorkspaceAgentActivityTimelineItem[] {
  return mergeTimelineItems(previous, incoming);
}

export function conversationSummaryFromAgentSession(
  session: CanonicalAgentSession,
  options: {
    needsUserAction?: boolean;
    userProjects?: readonly AgentGUIConversationUserProject[];
  } = {}
): AgentGUIConversationSummary {
  const projectResolver = createAgentGUIConversationProjectResolver(
    options.userProjects ?? []
  );
  const provider = resolveAgentGUIProviderIdentity({
    sessionProvider: session.provider
  });
  const { title, titleFallback } = resolveAgentGUIConversationTitle(
    session.title
  );
  return {
    id: session.agentSessionId.trim(),
    userId: "",
    agentTargetId: session.agentTargetId ?? null,
    provider,
    resumable: session.resumable,
    title,
    titleFallback,
    status: conversationStatusFromActivity(
      session.activeTurnId ? "working" : "idle"
    ),
    cwd: session.cwd?.trim() ?? "",
    isolation: session.isolation,
    railSectionKey: session.railSectionKey,
    project: resolveConversationProject(session, projectResolver),
    pinnedAtUnixMs: session.pinnedAtUnixMs ?? null,
    needsUserAction: options.needsUserAction ?? false,
    ...(session.visible === false ? { hiddenFromRail: true } : {}),
    sortTimeUnixMs: resolveWorkspaceAgentSessionSortTimeUnixMs(session),
    updatedAtUnixMs:
      session.updatedAtUnixMs || session.createdAtUnixMs || Date.now()
  };
}

export function applyAgentGUIConversationProjects(
  conversations: readonly AgentGUIConversationSummary[],
  userProjects: readonly AgentGUIConversationUserProject[] = []
): AgentGUIConversationSummary[] {
  let changed = false;
  const projectResolver =
    createAgentGUIConversationProjectResolver(userProjects);
  const next = conversations.map((conversation) => {
    const project = projectResolver.resolveSectionKey(
      conversation.railSectionKey
    );
    if (isSameAgentGUIConversationProject(conversation.project, project)) {
      return conversation;
    }
    changed = true;
    return {
      ...conversation,
      project
    };
  });
  return changed ? next : (conversations as AgentGUIConversationSummary[]);
}

export function resolveAgentGUIConversationTitleFromTimelineItems({
  timelineItems,
  conversation
}: {
  timelineItems: readonly WorkspaceAgentActivityTimelineItem[];
  conversation: AgentGUIConversationSummary;
}): {
  title: string;
  titleFallback: AgentGUIConversationTitleFallback;
} | null {
  if (resolveAgentGUIExplicitConversationTitle(conversation) !== null) {
    return null;
  }
  const userMessageTitle =
    firstUserMessageTitleFromTimelineItems(timelineItems);
  if (!userMessageTitle) {
    return null;
  }
  return resolveAgentGUIConversationTitle(userMessageTitle);
}

export function resolveAgentGUIConversationTitleFromMessages({
  messages,
  conversation
}: {
  messages: readonly AgentActivityMessage[];
  conversation: AgentGUIConversationSummary;
}): {
  title: string;
  titleFallback: AgentGUIConversationTitleFallback;
} | null {
  if (resolveAgentGUIExplicitConversationTitle(conversation) !== null) {
    return null;
  }
  const projectedTitle = resolveAgentGUIExplicitConversationTitleFromMessages({
    messages,
    provider: conversation.provider,
    title: conversation.title
  });
  if (!projectedTitle) {
    return null;
  }
  return resolveAgentGUIConversationTitle(projectedTitle);
}

function filterConversationServicesSnapshot(
  snapshot: AgentActivitySnapshot
): AgentActivitySnapshot {
  return {
    ...snapshot,
    sessions: selectRootAgentActivitySessions(snapshot).filter((session) =>
      isConversationServicesSession(session)
    )
  };
}

function isConversationServicesSession(session: AgentActivitySession): boolean {
  return session.agentSessionId.trim().length > 0;
}

function conversationSummaryFromActivity(
  activity: WorkspaceAgentActivityCard,
  session: AgentActivitySession | undefined,
  options: AgentGUIConversationProjectResolutionContext,
  rootSessionIdsAwaitingUserAction: ReadonlySet<string>
): AgentGUIConversationSummary {
  const status = conversationStatusFromActivity(activity.status);
  const provider = resolveAgentGUIProviderIdentity({
    workspaceSessionProvider: session?.provider,
    conversationProvider: activity.agentProvider
  });
  const explicitSessionTitle = session
    ? resolveAgentGUIExplicitConversationTitle({
        provider,
        title: session.title ?? "",
        titleFallback: null
      })
    : null;
  const { title, titleFallback } = resolveAgentGUIConversationTitle(
    explicitSessionTitle ?? (session ? "" : activity.title)
  );
  return {
    id: activity.sessionId,
    userId: session?.userId?.trim() ?? "",
    agentTargetId: session?.agentTargetId ?? null,
    provider,
    resumable: session?.resumable,
    title,
    titleFallback,
    status,
    cwd: session?.cwd.trim() ?? "",
    isolation: session?.isolation ?? null,
    ...(session ? { railSectionKey: session.railSectionKey } : {}),
    project: resolveConversationProject(session, options.projectResolver),
    pinnedAtUnixMs: session?.pinnedAtUnixMs ?? null,
    needsUserAction: rootSessionIdsAwaitingUserAction.has(activity.sessionId),
    sortTimeUnixMs: activity.sortTimeUnixMs,
    updatedAtUnixMs: session?.updatedAtUnixMs || activity.sortTimeUnixMs || 0,
    activeTurn: session?.activeTurn ?? null,
    ...(isImportedWorkspaceAgentSession(session) ? { isImported: true } : {})
  };
}

function resolveConversationProject(
  session: AgentActivitySession | CanonicalAgentSession | undefined,
  projectResolver: AgentGUIConversationProjectResolver
): AgentGUIConversationProjectSummary | null {
  return projectResolver.resolveSectionKey(session?.railSectionKey);
}

function isImportedWorkspaceAgentSession(
  session: AgentActivitySession | undefined
): boolean {
  return session?.imported === true;
}

function isSameAgentGUIConversationProject(
  left: AgentGUIConversationProjectSummary | null | undefined,
  right: AgentGUIConversationProjectSummary | null
): boolean {
  if (left === null && right === null) {
    return true;
  }
  if (!left || !right) {
    return false;
  }
  return (
    left.id === right.id &&
    left.path === right.path &&
    left.label === right.label &&
    left.createdAtUnixMs === right.createdAtUnixMs &&
    left.updatedAtUnixMs === right.updatedAtUnixMs &&
    left.lastUsedAtUnixMs === right.lastUsedAtUnixMs
  );
}

function activityWithExplicitConversationTitle(
  activity: WorkspaceAgentActivityCard,
  conversation: AgentGUIConversationProjectionSource
): WorkspaceAgentActivityCard {
  const explicitTitle = resolveAgentGUIExplicitConversationTitle(conversation);
  const status = conversation.status === "ready" ? "idle" : conversation.status;
  if (
    (!explicitTitle || activity.title === explicitTitle) &&
    activity.status === status
  ) {
    return activity;
  }
  return {
    ...activity,
    status,
    ...(explicitTitle ? { title: explicitTitle } : {})
  };
}

function conversationStatusFromActivity(
  status: WorkspaceAgentActivityStatus
): AgentGUIConversationStatus {
  switch (status) {
    case "working":
      return "working";
    case "waiting":
      return "waiting";
    case "failed":
      return "failed";
    case "completed":
      return "completed";
    case "canceled":
      return "canceled";
    case "idle":
    default:
      return "ready";
  }
}
