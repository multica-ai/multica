"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Bot,
  CircleUser,
  Copy,
  Layers,
  Link2,
  ListTodo,
  Star,
  Trash2,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import type { IssueView } from "@multica/core/api/schemas";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects/queries";
import { memberListOptions } from "@multica/core/workspace/queries";
import { issueViewEverywhereOptions } from "@multica/core/issue-views/queries";
import { issueViewFilterCount } from "@multica/core/issue-views/baseline";
import { issueViewSeedDefinition } from "@multica/core/issue-views/definition";
import {
  BUILTIN_VIEW_KEYS,
  builtinViewItemId,
  savedViewItemId,
  type BuiltinViewKey,
} from "@multica/core/issue-views/sidebar-views";
import { issueViewTarget } from "@multica/core/issue-views/view-target";
import { useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import { ActorAvatar } from "../common/actor-avatar";
import {
  CollectionCreateButton,
  CollectionPageHeader,
  CollectionScopeItems,
} from "../layout/collection-page";
import {
  CollectionEmptyValue,
  CollectionList,
  CollectionNameCell,
  CollectionRowMenu,
  type CollectionColumn,
} from "../layout/collection-list";
import { useT } from "../i18n";
import { useBuiltinViewNavigation } from "./builtin-views";
import { DeleteViewDialog } from "./delete-view-dialog";
import { useIssueViewActions } from "./use-view-actions";

type DirectoryScope = "all" | "builtin" | "mine" | "shared";
const SCOPES: readonly DirectoryScope[] = ["all", "builtin", "mine", "shared"];

const EMPTY_VIEWS: IssueView[] = [];
const WORKSPACE_SCOPE = { scope_type: "workspace" } as const;

type DirectoryRow =
  | { kind: "builtin"; id: string; key: BuiltinViewKey; name: string }
  | { kind: "view"; id: string; view: IssueView };

const BUILTIN_ICON: Record<BuiltinViewKey, React.ReactNode> = {
  my_issues: <CircleUser className="size-4 shrink-0 text-muted-foreground" />,
  my_agents: <Bot className="size-4 shrink-0 text-muted-foreground" />,
  all_issues: <ListTodo className="size-4 shrink-0 text-muted-foreground" />,
};

/** Every view the user can open, and the switch that puts it in the sidebar. */
export function AllViewsPage() {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const userId = useAuthStore((s) => s.user?.id);
  const actions = useIssueViewActions();
  const builtinNav = useBuiltinViewNavigation();
  const setActive = useActiveIssueViewStore((s) => s.setActive);
  const { data: views = EMPTY_VIEWS } = useQuery(issueViewEverywhereOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const [scope, setScope] = useState<DirectoryScope>("all");
  const [deleteTarget, setDeleteTarget] = useState<IssueView | null>(null);

  const projectTitle = useMemo(() => new Map(projects.map((p) => [p.id, p.title])), [projects]);
  const memberName = useMemo(() => new Map(members.map((m) => [m.user_id, m.name])), [members]);

  const builtins = BUILTIN_VIEW_KEYS.map((key) => ({ key, name: t(($) => $.views.builtin[key]) }));
  const sortedViews = useMemo(
    () => [...views].sort((a, b) => a.name.localeCompare(b.name)),
    [views],
  );
  const counts: Record<DirectoryScope, number> = {
    all: builtins.length + views.length,
    builtin: builtins.length,
    mine: views.filter((view) => view.owner_id === userId).length,
    shared: views.filter((view) => view.visibility === "workspace").length,
  };

  const shownBuiltins = scope === "all" || scope === "builtin" ? builtins : [];
  const shownViews =
    scope === "builtin"
      ? []
      : sortedViews.filter(
          (view) =>
            scope === "all" ||
            (scope === "mine" ? view.owner_id === userId : view.visibility === "workspace"),
        );

  const scopeLabel: Record<DirectoryScope, string> = {
    all: t(($) => $.views.directory.scope_all),
    builtin: t(($) => $.views.directory.scope_builtin),
    mine: t(($) => $.views.directory.scope_mine),
    shared: t(($) => $.views.directory.scope_shared),
  };

  const location = (view: IssueView) => {
    if (view.scope_type === "project") {
      return projectTitle.get(view.scope_id ?? "") ?? t(($) => $.views.directory.location_project);
    }
    return view.scope_type === "my" ? t(($) => $.views.builtin.my_issues) : t(($) => $.views.builtin.all_issues);
  };

  const newView = () =>
    actions.create({
      scope: WORKSPACE_SCOPE,
      scopeVariant: null,
      definition: issueViewSeedDefinition({ query: {}, display: {} }),
      renameIn: "header",
    });

  const rows: DirectoryRow[] = [
    ...shownBuiltins.map(({ key, name }) => ({
      kind: "builtin" as const,
      id: builtinViewItemId(key),
      key,
      name,
    })),
    ...shownViews.map((view) => ({ kind: "view" as const, id: savedViewItemId(view.id), view })),
  ];
  const rowName = (row: DirectoryRow) => (row.kind === "builtin" ? row.name : row.view.name);

  const columns: CollectionColumn<DirectoryRow>[] = [
    {
      key: "name",
      header: t(($) => $.views.directory.col_name),
      cell: (row) => (
        <CollectionNameCell
          icon={
            row.kind === "builtin" ? (
              BUILTIN_ICON[row.key]
            ) : (
              <Layers className="size-4 shrink-0 text-muted-foreground" />
            )
          }
          title={rowName(row)}
        />
      ),
    },
    {
      key: "location",
      header: t(($) => $.views.directory.col_location),
      width: 176,
      cell: (row) =>
        row.kind === "view" ? (
          <span className="truncate">{location(row.view)}</span>
        ) : (
          <CollectionEmptyValue />
        ),
    },
    {
      key: "filters",
      header: t(($) => $.views.directory.col_filters),
      width: 64,
      align: "end",
      className: "tabular-nums",
      cell: (row) => {
        const count = row.kind === "view" ? issueViewFilterCount(row.view.query) : 0;
        return count > 0 ? count : <CollectionEmptyValue />;
      },
    },
    {
      key: "visibility",
      header: t(($) => $.views.directory.col_visibility),
      width: 136,
      cell: (row) => (
        <span className="truncate">
          {row.kind === "view" && row.view.visibility !== "workspace"
            ? t(($) => $.views.only_me)
            : t(($) => $.views.workspace_members)}
        </span>
      ),
    },
    {
      key: "owner",
      header: t(($) => $.views.directory.col_owner),
      width: 144,
      cell: (row) =>
        row.kind === "builtin" ? (
          t(($) => $.views.directory.owner_builtin)
        ) : (
          <>
            <ActorAvatar actorType="member" actorId={row.view.owner_id} size="sm" />
            <span className="min-w-0 truncate">{memberName.get(row.view.owner_id) ?? ""}</span>
          </>
        ),
    },
    {
      key: "star",
      width: 24,
      core: true,
      interactive: true,
      cell: (row) => {
        const name = rowName(row);
        const starred = actions.sidebar.isStarred(row.id);
        return (
          <IconButton
            label={
              starred
                ? t(($) => $.views.directory.unstar, { name })
                : t(($) => $.views.directory.star, { name })
            }
            aria-pressed={starred}
            size="icon-xs"
            className={starred ? "text-warning" : "text-muted-foreground"}
            onClick={() => (starred ? actions.sidebar.unstar(row.id) : actions.sidebar.star(row.id))}
          >
            <Star className={cn(starred && "fill-current")} />
          </IconButton>
        );
      },
    },
  ];

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={Layers}
        title={scopeLabel[scope]}
        menu={
          <CollectionScopeItems
            value={scope}
            onChange={setScope}
            options={SCOPES.map((value) => ({ value, label: scopeLabel[value], count: counts[value] }))}
          />
        }
        actions={<CollectionCreateButton label={t(($) => $.views.directory.new_view)} onClick={newView} />}
      />
      <CollectionList
        label={scopeLabel[scope]}
        columns={columns}
        rows={rows}
        getRowId={(row) => row.id}
        getRowHref={(row) =>
          row.kind === "builtin"
            ? builtinNav.hrefFor(row.key)
            : issueViewTarget(wsId, row.view, paths).href
        }
        getRowTitle={rowName}
        onRowNavigate={(row) => {
          if (row.kind === "builtin") builtinNav.prepare(row.key);
          else setActive(issueViewTarget(wsId, row.view, paths).containerKey, row.view.id);
        }}
        rowMenu={(row) => {
          if (row.kind !== "view") return null;
          const { view } = row;
          return (
            <CollectionRowMenu
              label={t(($) => $.views.directory.row_actions, { name: view.name })}
              contentClassName="w-auto min-w-44"
            >
              <DropdownMenuItem onClick={() => actions.duplicate(view, "header")}>
                <Copy className="size-3.5" />
                {t(($) => $.views.duplicate)}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => actions.copyLink(issueViewTarget(wsId, view, paths).href)}
              >
                <Link2 className="size-3.5" />
                {t(($) => $.views.copy_link)}
              </DropdownMenuItem>
              {actions.canManage(view) && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="destructive" onClick={() => setDeleteTarget(view)}>
                    <Trash2 className="size-3.5" />
                    {t(($) => $.views.delete)}
                  </DropdownMenuItem>
                </>
              )}
            </CollectionRowMenu>
          );
        }}
        empty={t(($) => $.views.directory.no_matches)}
      />
      <DeleteViewDialog
        view={deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        onConfirm={actions.remove}
      />
    </div>
  );
}
