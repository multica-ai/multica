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
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { errorCode } from "@multica/core/api";
import { pinListOptions, useCreatePin, useDeletePin } from "@multica/core/pins";
import { copyText } from "@multica/ui/lib/clipboard";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { runConfirmIntent } from "./run-confirm-gate";
import { useIssueSurfaceActionsOptional } from "../surface/actions-context";
import type { IssueSurfaceMutationOptions } from "../surface/actions-context";

export interface UseIssueActionsResult {
  isPinned: boolean;
  updateField: (
    updates: Partial<UpdateIssueRequest>,
    options?: IssueSurfaceMutationOptions,
  ) => void;
  openInNewTab: () => void;
  togglePin: () => void;
  copyLink: () => Promise<void>;
  openCreateSubIssue: () => void;
  openSetParent: () => void;
  removeParent: () => void;
  openAddChild: () => void;
  openDeleteConfirm: (opts?: { onDeletedFallbackPath?: string }) => void;
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
  const surfaceActions = useIssueSurfaceActionsOptional();
  const createPin = useCreatePin();
  const deletePin = useDeletePin();
  const openModal = useModalStore((s) => s.open);

  const issueId = issue?.id ?? null;
  const issueIdentifier = issue?.identifier ?? null;
  const issueProjectId = issue?.project_id ?? null;
  const issueAssigneeType = issue?.assignee_type ?? null;
  const issueAssigneeId = issue?.assignee_id ?? null;
  const { entryOf } = useIssueStatuses(wsId);
  const updateField = useCallback(
    (
      updates: Partial<UpdateIssueRequest>,
      options?: IssueSurfaceMutationOptions,
    ) => {
      if (!issueId) return;
      // The two writes that can hand work to an agent — giving it an owner, and
      // promoting it out of the parking lot — confirm first, through the shared
      // gate every single-issue entry point routes on (runConfirmIntent). The
      // modal applies the change itself; everything else applies directly.
      //
      // Not wired into drag-and-drop or the batch toolbar, which keep applying
      // directly. That is the existing split, not a new one: a drop is direct
      // manipulation whose card has already moved, and batch status was made
      // deliberately dialog-free in MUL-4155.
      const intent = issue && runConfirmIntent(issue, updates, { entryOf });
      if (intent) {
        openModal("issue-run-confirm", intent);
        return;
      }
      if (surfaceActions) {
        surfaceActions.updateIssue(issueId, updates, {
          errorMessage: t(($) => $.detail.update_failed),
          ...options,
        });
      } else {
        updateIssue.mutate(
          { id: issueId, ...updates },
          {
            onSuccess: options?.onSuccess,
            onError: (err) => {
              toast.error(
                errorCode(err) === "revision_conflict"
                  ? t(($) => $.revision.conflict)
                  : err instanceof Error && err.message
                  ? err.message
                  : t(($) => $.detail.update_failed),
              );
              options?.onError?.(err);
            },
            onSettled: () => options?.onSettled?.(),
          },
        );
      }
    },
    [issue, issueId, entryOf, surfaceActions, updateIssue, openModal, t],
  );

  // Explicit "open it somewhere else" CTA, so the new tab takes focus
  // (`activate: true`) — the user is asking to move into the new context, not
  // to stash it for later the way modifier-click does. Same contract as the
  // table row open and the attachment preview's "Open in new tab".
  //
  // Only desktop implements `openInNewTab`; on web it is undefined and we fall
  // back to a real browser tab via the shareable URL.
  const openInNewTab = useCallback(() => {
    if (!issueId) return;
    // Identifier form, same as copyLink: on web this becomes a real browser
    // tab at the shareable URL, so it is a link the user sees and may copy out
    // of the address bar. Opening on the UUID would also make the route
    // immediately rewrite the fresh tab's URL.
    const path = paths.issueDetail(issueIdentifier || issueId);
    if (navigation.openInNewTab) {
      navigation.openInNewTab(path, issueIdentifier ?? undefined, {
        activate: true,
      });
      return;
    }
    window.open(
      navigation.getShareableUrl(path),
      "_blank",
      "noopener,noreferrer",
    );
  }, [issueId, issueIdentifier, navigation, paths]);

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
    // Share the identifier form (`/{ws}/issues/MUL-123`): a pasted link should
    // say which issue it points at. The UUID form stays valid, so links copied
    // before this still resolve.
    const url = navigation.getShareableUrl(paths.issueDetail(issueIdentifier || issueId));
    if (await copyText(url)) {
      toast.success(t(($) => $.detail.link_copied));
    } else {
      toast.error(t(($) => $.detail.link_copy_failed));
    }
  }, [paths, issueId, issueIdentifier, navigation, t]);

  const openCreateSubIssue = useCallback(() => {
    if (!issueId) return;
    openModal("create-issue", {
      parent_issue_id: issueId,
      parent_issue_identifier: issueIdentifier,
      ...(issueProjectId ? { project_id: issueProjectId } : {}),
      // Inherit the parent's assignee (member/agent/squad) so a sub-issue
      // created from the "Add sub-issue" entry starts with the same owner
      // (discussion #1728). The modal keys off whether these fields are
      // present, not their value, so a seed overrides the sticky last-used
      // assignee it would otherwise fall back to, while omitting both for
      // an unassigned parent leaves that fallback intact. Seed the two
      // together — assignee_type is meaningless without assignee_id.
      ...(issueAssigneeType && issueAssigneeId
        ? { assignee_type: issueAssigneeType, assignee_id: issueAssigneeId }
        : {}),
    });
  }, [
    openModal,
    issueId,
    issueIdentifier,
    issueProjectId,
    issueAssigneeType,
    issueAssigneeId,
  ]);

  const openSetParent = useCallback(() => {
    if (!issueId) return;
    openModal("issue-set-parent", { issueId });
  }, [openModal, issueId]);

  // Detach from the parent and promote to a standalone issue. Reversible
  // (Set parent re-links it), non-destructive, and mirrors the clear-date
  // actions — so it applies directly instead of a confirm modal. `stage`
  // only orders sub-issues under a parent, so clear it in the same write to
  // avoid an orphaned value on a standalone issue. The success toast fires
  // from onSuccess, not eagerly after mutate() — otherwise a request that
  // fails on permission/network/validation would flash "removed" before the
  // error toast and the optimistic rollback (false confirmation).
  const removeParent = useCallback(() => {
    if (!issueId) return;
    if (surfaceActions) {
      surfaceActions.updateIssue(
        issueId,
        {
          parent_issue_id: null,
          stage: null,
        },
        {
          onSuccess: () =>
            toast.success(t(($) => $.actions.remove_parent_issue_success)),
          errorMessage: t(($) => $.detail.update_failed),
        },
      );
    } else {
      updateIssue.mutate(
        {
          id: issueId,
          parent_issue_id: null,
          stage: null,
        },
        {
          onSuccess: () =>
            toast.success(t(($) => $.actions.remove_parent_issue_success)),
          onError: (err) =>
            toast.error(
              err instanceof Error && err.message
                ? err.message
                : t(($) => $.detail.update_failed),
            ),
        },
      );
    }
  }, [issueId, surfaceActions, updateIssue, t]);

  const openAddChild = useCallback(() => {
    if (!issueId) return;
    openModal("issue-add-child", { issueId });
  }, [openModal, issueId]);

  const openDeleteConfirm = useCallback(
    (opts?: { onDeletedFallbackPath?: string }) => {
      if (!issueId) return;
      openModal("issue-delete-confirm", {
        issueId,
        identifier: issueIdentifier,
        onDeletedFallbackPath: opts?.onDeletedFallbackPath,
      });
    },
    [openModal, issueId, issueIdentifier],
  );

  return {
    isPinned,
    updateField,
    openInNewTab,
    togglePin,
    copyLink,
    openCreateSubIssue,
    openSetParent,
    removeParent,
    openAddChild,
    openDeleteConfirm,
  };
}
