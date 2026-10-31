"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { copyText } from "@multica/ui/lib/clipboard";
import { ApiError } from "@multica/core/api/client";
import type { CreateIssueViewRequest, IssueView } from "@multica/core/api/schemas";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { memberListOptions } from "@multica/core/workspace/queries";
import { canManageIssueView, type IssueViewScope } from "@multica/core/issue-views/queries";
import {
  useCreateIssueView,
  useDeleteIssueView,
  useUpdateIssueView,
} from "@multica/core/issue-views/mutations";
import { useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import { savedViewItemId, useSidebarViews } from "@multica/core/issue-views/sidebar-views";
import { issueViewTarget } from "@multica/core/issue-views/view-target";
import type { IssueViewDefinition } from "@multica/core/issue-views/definition";
import {
  useIssueViewRenameStore,
  type IssueViewRenameSurface,
} from "@multica/core/issue-views/rename-store";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";

export interface CreateViewInput {
  scope: IssueViewScope;
  scopeVariant: CreateIssueViewRequest["scope_variant"];
  definition: IssueViewDefinition;
  name?: string;
  /** Where the new view's name opens for editing. */
  renameIn: IssueViewRenameSurface;
}

/**
 * Every write a saved view offers, shared by the sidebar, the page header and
 * the All views directory so each entrance behaves the same.
 */
export function useIssueViewActions() {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const userId = useAuthStore((s) => s.user?.id);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const role = useMemo(
    () => members.find((member) => member.user_id === userId)?.role ?? null,
    [members, userId],
  );
  const sidebar = useSidebarViews(wsId, userId);
  const createView = useCreateIssueView(wsId);
  const updateView = useUpdateIssueView(wsId);
  const deleteView = useDeleteIssueView(wsId);
  const setActive = useActiveIssueViewStore((s) => s.setActive);
  const startRename = useIssueViewRenameStore((s) => s.start);
  const { star } = sidebar;

  const open = useCallback(
    (view: Pick<IssueView, "id" | "scope_type" | "scope_id">) => {
      const target = issueViewTarget(wsId, view, paths);
      setActive(target.containerKey, view.id);
      if (navigation.pathname !== target.path) navigation.push(target.href);
    },
    [navigation, paths, setActive, wsId],
  );

  const create = useCallback(
    ({ scope, scopeVariant, definition, name, renameIn }: CreateViewInput) => {
      createView.mutate(
        {
          name: name ?? t(($) => $.views.untitled),
          scope_type: scope.scope_type,
          scope_id: scope.scope_type === "project" ? scope.scope_id : null,
          scope_variant: scopeVariant ?? null,
          visibility: "private",
          definition_version: 1,
          query: definition.query,
          display: definition.display,
        },
        {
          onSuccess: (created) => {
            if (!created) return;
            star(savedViewItemId(created.id));
            open(created);
            startRename(created.id, renameIn);
          },
          onError: () => toast.error(t(($) => $.views.toast_create_failed)),
        },
      );
    },
    [createView, open, star, startRename, t],
  );

  const update = useCallback(
    (
      view: IssueView,
      patch: Partial<Pick<IssueView, "name" | "query" | "display">> & {
        visibility?: "private" | "workspace";
      },
      success?: string,
    ) => {
      updateView.mutate(
        { id: view.id, ...patch, expected_revision: view.revision },
        {
          onSuccess: () => {
            if (success) toast.success(success);
          },
          onError: (err) => {
            toast.error(
              err instanceof ApiError && err.status === 409
                ? t(($) => $.views.toast_conflict)
                : t(($) => $.views.toast_save_failed),
            );
          },
        },
      );
    },
    [t, updateView],
  );

  return {
    sidebar,
    canManage: useCallback((view: IssueView) => canManageIssueView(view, userId ?? null, role), [role, userId]),
    open,
    create,
    duplicate: useCallback(
      (view: IssueView, renameIn: IssueViewRenameSurface) =>
        create({
          scope: issueViewTarget(wsId, view, paths).scope,
          scopeVariant: (view.scope_variant ?? null) as CreateIssueViewRequest["scope_variant"],
          definition: { query: view.query, display: view.display },
          name: t(($) => $.views.copy_name, { name: view.name }),
          renameIn,
        }),
      [create, paths, t, wsId],
    ),
    rename: useCallback(
      (view: IssueView, name: string) => {
        const next = name.trim();
        if (next && next !== view.name) update(view, { name: next });
      },
      [update],
    ),
    save: useCallback(
      (view: IssueView, definition: IssueViewDefinition) =>
        update(view, definition, t(($) => $.views.toast_saved)),
      [t, update],
    ),
    setVisibility: useCallback(
      (view: IssueView, visibility: "private" | "workspace") => {
        if (view.visibility !== visibility) update(view, { visibility });
      },
      [update],
    ),
    remove: useCallback(
      (view: IssueView) =>
        deleteView.mutateAsync(view.id).then(() => {
          const target = issueViewTarget(wsId, view, paths);
          const active = useActiveIssueViewStore.getState().active[target.containerKey];
          if (active === view.id) setActive(target.containerKey, null);
        }),
      [deleteView, paths, setActive, wsId],
    ),
    copyLink: useCallback(
      (href: string) => {
        void copyText(navigation.getShareableUrl(href)).then((ok) => {
          if (ok) toast.success(t(($) => $.views.link_copied));
        });
      },
      [navigation, t],
    ),
  };
}

export type IssueViewActions = ReturnType<typeof useIssueViewActions>;
