"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type {
  Issue,
  MemberWithUser,
  Agent,
  Project,
  UpdateIssueRequest,
} from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import {
  memberListOptions,
  agentListOptions,
} from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { pinListOptions, useCreatePin, useDeletePin } from "@multica/core/pins";
import { canAssignAgent } from "../components/pickers";
import { useNavigation } from "../../navigation";

const BACKLOG_HINT_LS_KEY = "multica:backlog-agent-hint-dismissed";

export interface UseIssueActionsResult {
  // Derived data for rendering menu rows
  members: MemberWithUser[];
  agents: Agent[];
  projects: Project[];
  isPinned: boolean;
  // Handlers
  updateField: (updates: Partial<UpdateIssueRequest>) => void;
  togglePin: () => void;
  copyLink: () => Promise<void>;
  openCreateSubIssue: () => void;
  openSetParent: () => void;
  openAddChild: () => void;
  openDeleteConfirm: (opts?: { onDeletedNavigateTo?: string }) => void;
}

/**
 * Accepts a nullable issue so callers can invoke the hook before they've
 * early-returned on a missing issue. Returned handlers are safe no-ops when
 * `issue` is null.
 */
export function useIssueActions(issue: Issue | null): UseIssueActionsResult {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const user = useAuthStore((s) => s.user);
  const userId = user?.id;

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: pinnedItems = [] } = useQuery({
    ...pinListOptions(wsId, userId ?? ""),
    enabled: !!userId,
  });

  const currentMemberRole = useMemo(
    () => members.find((m) => m.user_id === userId)?.role,
    [members, userId],
  );
  const filteredAgents = useMemo(
    () =>
      agents.filter(
        (a) => !a.archived_at && canAssignAgent(a, userId, currentMemberRole),
      ),
    [agents, userId, currentMemberRole],
  );
  const isPinned =
    !!issue &&
    pinnedItems.some(
      (p) => p.item_type === "issue" && p.item_id === issue.id,
    );

  const updateIssue = useUpdateIssue();
  const createPin = useCreatePin();
  const deletePin = useDeletePin();
  const openModal = useModalStore((s) => s.open);

  const issueId = issue?.id ?? null;
  const issueStatus = issue?.status ?? null;
  const issueIdentifier = issue?.identifier ?? null;

  const updateField = useCallback(
    (updates: Partial<UpdateIssueRequest>) => {
      if (!issueId) return;
      updateIssue.mutate(
        { id: issueId, ...updates },
        { onError: () => toast.error("Failed to update issue") },
      );
      // Hint: assigning an agent to a backlog issue won't trigger execution
      // until the issue is moved to an active status.
      if (
        updates.assignee_type === "agent" &&
        updates.assignee_id &&
        issueStatus === "backlog" &&
        typeof window !== "undefined" &&
        localStorage.getItem(BACKLOG_HINT_LS_KEY) !== "true"
      ) {
        openModal("issue-backlog-agent-hint", { issueId });
      }
    },
    [issueId, issueStatus, updateIssue, openModal],
  );

  const togglePin = useCallback(() => {
    if (!issueId) return;
    if (isPinned) {
      deletePin.mutate({ itemType: "issue", itemId: issueId });
    } else {
      createPin.mutate({ item_type: "issue", item_id: issueId });
    }
  }, [isPinned, issueId, createPin, deletePin]);

  const copyLink = useCallback(async () => {
    if (!issueId) return;
    const path = paths.issueDetail(issueId);
    const url = navigation.getShareableUrl
      ? navigation.getShareableUrl(path)
      : typeof window !== "undefined"
        ? window.location.origin + path
        : path;
    try {
      await navigator.clipboard.writeText(url);
      toast.success("Link copied");
    } catch {
      toast.error("Failed to copy link");
    }
  }, [paths, issueId, navigation]);

  const openCreateSubIssue = useCallback(() => {
    if (!issueId) return;
    openModal("create-issue", {
      parent_issue_id: issueId,
      parent_issue_identifier: issueIdentifier,
    });
  }, [openModal, issueId, issueIdentifier]);

  const openSetParent = useCallback(() => {
    if (!issueId) return;
    openModal("issue-set-parent", { issueId });
  }, [openModal, issueId]);

  const openAddChild = useCallback(() => {
    if (!issueId) return;
    openModal("issue-add-child", { issueId });
  }, [openModal, issueId]);

  const openDeleteConfirm = useCallback(
    (opts?: { onDeletedNavigateTo?: string }) => {
      if (!issueId) return;
      openModal("issue-delete-confirm", {
        issueId,
        identifier: issueIdentifier,
        onDeletedNavigateTo: opts?.onDeletedNavigateTo,
      });
    },
    [openModal, issueId, issueIdentifier],
  );

  return {
    members,
    agents: filteredAgents,
    projects,
    isPinned,
    updateField,
    togglePin,
    copyLink,
    openCreateSubIssue,
    openSetParent,
    openAddChild,
    openDeleteConfirm,
  };
}
