"use client";

import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { pinListOptions, useCreatePin, useDeletePin } from "@multica/core/pins";
import { copyText } from "@multica/ui/lib/clipboard";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

export interface UseIssueActionsResult {
  isPinned: boolean;
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

  const updateIssue = useUpdateIssue();
  const createPin = useCreatePin();
  const deletePin = useDeletePin();
  const openModal = useModalStore((s) => s.open);

  const issueId = issue?.id ?? null;
  const issueIdentifier = issue?.identifier ?? null;
  const issueProjectId = issue?.project_id ?? null;
  const issueStatus = issue?.status ?? null;

  const updateField = useCallback(
    (updates: Partial<UpdateIssueRequest>) => {
      if (!issueId) return;
      // Assigning to an agent/squad may start a run. Route through the
      // pre-trigger confirm modal (preview + optional handoff note + "暂不开始"),
      // which applies the change itself — the four entry points share this one
      // backend-driven flow instead of guessing (MUL-3375). Every other field
      // change (status, priority, member assign, unassign) applies directly.
      //
      // Backlog is the parking lot: assigning a backlog issue never starts a run
      // (server/internal/service/issue_trigger.go), so the modal would only show
      // an empty "won't start" box with a single Apply button. Apply directly,
      // matching the batch backlog short-circuit in BatchActionToolbar.
      if (
        (updates.assignee_type === "agent" || updates.assignee_type === "squad") &&
        updates.assignee_id &&
        issueStatus !== "backlog"
      ) {
        openModal("issue-run-confirm", {
          issueIds: [issueId],
          mode: "assign",
          assigneeType: updates.assignee_type,
          assigneeId: updates.assignee_id,
        });
        return;
      }
      updateIssue.mutate(
        { id: issueId, ...updates },
        {
          onError: (err) =>
            toast.error(
              err instanceof Error && err.message
                ? err.message
                : t(($) => $.detail.update_failed),
            ),
        },
      );
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
    if (!issueId) return;
    const url = navigation.getShareableUrl(paths.issueDetail(issueId));
    if (await copyText(url)) {
      toast.success(t(($) => $.detail.link_copied));
    } else {
      toast.error(t(($) => $.detail.link_copy_failed));
    }
  }, [paths, issueId, navigation, t]);

  const openCreateSubIssue = useCallback(() => {
    if (!issueId) return;
    openModal("create-issue", {
      parent_issue_id: issueId,
      parent_issue_identifier: issueIdentifier,
      ...(issueProjectId ? { project_id: issueProjectId } : {}),
    });
  }, [openModal, issueId, issueIdentifier, issueProjectId]);

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
