"use client";

import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { WSClient } from "../api/ws-client";
import type { StoreApi, UseBoundStore } from "zustand";
import type { AuthState } from "../auth/store";
import type { WorkspaceStore } from "../workspace/store";
import { createLogger } from "../logger";
import { clearWorkspaceStorage } from "../platform/storage-cleanup";
import { defaultStorage } from "../platform/storage";
import { issueKeys } from "../issues/queries";
import { projectKeys } from "../projects/queries";
import { pinKeys } from "../pins/queries";
import { autopilotKeys } from "../autopilots/queries";
import { runtimeKeys } from "../runtimes/queries";
import {
  onIssueCreated,
  onIssueUpdated,
  onIssueDeleted,
} from "../issues/ws-updaters";
import { onInboxNew, onInboxInvalidate, onInboxIssueStatusChanged } from "../inbox/ws-updaters";
import { inboxKeys } from "../inbox/queries";
import { workspaceKeys, workspaceListOptions } from "../workspace/queries";
import { chatKeys } from "../chat/queries";
import type {
  MemberAddedPayload,
  WorkspaceDeletedPayload,
  MemberRemovedPayload,
  IssueUpdatedPayload,
  IssueCreatedPayload,
  IssueDeletedPayload,
  InboxNewPayload,
  CommentCreatedPayload,
  CommentUpdatedPayload,
  CommentDeletedPayload,
  ActivityCreatedPayload,
  ReactionAddedPayload,
  ReactionRemovedPayload,
  IssueReactionAddedPayload,
  IssueReactionRemovedPayload,
  SubscriberAddedPayload,
  SubscriberRemovedPayload,
  TaskMessagePayload,
  TaskCompletedPayload,
  TaskFailedPayload,
  ChatDonePayload,
  InvitationCreatedPayload,
} from "../types";

const chatWsLogger = createLogger("chat.ws");

const logger = createLogger("realtime-sync");

export interface RealtimeSyncStores {
  authStore: UseBoundStore<StoreApi<AuthState>>;
  workspaceStore: UseBoundStore<StoreApi<WorkspaceStore>>;
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
  const { authStore, workspaceStore } = stores;
  const qc = useQueryClient();
  // Main sync: onAny -> refreshMap with debounce
  useEffect(() => {
    if (!ws) return;

    const refreshMap: Record<string, () => void> = {
      inbox: () => {
        const wsId = workspaceStore.getState().workspace?.id;
        if (wsId) onInboxInvalidate(qc, wsId);
      },
      agent: () => {
        const wsId = workspaceStore.getState().workspace?.id;
        if (wsId) qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      },
      member: () => {
        const wsId = workspaceStore.getState().workspace?.id;
        if (wsId) qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      },
      workspace: () => {
        qc.invalidateQueries({ queryKey: workspaceKeys.list() });
      },
      skill: () => {
        const wsId = workspaceStore.getState().workspace?.id;
        if (wsId) qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
      },
      project: () => {
        const wsId = workspaceStore.getState().workspace?.id;
        if (wsId) qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      },
      pin: () => {
        const wsId = workspaceStore.getState().workspace?.id;
        const userId = authStore.getState().user?.id;
        if (wsId && userId) qc.invalidateQueries({ queryKey: pinKeys.all(wsId, userId) });
      },
      daemon: () => {
        const wsId = workspaceStore.getState().workspace?.id;
        if (wsId) qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      },
      autopilot: () => {
        const wsId = workspaceStore.getState().workspace?.id;
        if (wsId) qc.invalidateQueries({ queryKey: autopilotKeys.all(wsId) });
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
      "issue:updated", "issue:created", "issue:deleted", "inbox:new",
      "comment:created", "comment:updated", "comment:deleted",
      "activity:created",
      "reaction:added", "reaction:removed",
      "issue_reaction:added", "issue_reaction:removed",
      "subscriber:added", "subscriber:removed",
      "daemon:heartbeat",
      // Chat / task events are handled explicitly below; do not double-invalidate.
      "chat:message", "chat:done", "chat:session_read",
      "task:message", "task:completed", "task:failed",
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
      const { issue } = p as IssueUpdatedPayload;
      if (!issue?.id) return;
      const wsId = workspaceStore.getState().workspace?.id;
      if (wsId) {
        onIssueUpdated(qc, wsId, issue);
        if (issue.status) {
          onInboxIssueStatusChanged(qc, wsId, issue.id, issue.status);
        }
      }
    });

    const unsubIssueCreated = ws.on("issue:created", (p) => {
      const { issue } = p as IssueCreatedPayload;
      if (!issue) return;
      const wsId = workspaceStore.getState().workspace?.id;
      if (wsId) onIssueCreated(qc, wsId, issue);
    });

    const unsubIssueDeleted = ws.on("issue:deleted", (p) => {
      const { issue_id } = p as IssueDeletedPayload;
      if (!issue_id) return;
      const wsId = workspaceStore.getState().workspace?.id;
      if (wsId) onIssueDeleted(qc, wsId, issue_id);
    });

    const unsubInboxNew = ws.on("inbox:new", (p) => {
      const { item } = p as InboxNewPayload;
      if (!item) return;
      const wsId = workspaceStore.getState().workspace?.id;
      if (wsId) onInboxNew(qc, wsId, item);
    });

    // --- Timeline event handlers (global fallback) ---
    // These events are also handled granularly by useIssueTimeline when
    // IssueDetail is mounted. This global handler ensures the timeline cache
    // is invalidated even when IssueDetail is unmounted, so stale data
    // isn't served on next mount (staleTime: Infinity relies on this).

    const invalidateTimeline = (issueId: string) => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
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

    const unsubWsDeleted = ws.on("workspace:deleted", (p) => {
      const { workspace_id } = p as WorkspaceDeletedPayload;
      clearWorkspaceStorage(defaultStorage, workspace_id);
      const currentWs = workspaceStore.getState().workspace;
      if (currentWs?.id === workspace_id) {
        logger.warn("current workspace deleted, switching");
        onToast?.("This workspace was deleted", "info");
        qc.fetchQuery({ ...workspaceListOptions(), staleTime: 0 }).then((wsList) => {
          workspaceStore.getState().hydrateWorkspace(wsList);
        });
      }
    });

    const unsubMemberRemoved = ws.on("member:removed", (p) => {
      const { user_id } = p as MemberRemovedPayload;
      const myUserId = authStore.getState().user?.id;
      if (user_id === myUserId) {
        const wsId = workspaceStore.getState().workspace?.id;
        if (wsId) clearWorkspaceStorage(defaultStorage, wsId);
        logger.warn("removed from workspace, switching");
        onToast?.("You were removed from this workspace", "info");
        qc.fetchQuery({ ...workspaceListOptions(), staleTime: 0 }).then((wsList) => {
          workspaceStore.getState().hydrateWorkspace(wsList);
        });
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
      const currentWsId = workspaceStore.getState().workspace?.id;
      if (currentWsId) {
        qc.invalidateQueries({ queryKey: workspaceKeys.invitations(currentWsId) });
        qc.invalidateQueries({ queryKey: workspaceKeys.members(currentWsId) });
      }
    });
    const unsubInvitationDeclined = ws.on("invitation:declined", () => {
      const currentWsId = workspaceStore.getState().workspace?.id;
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
        ["task-messages", payload.task_id],
        (old = []) => {
          if (old.some((m) => m.seq === payload.seq)) return old;
          return [...old, payload].sort((a, b) => a.seq - b.seq);
        },
      );
      chatWsLogger.debug("task:message (global)", {
        task_id: payload.task_id,
        seq: payload.seq,
        type: payload.type,
      });
    });

    // Helpers reused by chat lifecycle handlers.
    const invalidatePendingAggregate = () => {
      const id = workspaceStore.getState().workspace?.id;
      if (id) qc.invalidateQueries({ queryKey: chatKeys.pendingTasks(id) });
    };
    const invalidateSessionLists = () => {
      const id = workspaceStore.getState().workspace?.id;
      if (id) {
        qc.invalidateQueries({ queryKey: chatKeys.sessions(id) });
        qc.invalidateQueries({ queryKey: chatKeys.allSessions(id) });
      }
    };

    const unsubChatMessage = ws.on("chat:message", (p) => {
      const payload = p as { chat_session_id: string };
      chatWsLogger.info("chat:message (global)", { chat_session_id: payload.chat_session_id });
      qc.invalidateQueries({ queryKey: chatKeys.messages(payload.chat_session_id) });
      qc.invalidateQueries({ queryKey: chatKeys.pendingTask(payload.chat_session_id) });
      invalidatePendingAggregate();
    });

    const unsubChatDone = ws.on("chat:done", (p) => {
      const payload = p as ChatDonePayload;
      chatWsLogger.info("chat:done (global)", {
        task_id: payload.task_id,
        chat_session_id: payload.chat_session_id,
      });
      // Assistant message was just written and task flipped out of 'running'.
      // Clear pending-task cache immediately so the live-timeline-vs-assistant
      // race window collapses to zero — the subsequent refetch will confirm.
      qc.setQueryData(chatKeys.pendingTask(payload.chat_session_id), {});
      qc.invalidateQueries({ queryKey: chatKeys.messages(payload.chat_session_id) });
      qc.invalidateQueries({ queryKey: chatKeys.pendingTask(payload.chat_session_id) });
      invalidatePendingAggregate();
      // Assistant message just landed → has_unread may have flipped to true.
      invalidateSessionLists();
    });

    const unsubTaskCompleted = ws.on("task:completed", (p) => {
      const payload = p as TaskCompletedPayload;
      if (!payload.chat_session_id) return; // issue tasks handled elsewhere
      chatWsLogger.info("task:completed (global, chat)", {
        task_id: payload.task_id,
        chat_session_id: payload.chat_session_id,
      });
      qc.setQueryData(chatKeys.pendingTask(payload.chat_session_id), {});
      qc.invalidateQueries({ queryKey: chatKeys.messages(payload.chat_session_id) });
      qc.invalidateQueries({ queryKey: chatKeys.pendingTask(payload.chat_session_id) });
      invalidatePendingAggregate();
    });

    const unsubTaskFailed = ws.on("task:failed", (p) => {
      const payload = p as TaskFailedPayload;
      if (!payload.chat_session_id) return;
      chatWsLogger.warn("task:failed (global, chat)", {
        task_id: payload.task_id,
        chat_session_id: payload.chat_session_id,
      });
      // No new message; just flip the pending signal.
      qc.setQueryData(chatKeys.pendingTask(payload.chat_session_id), {});
      qc.invalidateQueries({ queryKey: chatKeys.pendingTask(payload.chat_session_id) });
      invalidatePendingAggregate();
    });

    const unsubChatSessionRead = ws.on("chat:session_read", (p) => {
      const payload = p as { chat_session_id: string };
      chatWsLogger.info("chat:session_read (global)", payload);
      invalidateSessionLists();
    });

    return () => {
      unsubAny();
      unsubIssueUpdated();
      unsubIssueCreated();
      unsubIssueDeleted();
      unsubInboxNew();
      unsubCommentCreated();
      unsubCommentUpdated();
      unsubCommentDeleted();
      unsubActivityCreated();
      unsubReactionAdded();
      unsubReactionRemoved();
      unsubIssueReactionAdded();
      unsubIssueReactionRemoved();
      unsubSubscriberAdded();
      unsubSubscriberRemoved();
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
      unsubTaskCompleted();
      unsubTaskFailed();
      unsubChatSessionRead();
      timers.forEach(clearTimeout);
      timers.clear();
    };
  }, [ws, qc, authStore, workspaceStore, onToast]);

  // Reconnect -> refetch all data to recover missed events
  useEffect(() => {
    if (!ws) return;

    const unsub = ws.onReconnect(async () => {
      logger.info("reconnected, refetching all data");
      try {
        const wsId = workspaceStore.getState().workspace?.id;
        if (wsId) {
          qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
          qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
          qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
          qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
          qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
          qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
          qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
          qc.invalidateQueries({ queryKey: autopilotKeys.all(wsId) });
        }
        qc.invalidateQueries({ queryKey: workspaceKeys.list() });
      } catch (e) {
        logger.error("reconnect refetch failed", e);
      }
    });

    return unsub;
  }, [ws, qc, workspaceStore]);
}
