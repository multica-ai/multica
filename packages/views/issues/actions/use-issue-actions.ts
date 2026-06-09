"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import { ApiError } from "@multica/core/api";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { pinListOptions, useCreatePin, useDeletePin } from "@multica/core/pins";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

const BACKLOG_HINT_LS_KEY = "multica:backlog-agent-hint-dismissed";

export interface UseIssueActionsResult {
  isPinned: boolean;
  canDelete: boolean;
  updateField: (updates: Partial<UpdateIssueRequest>) => void;
  togglePin: () => void;
  copyLink: () => Promise<void>;
  openCreateIssueFromCurrent: () => void;
  openCreateSubIssue: () => void;
  openSetParent: () => void;
  openAddChild: () => void;
  openDeleteConfirm: (opts?: { onDeletedNavigateTo?: string }) => void;
  openArchiveConfirm: () => void;
  openUnarchiveConfirm: () => void;
}

/**
 * Accepts a nullable issue so callers can invoke the hook before they've
 * early-returned on a missing issue. Returned handlers are safe no-ops when
 * `issue` is null.
 */
export function useIssueActions(issue: Issue | null): UseIssueActionsResult {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const user = useAuthStore((s) => s.user);
  const userId = user?.id;

  const { data: pinnedItems = [] } = useQuery({
    ...pinListOptions(wsId, userId ?? ""),
    enabled: !!userId,
  });

  const isPinned =
    !!issue &&
    pinnedItems.some(
      (p) => p.item_type === "issue" && p.item_id === issue.id,
    );

  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const currentMemberRole = useMemo(
    () => members.find((m) => m.user_id === userId)?.role,
    [members, userId],
  );

  const isCreator =
    !!user && !!issue && issue.creator_type === "member" && issue.creator_id === user.id;
  const isAdmin =
    currentMemberRole === "owner" || currentMemberRole === "admin";
  const canDelete = isAdmin || isCreator;

  const updateIssue = useUpdateIssue();
  const createPin = useCreatePin();
  const deletePin = useDeletePin();
  const openModal = useModalStore((s) => s.open);

  const issueId = issue?.id ?? null;
  const issueStatus = issue?.status ?? null;
  const issueIdentifier = issue?.identifier ?? null;
  const issueTitle = issue?.title ?? "";
  const issueDescription = issue?.description ?? "";
  const issueProjectId = issue?.project_id ?? null;

  const updateField = useCallback(
    (updates: Partial<UpdateIssueRequest>) => {
      if (!issueId) return;
      updateIssue.mutate(
        { id: issueId, ...updates },
        {
          onError: (err) => {
            if (err instanceof ApiError && err.status === 409) {
              toast.warning(t(($) => $.detail.description_conflict));
              return;
            }
            toast.error(
              err instanceof Error && err.message
                ? err.message
                : t(($) => $.detail.update_failed),
            );
          },
        },
      );
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
    [issueId, issueStatus, updateIssue, openModal, t],
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
    if (!issueIdentifier) return;
    const url = navigation.getShareableUrl(paths.issueDetail(issueIdentifier));
    try {
      await navigator.clipboard.writeText(url);
      toast.success(t(($) => $.detail.link_copied));
    } catch {
      toast.error(t(($) => $.detail.link_copy_failed));
    }
  }, [paths, issueIdentifier, navigation, t]);

  const openCreateSubIssue = useCallback(() => {
    if (!issueId) return;
    openModal("create-issue", {
      parent_issue_id: issueId,
      parent_issue_identifier: issueIdentifier,
      ...(issueProjectId ? { project_id: issueProjectId } : {}),
    });
  }, [openModal, issueId, issueIdentifier, issueProjectId]);

  const openCreateIssueFromCurrent = useCallback(() => {
    if (!issueId) return;
    openModal("create-issue", {
      title: issueTitle,
      description: issueDescription,
      parent_issue_id: issueId,
      parent_issue_identifier: issueIdentifier,
      block_issue_id_on_create: issueId,
      ...(issueProjectId ? { project_id: issueProjectId } : {}),
    });
  }, [openModal, issueId, issueTitle, issueDescription, issueIdentifier, issueProjectId]);

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

  const openArchiveConfirm = useCallback(() => {
    if (!issueId) return;
    openModal("issue-archive-confirm", { issueId, issueTitle });
  }, [openModal, issueId, issueTitle]);

  const openUnarchiveConfirm = useCallback(() => {
    if (!issueId) return;
    openModal("issue-unarchive-confirm", { issueId, issueTitle });
  }, [openModal, issueId, issueTitle]);

  return {
    isPinned,
    canDelete,
    updateField,
    togglePin,
    copyLink,
    openCreateIssueFromCurrent,
    openCreateSubIssue,
    openSetParent,
    openAddChild,
    openDeleteConfirm,
    openArchiveConfirm,
    openUnarchiveConfirm,
  };
}
