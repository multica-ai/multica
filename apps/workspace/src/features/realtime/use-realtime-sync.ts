"use client";

import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { WSClient } from "@/shared/api";
import { toast } from "sonner";
import { useIssueStore } from "@/features/issues";
import { useInboxStore } from "@/features/inbox";
import { useWorkspaceStore } from "@/features/workspace";
import { useAuthStore } from "@/features/auth";
import { createLogger } from "@/shared/logger";
import { prepareQueryCacheForReconnect, queryKeys } from "@/shared/query";
import { inboxQueryOptions } from "@/features/inbox/queries";
import { issuesListQueryOptions } from "@/features/issues/queries";
import { runtimesQueryOptions } from "@/features/runtimes/queries";
import {
  workspaceAgentsQueryOptions,
  workspaceMembersQueryOptions,
  workspaceSkillsQueryOptions,
  workspacesQueryOptions,
} from "@/features/workspace/queries";
import type {
  MemberAddedPayload,
  WorkspaceDeletedPayload,
  MemberRemovedPayload,
  IssueUpdatedPayload,
  IssueCreatedPayload,
  IssueDeletedPayload,
  InboxNewPayload,
} from "@/shared/types";

const logger = createLogger("realtime-sync");

/**
 * Centralized WS → store sync. Called once from WSProvider.
 *
 * Uses the "WS as invalidation signal + refetch" pattern:
 * - onAny handler extracts event prefix and calls the matching store refresh
 * - Debounce per-prefix prevents rapid-fire refetches (e.g. bulk issue updates)
 * - Precise handlers only for side effects (toast, navigation, self-check)
 *
 * Per-page events (comments, activity, subscribers, daemon) are still handled
 * by individual components via useWSEvent — not here.
 */
export function useRealtimeSync(ws: WSClient | null) {
  const queryClient = useQueryClient();

  // Main sync: onAny → refreshMap with debounce
  useEffect(() => {
    if (!ws) return;

    // Event types handled by specific handlers below — skip generic refresh
    const specificEvents = new Set([
      "issue:updated", "issue:created", "issue:deleted", "inbox:new",
    ]);

    const refreshMap: Record<string, () => void> = {
      inbox: () => {
        const workspaceId = useWorkspaceStore.getState().workspace?.id;
        if (!workspaceId) return;
        void queryClient.invalidateQueries({ queryKey: queryKeys.inbox.all(workspaceId) });
      },
      agent: () => {
        const workspaceId = useWorkspaceStore.getState().workspace?.id;
        if (!workspaceId) return;
        void queryClient.invalidateQueries({ queryKey: queryKeys.workspace.agents(workspaceId) });
      },
      member: () => {
        const workspaceId = useWorkspaceStore.getState().workspace?.id;
        if (!workspaceId) return;
        void queryClient.invalidateQueries({ queryKey: queryKeys.workspace.members(workspaceId) });
      },
      workspace: () => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.workspaces.all() });
      },
      skill: () => {
        const workspaceId = useWorkspaceStore.getState().workspace?.id;
        if (!workspaceId) return;
        void queryClient.invalidateQueries({ queryKey: queryKeys.workspace.skills(workspaceId) });
      },
      project: () => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.projects.all() });
        void queryClient.invalidateQueries({ queryKey: queryKeys.issues.all() });
      },
      daemon: () => {
        const workspaceId = useWorkspaceStore.getState().workspace?.id;
        if (!workspaceId) return;
        void queryClient.invalidateQueries({ queryKey: queryKeys.runtimes.all(workspaceId) });
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

    const unsubAny = ws.onAny((msg) => {
      const myUserId = useAuthStore.getState().user?.id;
      if (msg.actor_id && msg.actor_id === myUserId) {
        logger.debug("skipping self-event", msg.type);
        return;
      }
      if (specificEvents.has(msg.type)) return;
      const prefix = msg.type.split(":")[0] ?? "";
      const refresh = refreshMap[prefix];
      if (refresh) debouncedRefresh(prefix, refresh);
    });

    // --- Specific event handlers (granular updates, no full refetch) ---

    const unsubIssueUpdated = ws.on("issue:updated", (p) => {
      const { issue } = p as IssueUpdatedPayload;
      if (!issue?.id) return;
      useIssueStore.getState().updateIssue(issue.id, issue);
      if (issue.status) {
        useInboxStore.getState().updateIssueStatus(issue.id, issue.status);
      }
    });

    const unsubIssueCreated = ws.on("issue:created", (p) => {
      const { issue } = p as IssueCreatedPayload;
      if (issue) useIssueStore.getState().addIssue(issue);
    });

    const unsubIssueDeleted = ws.on("issue:deleted", (p) => {
      const { issue_id } = p as IssueDeletedPayload;
      if (issue_id) useIssueStore.getState().removeIssue(issue_id);
    });

    const unsubInboxNew = ws.on("inbox:new", (p) => {
      const { item } = p as InboxNewPayload;
      if (item) useInboxStore.getState().addItem(item);
    });

    // --- Side-effect handlers (toast, navigation) ---

    const unsubWsDeleted = ws.on("workspace:deleted", (p) => {
      const { workspace_id } = p as WorkspaceDeletedPayload;
      const currentWs = useWorkspaceStore.getState().workspace;
      if (currentWs?.id === workspace_id) {
        logger.warn("current workspace deleted, switching");
        toast.info("This workspace was deleted");
        useWorkspaceStore.getState().refreshWorkspaces();
      }
    });

    const unsubMemberRemoved = ws.on("member:removed", (p) => {
      const { user_id } = p as MemberRemovedPayload;
      const myUserId = useAuthStore.getState().user?.id;
      if (user_id === myUserId) {
        logger.warn("removed from workspace, switching");
        toast.info("You were removed from this workspace");
        useWorkspaceStore.getState().refreshWorkspaces();
      }
    });

    const unsubMemberAdded = ws.on("member:added", (p) => {
      const { member, workspace_name } = p as MemberAddedPayload;
      const myUserId = useAuthStore.getState().user?.id;
      if (member.user_id === myUserId) {
        useWorkspaceStore.getState().refreshWorkspaces();
        toast.info(
          `You were invited to ${workspace_name ?? "a workspace"}`,
        );
      }
    });

    return () => {
      unsubAny();
      unsubIssueUpdated();
      unsubIssueCreated();
      unsubIssueDeleted();
      unsubInboxNew();
      unsubWsDeleted();
      unsubMemberRemoved();
      unsubMemberAdded();
      timers.forEach(clearTimeout);
      timers.clear();
    };
  }, [ws]);

  // Reconnect → refetch all data to recover missed events
  useEffect(() => {
    if (!ws) return;

    const unsub = ws.onReconnect(async () => {
      logger.info("reconnected, refetching all data");
      try {
        await prepareQueryCacheForReconnect(
          queryClient,
          useWorkspaceStore.getState().workspace?.id,
        );
        const workspaceId = useWorkspaceStore.getState().workspace?.id;
        await Promise.all([
          queryClient.fetchQuery(workspacesQueryOptions()),
          ...(workspaceId
            ? [
                queryClient.fetchQuery(issuesListQueryOptions(workspaceId)),
                queryClient.fetchQuery(inboxQueryOptions(workspaceId)),
                queryClient.fetchQuery(workspaceAgentsQueryOptions(workspaceId)),
                queryClient.fetchQuery(workspaceMembersQueryOptions(workspaceId)),
                queryClient.fetchQuery(workspaceSkillsQueryOptions(workspaceId)),
                queryClient.fetchQuery(runtimesQueryOptions(workspaceId)),
              ]
            : []),
        ]);
      } catch (e) {
        logger.error("reconnect refetch failed", e);
      }
    });

    return unsub;
  }, [queryClient, ws]);
}
