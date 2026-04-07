"use client";

import { useEffect } from "react";
import type { WSClient } from "@/shared/api";
import { toast } from "sonner";
import { useIssueStore } from "@/features/issues";
import { useInboxStore } from "@/features/inbox";
import { useWorkspaceStore } from "@/features/workspace";
import { useAuthStore } from "@/features/auth";
import { createLogger } from "@/shared/logger";
import { api } from "@/shared/api";
import type {
  MemberAddedPayload,
  WorkspaceDeletedPayload,
  MemberRemovedPayload,
  IssueUpdatedPayload,
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
  // Main sync: onAny → refreshMap with debounce
  useEffect(() => {
    if (!ws) return;

    const refreshMap: Record<string, () => void> = {
      issue: () => void useIssueStore.getState().fetch(),
      inbox: () => void useInboxStore.getState().fetch(),
      agent: () => void useWorkspaceStore.getState().refreshAgents(),
      member: () => void useWorkspaceStore.getState().refreshMembers(),
      workspace: () => {
        // Lightweight: only re-fetch workspace list, don't hydrate everything.
        // workspace:deleted is handled by a precise side-effect handler below.
        api.listWorkspaces().then((wsList) => {
          const current = useWorkspaceStore.getState().workspace;
          const updated = current
            ? wsList.find((w) => w.id === current.id)
            : null;
          if (updated) useWorkspaceStore.getState().updateWorkspace(updated);
        }).catch((err) => {
          logger.error("workspace refresh failed", err);
        });
      },
      skill: () => void useWorkspaceStore.getState().refreshSkills(),
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
      const prefix = msg.type.split(":")[0] ?? "";
      const refresh = refreshMap[prefix];
      if (refresh) debouncedRefresh(prefix, refresh);
    });

    // --- Side-effect handlers (toast, navigation, cross-store sync) ---

    // Keep inbox issue_status in sync when issues change
    const unsubIssueUpdated = ws.on("issue:updated", (p) => {
      const { issue } = p as IssueUpdatedPayload;
      if (issue?.id && issue?.status) {
        useInboxStore.getState().updateIssueStatus(issue.id, issue.status);
      }
    });

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
        await Promise.all([
          useIssueStore.getState().fetch(),
          useInboxStore.getState().fetch(),
          useWorkspaceStore.getState().refreshAgents(),
          useWorkspaceStore.getState().refreshMembers(),
          useWorkspaceStore.getState().refreshSkills(),
        ]);
      } catch (e) {
        logger.error("reconnect refetch failed", e);
      }
    });

    return unsub;
  }, [ws]);
}
