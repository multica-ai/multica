"use client";

import { useEffect, useRef } from "react";
import { useQueryClient, type InfiniteData, type QueryClient } from "@tanstack/react-query";
import type { WSClient } from "../api/ws-client";
import type { StoreApi, UseBoundStore } from "zustand";
import type { AuthState } from "../auth/store";
import { createLogger } from "../logger";
import { clearWorkspaceStorage } from "../platform/storage-cleanup";
import { defaultStorage } from "../platform/storage";
import { getCurrentWsId, getCurrentSlug } from "../platform/workspace-storage";
import { issueKeys } from "../issues/queries";
import { projectKeys } from "../projects/queries";
import { pinKeys } from "../pins/queries";
import { autopilotKeys } from "../autopilots/queries";
import { runtimeKeys } from "../runtimes/queries";
import { labelKeys } from "../labels/queries";
import {
  agentTaskSnapshotKeys,
  agentActivityKeys,
  agentRunCountsKeys,
  agentTasksKeys,
} from "../agents/queries";
import { githubKeys } from "../github/queries";
import { larkKeys } from "../lark/queries";
import { slackKeys } from "../slack/queries";
import {
  onIssueCreated,
  onIssueUpdated,
  onIssueDeleted,
  onIssueLabelsChanged,
  onIssueMetadataChanged,
} from "../issues/ws-updaters";
import { onInboxNew, onInboxInvalidate, onInboxIssueStatusChanged, onInboxIssueDeleted, onInboxSummaryInvalidate } from "../inbox/ws-updaters";
import { inboxKeys } from "../inbox/queries";
import {
  notificationPreferenceOptions,
  notificationPreferenceKeys,
} from "../notification-preferences/queries";
import { workspaceKeys, workspaceListOptions } from "../workspace/queries";
import {
  showWebNotification,
  type SystemNotificationPayload,
} from "../platform/system-notification";
import type { Workspace } from "../types/workspace";
import { chatKeys, mergeTaskMessagesBySeq } from "../chat/queries";
import { useChatStore } from "../chat";
import { resolvePostAuthDestination, useHasOnboarded } from "../paths";
import type {
  MemberAddedPayload,
  WorkspaceDeletedPayload,
  WorkspaceUpdatedPayload,
  MemberRemovedPayload,
  IssueUpdatedPayload,
  IssueCreatedPayload,
  IssueDeletedPayload,
  IssueLabelsChangedPayload,
  IssueMetadataChangedPayload,
  InboxNewPayload,
  InboxItem,
  NotificationPreferenceResponse,
  CommentCreatedPayload,
  CommentUpdatedPayload,
  CommentDeletedPayload,
  CommentResolvedPayload,
  CommentUnresolvedPayload,
  ActivityCreatedPayload,
  ReactionAddedPayload,
  ReactionRemovedPayload,
  IssueReactionAddedPayload,
  IssueReactionRemovedPayload,
  SubscriberAddedPayload,
  SubscriberRemovedPayload,
  TaskMessagePayload,
  TaskQueuedPayload,
  TaskDispatchPayload,
  TaskRunningPayload,
  TaskWaitingLocalDirectoryPayload,
  TaskCompletedPayload,
  TaskFailedPayload,
  TaskCancelledPayload,
  ChatDonePayload,
  ChatMessage,
  ChatPendingTask,
  ChatMessagesPage,
  InvitationCreatedPayload,
} from "../types";

const chatWsLogger = createLogger("chat.ws");

const logger = createLogger("realtime-sync");

export function invalidateChatMessageQueries(
  qc: QueryClient,
  sessionId: string,
) {
  qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
  qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
}

export function applyChatDoneToCache(
  qc: QueryClient,
  payload: ChatDonePayload,
) {
  const sessionId = payload.chat_session_id;
  const taskId = payload.task_id;
  const messageId = payload.message_id;
  const content = payload.content;
  if (messageId && content !== undefined) {
    const assistant: ChatMessage = {
      id: messageId,
      chat_session_id: sessionId,
      role: "assistant",
      content,
      task_id: taskId,
      created_at: payload.created_at ?? new Date().toISOString(),
      elapsed_ms: payload.elapsed_ms ?? null,
    };
    qc.setQueryData<ChatMessage[] | undefined>(
      chatKeys.messages(sessionId),
      (old) => {
        if (!old) return old; // first fetch will pick it up
        // Idempotent against reconnect replay.
        if (old.some((m) => m.id === messageId)) return old;
        return [...old, assistant];
      },
    );
    qc.setQueryData<InfiniteData<ChatMessagesPage> | undefined>(
      chatKeys.messagesPage(sessionId),
      (old) => patchLatestChatMessagePage(old, assistant),
    );
  }
  // Replacement is in the messages list now; safe to drop pending.
  qc.setQueryData(chatKeys.pendingTask(sessionId), {});
  // Authoritative refetch reconciles redaction / migrations / clients
  // that took the fallback branch above.
  invalidateChatMessageQueries(qc, sessionId);
  qc.invalidateQueries({ queryKey: chatKeys.pendingTask(sessionId) });
}

function patchLatestChatMessagePage(
  old: InfiniteData<ChatMessagesPage> | undefined,
  message: ChatMessage,
): InfiniteData<ChatMessagesPage> | undefined {
  if (!old?.pages.length) return old;
  const seen = old.pages.some((page) => page.messages.some((m) => m.id === message.id));
  if (seen) return old;
  return {
    ...old,
    pages: old.pages.map((page, index) => {
      if (index !== 0) return page;
      return {
        ...page,
        messages: [...page.messages, message],
      };
    }),
  };
}

/**
 * Apply a workspace:updated event to the cache. Always refreshes the
 * workspace list. If the incoming `issue_prefix` differs from what's
 * currently cached, also invalidates issueKeys.all for that workspace,
 * since every issue's rendered identifier (`MUL-123`) is recomputed from
 * the workspace prefix at read time. Without this, the UI keeps showing
 * the old `OLD-N` keys until the next hard refresh.
 *
 * If the workspace isn't in the cached list (first observation), we
 * conservatively invalidate — the prefix is effectively "new" relative to
 * what's cached, so any issues already loaded under the old prefix would
 * be stale anyway.
 */
export function applyWorkspaceUpdatedToCache(
  qc: QueryClient,
  payload: WorkspaceUpdatedPayload,
): void {
  const next = payload.workspace;
  if (next?.id) {
    const cached =
      qc
        .getQueryData<Workspace[]>(workspaceKeys.list())
        ?.find((w) => w.id === next.id) ?? null;
    if (!cached || cached.issue_prefix !== next.issue_prefix) {
      qc.invalidateQueries({ queryKey: issueKeys.all(next.id) });
    }
  }
  qc.invalidateQueries({ queryKey: workspaceKeys.list() });
}

/**
 * Resolves the slug of the workspace an inbox item originated from, via the
 * cached workspace list (fetched once when the cache is cold).
 *
 * Desktop notification routing must pin to the *source* workspace of the
 * inbox item, not the currently active one: the user can be on workspace B
 * when an `inbox:new` for workspace A arrives, and macOS Notification Center
 * holds banners across workspace switches. Returns null when the workspace
 * cannot be resolved — callers must NOT fall back to the current slug (that
 * recreates the wrong-workspace routing this exists to prevent, #3766) and
 * should show the notification without a deep link instead.
 */
export async function resolveInboxSourceSlug(
  qc: QueryClient,
  workspaceId: string,
): Promise<string | null> {
  if (!workspaceId) return null;
  try {
    const workspaces = await qc.ensureQueryData(workspaceListOptions());
    return workspaces?.find((w) => w.id === workspaceId)?.slug ?? null;
  } catch {
    // Workspace list unavailable (e.g. network hiccup): degrade to a
    // link-less notification rather than guessing a slug.
    return null;
  }
}

/**
 * Handles an `inbox:new` event end-to-end: inbox cache invalidation, the
 * focus / mute checks, and the native OS banner. Exported so the handler
 * behavior (not just slug resolution) is testable.
 *
 * Every workspace-scoped read here keys on the ITEM's workspace
 * (`item.workspace_id`), never the currently active one (#3766): the cache
 * invalidation must refresh the source workspace's inbox list / unread
 * count / dock badge, the mute check must honor the source workspace's
 * preference, and the deep link must carry the source workspace's slug.
 */
export async function handleInboxNew(
  qc: QueryClient,
  item: InboxItem,
): Promise<void> {
  const sourceWsId = item.workspace_id;
  if (sourceWsId) onInboxNew(qc, sourceWsId, item);
  // A new item in ANY workspace can light the workspace-switcher dot, so
  // refresh the cross-workspace summary regardless of the active workspace.
  onInboxSummaryInvalidate(qc);
  // Fire a native OS notification only when the app isn't focused. When
  // the user is already looking at Multica, the inbox sidebar's unread
  // styling is enough — no need to interrupt with a banner. `desktopAPI`
  // is injected by the preload script; its absence (web app) skips silently.
  if (typeof document !== "undefined" && document.hasFocus()) return;
  // Resolve the source workspace's slug once: it pins BOTH the mute check
  // and the deep link to the workspace the inbox item BELONGS to, never the
  // currently active one. Reading `getCurrentSlug()` here was the source of
  // wrong-workspace routing (#3766): an `inbox:new` from workspace A arriving
  // while workspace B is active emitted a notification carrying B's slug and
  // A's issue id, deep-linking to an issue B doesn't have.
  const slug = await resolveInboxSourceSlug(qc, sourceWsId);
  // Respect the SOURCE workspace's system-notification preference. Keying the
  // query on `sourceWsId` is not enough: the request resolves its workspace
  // from the `X-Workspace-Slug` header, which follows the ACTIVE workspace —
  // so a cold-cache lookup while viewing B would read B's mute setting and
  // cache it under A's key. Passing the source slug scopes the fetch to A.
  // When the slug can't be resolved we read only an already-warm cache
  // (populated earlier with the correct workspace context) rather than fetch
  // with the wrong one; on network failure we fall through to the default
  // ("all") rather than swallow the banner.
  if (sourceWsId) {
    try {
      const prefData = slug
        ? await qc.ensureQueryData(
            notificationPreferenceOptions(sourceWsId, slug),
          )
        : qc.getQueryData<NotificationPreferenceResponse>(
            notificationPreferenceKeys.all(sourceWsId),
          );
      if (prefData?.preferences?.system_notifications === "muted") return;
    } catch {
      // Fall through with default behavior.
    }
  }
  // `issueKey` matches the inbox page's URL selector (issue id when the
  // item is attached to an issue, otherwise the inbox item id). `itemId`
  // is the inbox row's own id, needed to fire markInboxRead on click.
  // A null slug (workspace list unavailable / item from a workspace this
  // client can't see) still shows the banner — the user should learn about
  // the inbox item — but with an empty slug so the click is a no-op
  // (the inbox bridge ignores empty slugs) instead of routing wrong.
  const payload: SystemNotificationPayload = {
    slug: slug ?? "",
    itemId: item.id,
    issueKey: item.issue_id ?? item.id,
    title: item.title,
    body: item.body ?? "",
  };
  const desktopAPI = (
    globalThis as unknown as {
      desktopAPI?: {
        showNotification?: (payload: SystemNotificationPayload) => void;
      };
    }
  ).desktopAPI;
  if (desktopAPI?.showNotification) {
    // Desktop: native OS banner rendered by the Electron main process.
    desktopAPI.showNotification(payload);
    return;
  }
  // Web: the browser Notification API. No-op without granted permission or on
  // SSR — the in-app inbox + unread badge still reflect the new item.
  showWebNotification(payload);
}

/**
 * Invalidates all workspace-scoped queries. Used after reconnect and when a
 * new WSClient instance is detected (workspace switch) to recover events
 * missed while disconnected.
 */
function invalidateWorkspaceScopedQueries(qc: QueryClient): void {
  const wsId = getCurrentWsId();
  if (wsId) {
    qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
    qc.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });
    qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
    qc.invalidateQueries({ queryKey: workspaceKeys.invitations(wsId) });
    qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: autopilotKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: agentTaskSnapshotKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: agentActivityKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: agentRunCountsKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: chatKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
  }
  // Cross-workspace, so outside the wsId guard: a reconnect may have missed
  // inbox events from any workspace, so re-pull the switcher-dot summary.
  onInboxSummaryInvalidate(qc);
  // Per-issue caches are keyed without wsId, so the issueKeys.all(wsId)
  // prefix above does not reach them. They rely entirely on WS events for
  // freshness (staleTime: Infinity), so events missed while disconnected
  // left them stale until a full reload — the inbox showed an agent's new
  // comment while the issue timeline didn't (#3953). Inactive caches only
  // get marked stale here and refetch on next mount; the one mounted issue
  // refetches immediately, same as its own useWSReconnect already does.
  qc.invalidateQueries({ queryKey: issueKeys.timelineAll() });
  qc.invalidateQueries({ queryKey: issueKeys.reactionsAll() });
  qc.invalidateQueries({ queryKey: issueKeys.subscribersAll() });
  qc.invalidateQueries({ queryKey: issueKeys.usageAll() });
  qc.invalidateQueries({ queryKey: issueKeys.attachmentsAll() });
  qc.invalidateQueries({ queryKey: issueKeys.tasksAll() });
  // Per-chat-session caches are also keyed without wsId, so the
  // chatKeys.all(wsId) prefix above only reaches session lists / aggregates.
  // Message streams rely on WS invalidation with staleTime: Infinity; recover
  // sessions that missed chat/task events while the socket was disconnected.
  qc.invalidateQueries({ queryKey: chatKeys.messagesAll() });
  qc.invalidateQueries({ queryKey: chatKeys.messagesPageAll() });
  qc.invalidateQueries({ queryKey: chatKeys.pendingTaskAll() });
  qc.invalidateQueries({ queryKey: chatKeys.taskMessagesAll() });
  qc.invalidateQueries({ queryKey: workspaceKeys.list() });
}

function invalidateSquadMemberStatusQueries(qc: QueryClient, wsId: string): void {
  qc.invalidateQueries({
    predicate: (query) => {
      const key = query.queryKey;
      return (
        key[0] === "workspaces" &&
        key[1] === wsId &&
        key[2] === "squads" &&
        key[4] === "members-status"
      );
    },
  });
}

export interface RealtimeSyncStores {
  authStore: UseBoundStore<StoreApi<AuthState>>;
}

/**
 * Centralized WS -> store sync. Called once from WSProvider.
 *
 * Uses the "WS as invalidation signal + refetch" pattern:
 * - onAny handler extracts event prefix and calls the matching store refresh
 * - Debounce per-prefix prevents rapid-fire refetches (e.g. bulk issue updates)
 * - Precise handlers only for side effects (toast, navigation, self-check)
 *
 * Per-issue events (comments, activity, reactions, subscribers) are handled
 * both here (invalidation fallback) and by per-page useWSEvent hooks (granular
 * updates). Daemon register events invalidate runtimes globally; heartbeats
 * are skipped to avoid excessive refetches.
 *
 * @param ws - WebSocket client instance (null when not yet connected)
 * @param stores - Platform-created Zustand store instances for auth and workspace
 * @param onToast - Optional callback for showing toast messages (platform-specific)
 */
export function useRealtimeSync(
  ws: WSClient | null,
  stores: RealtimeSyncStores,
  onToast?: (message: string, type?: "info" | "error") => void,
) {
  const { authStore } = stores;
  const qc = useQueryClient();

  // Captured via ref so the (rare) hasOnboarded change doesn't re-subscribe
  // every WS handler in this effect. The resolver reads `.current` at the
  // moment workspace-loss fires, which is what we want.
  const hasOnboarded = useHasOnboarded();
  const hasOnboardedRef = useRef(hasOnboarded);
  hasOnboardedRef.current = hasOnboarded;

  // Main sync: onAny -> refreshMap with debounce
  useEffect(() => {
    if (!ws) return;

    const refreshMap: Record<string, () => void> = {
      inbox: () => {
        const wsId = getCurrentWsId();
        if (wsId) onInboxInvalidate(qc, wsId);
        // inbox:read / inbox:archived / batch events arrive here. They can
        // originate from a workspace other than the active one (personal
        // events fan out to all the user's connections), so always refresh
        // the cross-workspace summary — its dot must clear when another
        // workspace's items are read/archived.
        onInboxSummaryInvalidate(qc);
      },
      agent: () => {
        const wsId = getCurrentWsId();
        if (wsId) {
          qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
          // Squad members status is derived per agent, so any agent
          // change (status flip, archive, runtime swap) needs to refresh the
          // per-squad members-status cache without refetching the static squad
          // list summary.
          invalidateSquadMemberStatusQueries(qc, wsId);
        }
      },
      member: () => {
        const wsId = getCurrentWsId();
        if (wsId) qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      },
      // workspace:updated is handled by the specific handler below
      // (compares prefixes to decide whether to also invalidate issues).
      // This generic fallback still fires for workspace:deleted (paired
      // with the specific navigation handler) and any future workspace:*
      // events without dedicated handlers.
      workspace: () => {
        qc.invalidateQueries({ queryKey: workspaceKeys.list() });
      },
      skill: () => {
        const wsId = getCurrentWsId();
        if (wsId) qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
      },
      project: () => {
        const wsId = getCurrentWsId();
        if (wsId) qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      },
      squad: () => {
        const wsId = getCurrentWsId();
        if (wsId) {
          qc.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });
          // squad:deleted triggers assignee transfer — refresh issues too.
          qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
        }
      },
      label: () => {
        // label:created/updated/deleted — also refresh issues, since each
        // issue carries a denormalized snapshot of its labels (rename/recolor
        // /delete on a label needs to flush the chips on every issue showing
        // it).
        const wsId = getCurrentWsId();
        if (wsId) {
          qc.invalidateQueries({ queryKey: ["labels", wsId] });
          qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
        }
      },
      pin: () => {
        const wsId = getCurrentWsId();
        const userId = authStore.getState().user?.id;
        if (wsId && userId) qc.invalidateQueries({ queryKey: pinKeys.all(wsId, userId) });
      },
      daemon: () => {
        const wsId = getCurrentWsId();
        if (wsId) {
          qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
          // Runtime online/offline transitions move the derived status
          // for every agent that hosts on this runtime, which shifts the
          // working/idle/offline pill on the squad page.
          invalidateSquadMemberStatusQueries(qc, wsId);
        }
      },
      autopilot: () => {
        const wsId = getCurrentWsId();
        if (wsId) qc.invalidateQueries({ queryKey: autopilotKeys.all(wsId) });
      },
      github_installation: () => {
        const wsId = getCurrentWsId();
        if (wsId) qc.invalidateQueries({ queryKey: githubKeys.installations(wsId) });
      },
      lark_installation: () => {
        const wsId = getCurrentWsId();
        if (wsId) qc.invalidateQueries({ queryKey: larkKeys.installations(wsId) });
      },
      slack_installation: () => {
        const wsId = getCurrentWsId();
        if (wsId) qc.invalidateQueries({ queryKey: slackKeys.installations(wsId) });
      },
      pull_request: () => {
        // PR list is keyed by issue id, not workspace, so we invalidate all
        // PR queries — the open issue detail page will refetch its own list.
        qc.invalidateQueries({ queryKey: ["github", "pull-requests"] });
      },
      // Powers the agent presence cache: any task lifecycle change
      // (dispatch / completed / failed / cancelled) refreshes the
      // workspace-wide agent-task-snapshot query so per-agent presence
      // reflects the change. task:message is NOT in this prefix path — it
      // stays in specificEvents to avoid an invalidate storm during long runs.
      task: () => {
        const wsId = getCurrentWsId();
        if (!wsId) return;
        qc.invalidateQueries({ queryKey: agentTaskSnapshotKeys.list(wsId) });
        // 30d activity series shares the same lifecycle signal — any task
        // completion / failure shifts the histogram. (Dispatch alone
        // doesn't change a completed_at-anchored series, but invalidating
        // here keeps the WS-handler shape uniform; the resulting refetch
        // is cheap.) Both the list (trailing 7d slice) and the detail
        // panel read off this single cache.
        qc.invalidateQueries({ queryKey: agentActivityKeys.last30d(wsId) });
        // 30-day run count likewise increments per task lifecycle event.
        qc.invalidateQueries({ queryKey: agentRunCountsKeys.last30d(wsId) });
        // Per-agent task list (Activity tab "Recent work"). Prefix match
        // catches every agent's list — the per-agent detail key sits
        // under agentTasks/<wsId>/<agentId>.
        qc.invalidateQueries({ queryKey: agentTasksKeys.all(wsId) });
        // Per-issue task list (issue-detail Execution log). Prefix match
        // across all issues — keeps the contract "any task: event makes
        // every list-of-tasks query stale" so cache stays fresh even
        // when the relevant component isn't currently mounted.
        qc.invalidateQueries({ queryKey: ["issues", "tasks"] });
        // Per-issue token usage card (issue-detail right rail). Same
        // shape as the tasks invalidation above — any task lifecycle
        // event shifts the aggregated usage numbers.
        qc.invalidateQueries({ queryKey: ["issues", "usage"] });
        // Squad members-status reads the same task lifecycle to flip
        // working ↔ idle for each agent member.
        invalidateSquadMemberStatusQueries(qc, wsId);
        // Comment trigger previews answer "who would a send wake right
        // now" — the pending-task dedup guard makes that answer
        // queue-dependent, so any task lifecycle change must refresh an
        // open composer's chips (e.g. an agent finishing its run becomes
        // triggerable again mid-typing).
        qc.invalidateQueries({ queryKey: issueKeys.commentTriggerPreviewAll() });
        // Issue-trigger previews (assign/status/create/batch) are deliberately
        // NOT invalidated here. Unlike comment triggers, the assign source
        // (create / assignee change) cancels existing tasks before enqueuing, so
        // a task event can never change its verdict; only the status source's
        // pending dedup could, and that preview is advisory — the write path
        // re-evaluates authoritatively, so a rare stale label is harmless.
        // Refetching every mounted preview on every workspace task event caused
        // visible flicker, so the preview now refetches only on input change
        // (signature), mirroring its query design (MUL-3375).
      },
    };

    const timers = new Map<string, ReturnType<typeof setTimeout>>();
    const debouncedRefresh = (prefix: string, fn: () => void) => {
      const existing = timers.get(prefix);
      if (existing) clearTimeout(existing);
      timers.set(
        prefix,
        setTimeout(() => {
          timers.delete(prefix);
          fn();
        }, 100),
      );
    };

    // Event types handled by specific handlers below -- skip generic refresh
    const specificEvents = new Set([
      "workspace:updated",
      "issue:updated", "issue:created", "issue:deleted", "issue_labels:changed", "issue_metadata:changed", "inbox:new",
      "comment:created", "comment:updated", "comment:deleted",
      "comment:resolved", "comment:unresolved",
      "activity:created",
      "reaction:added", "reaction:removed",
      "issue_reaction:added", "issue_reaction:removed",
      "subscriber:added", "subscriber:removed",
      "daemon:heartbeat",
      // Chat events are handled explicitly below; do not double-invalidate.
      "chat:message", "chat:done", "chat:session_read", "chat:session_deleted",
      "chat:session_updated",
      // task:message stays out of the prefix path because it fires per
      // streamed message during a long run — invalidating the snapshot on
      // every message would flood the network. Specific chat handlers below
      // still receive it via ws.on() (a separate subscription channel).
      "task:message",
      // task:completed / task:failed deliberately NOT here. They go through
      // both the task-prefix invalidate (refreshes the agent-task-snapshot
      // cache) AND the chat-specific ws.on() handlers below. The two
      // channels are independent — onAny dispatch and ws.on are separate
      // subscriptions.
    ]);

    const unsubAny = ws.onAny((msg) => {
      if (specificEvents.has(msg.type)) return;
      const prefix = msg.type.split(":")[0] ?? "";
      const refresh = refreshMap[prefix];
      if (refresh) debouncedRefresh(prefix, refresh);
    });

    // --- Specific event handlers (granular cache updates) ---
    // No self-event filtering: actor_id identifies the USER, not the TAB.
    // Filtering by actor_id would block other tabs of the same user.
    // Instead, both mutations and WS handlers use dedup checks to be idempotent.

    const unsubIssueUpdated = ws.on("issue:updated", (p) => {
      const payload = p as IssueUpdatedPayload;
      const { issue } = payload;
      if (!issue?.id) return;
      const wsId = getCurrentWsId();
      if (wsId) {
        onIssueUpdated(qc, wsId, issue, {
          assigneeChanged: payload.assignee_changed,
          statusChanged: payload.status_changed,
          projectChanged: payload.project_changed,
        });
        if (issue.status) {
          onInboxIssueStatusChanged(qc, wsId, issue.id, issue.status);
        }
      }
    });

    const unsubIssueCreated = ws.on("issue:created", (p) => {
      const { issue } = p as IssueCreatedPayload;
      if (!issue) return;
      const wsId = getCurrentWsId();
      if (wsId) onIssueCreated(qc, wsId, issue);
    });

    const unsubIssueDeleted = ws.on("issue:deleted", (p) => {
      const { issue_id } = p as IssueDeletedPayload;
      if (!issue_id) return;
      const wsId = getCurrentWsId();
      if (wsId) {
        onIssueDeleted(qc, wsId, issue_id);
        onInboxIssueDeleted(qc, wsId, issue_id);
      }
    });

    const unsubIssueLabelsChanged = ws.on("issue_labels:changed", (p) => {
      const { issue_id, labels } = p as IssueLabelsChangedPayload;
      if (!issue_id) return;
      const wsId = getCurrentWsId();
      if (wsId) onIssueLabelsChanged(qc, wsId, issue_id, labels ?? []);
    });

    const unsubIssueMetadataChanged = ws.on("issue_metadata:changed", (p) => {
      const { issue_id, metadata } = p as IssueMetadataChangedPayload;
      if (!issue_id) return;
      const wsId = getCurrentWsId();
      if (wsId) onIssueMetadataChanged(qc, wsId, issue_id, metadata ?? {});
    });

    const unsubInboxNew = ws.on("inbox:new", async (p) => {
      const { item } = p as InboxNewPayload;
      if (!item) return;
      await handleInboxNew(qc, item);
    });

    // --- Timeline event handlers (global fallback) ---
    // These events are also handled granularly by useIssueTimeline when
    // IssueDetail is mounted. This global handler exists to mark the
    // timeline cache stale for issues whose IssueDetail is *not* mounted,
    // so stale data isn't served on next mount (staleTime: Infinity, set on
    // the QueryClient default, relies on this).
    //
    // `refetchType: "none"` is the load-bearing detail: without it, an
    // active IssueDetail observer would refetch the entire timeline on
    // every comment / activity / reaction event. The refetch replaces
    // every entry's reference and busts React.memo on every CommentCard
    // subtree (visible during AI streaming as a flash across all sibling
    // threads, MUL-1941). Inactive observers don't refetch either way;
    // when IssueDetail mounts later, the stale flag triggers the refetch
    // through `refetchOnMount`. Active observers stay fresh via the
    // granular setQueryData handlers in `useIssueTimeline`.
    const invalidateTimeline = (issueId: string) => {
      qc.invalidateQueries({
        queryKey: issueKeys.timeline(issueId),
        refetchType: "none",
      });
    };

    const unsubCommentCreated = ws.on("comment:created", (p) => {
      const { comment } = p as CommentCreatedPayload;
      if (comment?.issue_id) invalidateTimeline(comment.issue_id);
    });

    const unsubCommentUpdated = ws.on("comment:updated", (p) => {
      const { comment } = p as CommentUpdatedPayload;
      if (comment?.issue_id) invalidateTimeline(comment.issue_id);
    });

    const unsubCommentDeleted = ws.on("comment:deleted", (p) => {
      const { issue_id } = p as CommentDeletedPayload;
      if (issue_id) invalidateTimeline(issue_id);
    });

    const unsubCommentResolved = ws.on("comment:resolved", (p) => {
      const { comment } = p as CommentResolvedPayload;
      if (comment?.issue_id) invalidateTimeline(comment.issue_id);
    });

    const unsubCommentUnresolved = ws.on("comment:unresolved", (p) => {
      const { comment } = p as CommentUnresolvedPayload;
      if (comment?.issue_id) invalidateTimeline(comment.issue_id);
    });

    const unsubActivityCreated = ws.on("activity:created", (p) => {
      const { issue_id } = p as ActivityCreatedPayload;
      if (issue_id) invalidateTimeline(issue_id);
    });

    const unsubReactionAdded = ws.on("reaction:added", (p) => {
      const { issue_id } = p as ReactionAddedPayload;
      if (issue_id) invalidateTimeline(issue_id);
    });

    const unsubReactionRemoved = ws.on("reaction:removed", (p) => {
      const { issue_id } = p as ReactionRemovedPayload;
      if (issue_id) invalidateTimeline(issue_id);
    });

    // --- Issue-level reactions & subscribers (global fallback) ---

    const unsubIssueReactionAdded = ws.on("issue_reaction:added", (p) => {
      const { issue_id } = p as IssueReactionAddedPayload;
      if (issue_id) qc.invalidateQueries({ queryKey: issueKeys.reactions(issue_id) });
    });

    const unsubIssueReactionRemoved = ws.on("issue_reaction:removed", (p) => {
      const { issue_id } = p as IssueReactionRemovedPayload;
      if (issue_id) qc.invalidateQueries({ queryKey: issueKeys.reactions(issue_id) });
    });

    const unsubSubscriberAdded = ws.on("subscriber:added", (p) => {
      const { issue_id } = p as SubscriberAddedPayload;
      if (issue_id) qc.invalidateQueries({ queryKey: issueKeys.subscribers(issue_id) });
    });

    const unsubSubscriberRemoved = ws.on("subscriber:removed", (p) => {
      const { issue_id } = p as SubscriberRemovedPayload;
      if (issue_id) qc.invalidateQueries({ queryKey: issueKeys.subscribers(issue_id) });
    });

    // --- Side-effect handlers (toast, navigation) ---

    // After the current workspace disappears (deleted or we were kicked out),
    // navigate to another workspace the user still has access to, or to the
    // create-workspace page. We use a full-page navigation: this reliably
    // tears down any in-flight queries / subscriptions tied to the dead
    // workspace without relying on framework-specific routers from here in
    // core.
    const relocateAfterWorkspaceLoss = async (lostWsId: string) => {
      const wsList = await qc.fetchQuery({
        ...workspaceListOptions(),
        staleTime: 0,
      });
      const remaining = wsList.filter((w) => w.id !== lostWsId);
      const target = resolvePostAuthDestination(
        remaining,
        hasOnboardedRef.current,
      );
      if (typeof window !== "undefined") {
        window.location.assign(target);
      }
    };

    const unsubWsUpdated = ws.on("workspace:updated", (p) => {
      applyWorkspaceUpdatedToCache(qc, p as WorkspaceUpdatedPayload);
    });

    const unsubWsDeleted = ws.on("workspace:deleted", (p) => {
      const { workspace_id } = p as WorkspaceDeletedPayload;
      // Event payload has UUID; look up slug from cached workspace list
      // since clearWorkspaceStorage keys are namespaced by slug.
      const wsList = qc.getQueryData<{ id: string; slug: string }[]>(workspaceKeys.list()) ?? [];
      const deletedSlug = wsList.find((w) => w.id === workspace_id)?.slug;
      if (deletedSlug) clearWorkspaceStorage(defaultStorage, deletedSlug);
      if (getCurrentWsId() === workspace_id) {
        logger.warn("current workspace deleted, switching");
        onToast?.("This workspace was deleted", "info");
        relocateAfterWorkspaceLoss(workspace_id);
      }
    });

    const unsubMemberRemoved = ws.on("member:removed", (p) => {
      const { user_id } = p as MemberRemovedPayload;
      const myUserId = authStore.getState().user?.id;
      if (user_id === myUserId) {
        const slug = getCurrentSlug();
        const wsId = getCurrentWsId();
        if (slug && wsId) {
          clearWorkspaceStorage(defaultStorage, slug);
          logger.warn("removed from workspace, switching");
          onToast?.("You were removed from this workspace", "info");
          relocateAfterWorkspaceLoss(wsId);
        }
      }
    });

    const unsubMemberAdded = ws.on("member:added", (p) => {
      const { member, workspace_name } = p as MemberAddedPayload;
      const myUserId = authStore.getState().user?.id;
      if (member.user_id === myUserId) {
        qc.invalidateQueries({ queryKey: workspaceKeys.list() });
        qc.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
        onToast?.(
          `You joined ${workspace_name ?? "a workspace"}`,
          "info",
        );
      }
    });

    // invitation:created — notify the invitee of a new pending invitation
    const unsubInvitationCreated = ws.on("invitation:created", (p) => {
      const { workspace_name } = p as InvitationCreatedPayload;
      qc.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
      onToast?.(
        `You were invited to ${workspace_name ?? "a workspace"}`,
        "info",
      );
    });

    // invitation:accepted / declined / revoked — refresh invitation lists
    const unsubInvitationAccepted = ws.on("invitation:accepted", () => {
      const currentWsId = getCurrentWsId();
      if (currentWsId) {
        qc.invalidateQueries({ queryKey: workspaceKeys.invitations(currentWsId) });
        qc.invalidateQueries({ queryKey: workspaceKeys.members(currentWsId) });
      }
    });
    const unsubInvitationDeclined = ws.on("invitation:declined", () => {
      const currentWsId = getCurrentWsId();
      if (currentWsId) {
        qc.invalidateQueries({ queryKey: workspaceKeys.invitations(currentWsId) });
      }
    });
    const unsubInvitationRevoked = ws.on("invitation:revoked", () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
    });

    // --- Chat / task events (global, survives ChatWindow unmount) ---
    //
    // Single source of truth: the Query cache. No Zustand writes here — the
    // earlier mirror caused a race where the cache and store disagreed
    // during the invalidate → refetch window and the UI rendered duplicates.
    //
    // task:message is written directly into the task-messages cache so the
    // live timeline updates in place. chat:message / chat:done /
    // task:completed / task:failed invalidate messages + pending-task so the
    // DB remains authoritative.

    const unsubTaskMessage = ws.on("task:message", (p) => {
      const payload = p as TaskMessagePayload;
      qc.setQueryData<TaskMessagePayload[]>(
        chatKeys.taskMessages(payload.task_id),
        (old = []) => mergeTaskMessagesBySeq(old, [payload]),
      );
      chatWsLogger.debug("task:message (global)", {
        task_id: payload.task_id,
        seq: payload.seq,
        type: payload.type,
      });
    });

    // Helpers reused by chat lifecycle handlers.
    const invalidatePendingAggregate = () => {
      const id = getCurrentWsId();
      if (id) qc.invalidateQueries({ queryKey: chatKeys.pendingTasks(id) });
    };
    const invalidateSessionLists = () => {
      const id = getCurrentWsId();
      if (id) qc.invalidateQueries({ queryKey: chatKeys.sessions(id) });
    };

    const unsubChatMessage = ws.on("chat:message", (p) => {
      const payload = p as { chat_session_id: string };
      chatWsLogger.info("chat:message (global)", { chat_session_id: payload.chat_session_id });
      invalidateChatMessageQueries(qc, payload.chat_session_id);
      qc.invalidateQueries({ queryKey: chatKeys.pendingTask(payload.chat_session_id) });
      invalidatePendingAggregate();
    });

    const unsubChatDone = ws.on("chat:done", (p) => {
      const payload = p as ChatDonePayload;
      chatWsLogger.info("chat:done (global)", {
        task_id: payload.task_id,
        chat_session_id: payload.chat_session_id,
        has_message: !!payload.message_id,
      });
      // Inline-insert the assistant message into the messages cache BEFORE
      // clearing pending-task. Both writes land in the same React render
      // tick, so ChatMessageList sees `pendingAlreadyPersisted === true`
      // and the live TimelineView unmounts only after AssistantMessage has
      // mounted — no flicker window. This applies TkDodo's "combine
      // setQueryData (active query) + invalidateQueries (others)" pattern
      // (https://tkdodo.eu/blog/using-web-sockets-with-react-query).
      //
      // Falls back to invalidate-only when the server omits the message
      // payload (older builds). Older clients hitting a newer server also
      // work: they ignore the extra fields and rely on the invalidate
      // below, which keeps the old behavior alive.
      applyChatDoneToCache(qc, payload);
      invalidatePendingAggregate();
      // Assistant message just landed → has_unread may have flipped to true.
      invalidateSessionLists();
    });

    // Chat task lifecycle writethrough: keep `chatKeys.pendingTask(sessionId)`
    // synchronized with the server state machine via setQueryData rather than
    // invalidate-refetch. Same pattern as task:message — the WS payload
    // carries everything we need, and an HTTP roundtrip just to read what we
    // already know would add latency to every stage transition.
    //
    // task:queued is emitted by EnqueueChatTask. The optimistic seed in
    // chat-window.tsx may have already populated the cache with a temporary
    // id; this handler upgrades it to the real task_id (and reaffirms status
    // when reconnect replays the event for an already-running task).
    const unsubTaskQueued = ws.on("task:queued", (p) => {
      const payload = p as TaskQueuedPayload;
      if (!payload.chat_session_id) return;
      qc.setQueryData<ChatPendingTask>(
        chatKeys.pendingTask(payload.chat_session_id),
        (old) => ({
          ...(old ?? {}),
          task_id: payload.task_id,
          status: "queued",
        }),
      );
      invalidatePendingAggregate();
    });

    // task:dispatch fires when the daemon claims the queued task. The daemon
    // immediately follows with StartTask, so dispatched→running is sub-second.
    // We collapse that window by writing "running" directly — the pill jumps
    // from "Queued" straight to "Thinking", skipping a meaningless "Starting"
    // frame. Stage decision in TaskStatusPill maps "running" + empty
    // taskMessages → "Thinking · Ns".
    const unsubTaskDispatch = ws.on("task:dispatch", (p) => {
      const payload = p as TaskDispatchPayload;
      if (!payload.chat_session_id) return;
      qc.setQueryData<ChatPendingTask>(
        chatKeys.pendingTask(payload.chat_session_id),
        (old) => {
          if (!old || old.task_id !== payload.task_id) return old;
          return { ...old, status: "running" };
        },
      );
    });

    // task:running fires when the daemon transitions a previously-parked task
    // (waiting_local_directory) back into the run phase. The dispatch→running
    // path is collapsed in the handler above, so this handler exists mainly to
    // clear a stale `waiting_local_directory` pill — without it, the pill
    // would stay parked even after the daemon resumed work.
    const unsubTaskRunning = ws.on("task:running", (p) => {
      const payload = p as TaskRunningPayload;
      if (!payload.chat_session_id) return;
      qc.setQueryData<ChatPendingTask>(
        chatKeys.pendingTask(payload.chat_session_id),
        (old) => {
          if (!old || old.task_id !== payload.task_id) return old;
          return { ...old, status: "running" };
        },
      );
    });

    // task:waiting_local_directory fires when the daemon dequeues a task but
    // can't acquire the local_directory path lock — another task on this
    // daemon is in the same directory. Write the status so TaskStatusPill
    // can render the "Waiting for local directory" stage instead of pinning
    // a stale "Starting / Thinking" frame.
    const unsubTaskWaitingLocalDir = ws.on(
      "task:waiting_local_directory",
      (p) => {
        const payload = p as TaskWaitingLocalDirectoryPayload;
        if (!payload.chat_session_id) return;
        qc.setQueryData<ChatPendingTask>(
          chatKeys.pendingTask(payload.chat_session_id),
          (old) => {
            if (!old || old.task_id !== payload.task_id) return old;
            return { ...old, status: "waiting_local_directory" };
          },
        );
      },
    );

    // task:cancelled reaches us when:
    //   1. handleStop already cleared the cache locally (this is a no-op confirm)
    //   2. another tab / admin / system cancels — this is the only path that
    //      drops the pending pill in those cases. Without it the pill spins
    //      forever in the second-tab scenario.
    // CancelTask also persists a best-effort assistant snapshot when the
    // stopped chat task had already streamed transcript rows, so refresh the
    // message page along with clearing pending.
    const unsubTaskCancelled = ws.on("task:cancelled", (p) => {
      const payload = p as TaskCancelledPayload;
      if (!payload.chat_session_id) return;
      chatWsLogger.info("task:cancelled (global, chat)", {
        task_id: payload.task_id,
        chat_session_id: payload.chat_session_id,
      });
      qc.setQueryData(chatKeys.pendingTask(payload.chat_session_id), {});
      invalidateChatMessageQueries(qc, payload.chat_session_id);
      invalidatePendingAggregate();
    });

    const unsubTaskCompleted = ws.on("task:completed", (p) => {
      const payload = p as TaskCompletedPayload;
      if (!payload.chat_session_id) return; // issue tasks handled elsewhere
      chatWsLogger.info("task:completed (global, chat)", {
        task_id: payload.task_id,
        chat_session_id: payload.chat_session_id,
      });
      // `chat:done` (broadcast immediately before this event in CompleteTask)
      // already wrote the assistant message into the messages cache and
      // cleared `chatKeys.pendingTask`. This event is now only responsible
      // for refreshing the per-user cross-session aggregate that drives the
      // FAB indicator — `chat:done` is per-session and doesn't carry that
      // information.
      invalidatePendingAggregate();
    });

    const unsubTaskFailed = ws.on("task:failed", (p) => {
      const payload = p as TaskFailedPayload;
      if (!payload.chat_session_id) return;
      chatWsLogger.warn("task:failed (global, chat)", {
        task_id: payload.task_id,
        chat_session_id: payload.chat_session_id,
      });
      // FailTask writes a failure chat_message (mirroring CompleteTask's
      // success message), so this path mirrors the task:completed handler:
      // clear the pending signal AND invalidate the messages list so the
      // failure bubble shows up without requiring a page refresh. Pre-#1823
      // this branch only flipped pending — the comment "No new message"
      // was true then, but FailTask now persists a row.
      qc.setQueryData(chatKeys.pendingTask(payload.chat_session_id), {});
      invalidateChatMessageQueries(qc, payload.chat_session_id);
      qc.invalidateQueries({ queryKey: chatKeys.pendingTask(payload.chat_session_id) });
      invalidatePendingAggregate();
    });

    const unsubChatSessionRead = ws.on("chat:session_read", (p) => {
      const payload = p as { chat_session_id: string };
      chatWsLogger.info("chat:session_read (global)", payload);
      invalidateSessionLists();
    });

    // chat:session_updated fires after the creator renames a session in
    // any tab/device. Patch the cached row inline so the dropdown reflects
    // the new title without a full sessions-list refetch.
    const unsubChatSessionUpdated = ws.on("chat:session_updated", (p) => {
      const payload = p as {
        chat_session_id: string;
        title?: string;
        updated_at?: string;
      };
      chatWsLogger.info("chat:session_updated (global)", payload);
      const id = getCurrentWsId();
      if (!id) return;
      const patch = (
        old?: { id: string; title: string; updated_at: string }[],
      ) =>
        old?.map((s) =>
          s.id === payload.chat_session_id
            ? {
                ...s,
                title: payload.title ?? s.title,
                updated_at: payload.updated_at ?? s.updated_at,
              }
            : s,
        );
      qc.setQueryData(chatKeys.sessions(id), patch);
    });

    // chat:session_deleted fires after a hard delete. The originating tab has
    // already optimistically dropped the row via useDeleteChatSession; this
    // handler keeps OTHER tabs/devices in sync and also clears the active
    // session pointer so a deleted session doesn't keep the chat window
    // pointed at vanished messages.
    const unsubChatSessionDeleted = ws.on("chat:session_deleted", (p) => {
      const payload = p as { chat_session_id: string };
      chatWsLogger.info("chat:session_deleted (global)", payload);
      const id = getCurrentWsId();
      if (id) {
        const drop = (old?: { id: string }[]) =>
          old?.filter((s) => s.id !== payload.chat_session_id);
        qc.setQueryData(chatKeys.sessions(id), drop);
      }
      qc.removeQueries({ queryKey: chatKeys.messages(payload.chat_session_id) });
      qc.removeQueries({ queryKey: chatKeys.pendingTask(payload.chat_session_id) });
      invalidatePendingAggregate();

      const chatState = useChatStore.getState?.();
      if (chatState && chatState.activeSessionId === payload.chat_session_id) {
        chatState.setActiveSession(null);
      }
    });

    return () => {
      unsubAny();
      unsubIssueUpdated();
      unsubIssueCreated();
      unsubIssueDeleted();
      unsubIssueLabelsChanged();
      unsubIssueMetadataChanged();
      unsubInboxNew();
      unsubCommentCreated();
      unsubCommentUpdated();
      unsubCommentDeleted();
      unsubCommentResolved();
      unsubCommentUnresolved();
      unsubActivityCreated();
      unsubReactionAdded();
      unsubReactionRemoved();
      unsubIssueReactionAdded();
      unsubIssueReactionRemoved();
      unsubSubscriberAdded();
      unsubSubscriberRemoved();
      unsubWsUpdated();
      unsubWsDeleted();
      unsubMemberRemoved();
      unsubMemberAdded();
      unsubInvitationCreated();
      unsubInvitationAccepted();
      unsubInvitationDeclined();
      unsubInvitationRevoked();
      unsubTaskMessage();
      unsubChatMessage();
      unsubChatDone();
      unsubTaskQueued();
      unsubTaskDispatch();
      unsubTaskRunning();
      unsubTaskWaitingLocalDir();
      unsubTaskCancelled();
      unsubTaskCompleted();
      unsubTaskFailed();
      unsubChatSessionRead();
      unsubChatSessionDeleted();
      unsubChatSessionUpdated();
      timers.forEach(clearTimeout);
      timers.clear();
    };
  }, [ws, qc, authStore, onToast]);

  // Reconnect -> refetch all data to recover missed events
  useEffect(() => {
    if (!ws) return;

    const unsub = ws.onReconnect(async () => {
      logger.info("reconnected, refetching all data");
      try {
        invalidateWorkspaceScopedQueries(qc);
      } catch (e) {
        logger.error("reconnect refetch failed", e);
      }
    });

    return unsub;
  }, [ws, qc]);

  // New WSClient instance (workspace switch) -> invalidate workspace-scoped
  // queries to recover events missed while the previous instance was torn down.
  // Skips the initial assignment to avoid a redundant refetch on first mount.
  const wsInstanceRef = useRef<WSClient | null>(null);
  useEffect(() => {
    if (!ws) return;
    if (wsInstanceRef.current === null) {
      // First non-null instance — store and skip invalidation.
      wsInstanceRef.current = ws;
      return;
    }
    if (wsInstanceRef.current === ws) return;
    wsInstanceRef.current = ws;

    logger.info("new WSClient instance detected, invalidating workspace queries");
    invalidateWorkspaceScopedQueries(qc);
  }, [ws, qc]);
}
