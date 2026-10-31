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
  MoreHorizontal,
  Star,
  Trash2,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  ListGrid,
  ListGridCell,
  ListGridHeader,
  ListGridHeaderCell,
  ListGridRow,
  LIST_GRID_BOTTOM_CLEARANCE,
} from "@multica/ui/components/ui/list-grid";
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
import { rowLinkInteractiveProps, useRowLink } from "../navigation";
import { useT } from "../i18n";
import { useBuiltinViewNavigation } from "./builtin-views";
import { DeleteViewDialog } from "./delete-view-dialog";
import { useIssueViewActions } from "./use-view-actions";

type DirectoryScope = "all" | "builtin" | "mine" | "shared";
const SCOPES: readonly DirectoryScope[] = ["all", "builtin", "mine", "shared"];

const EMPTY_VIEWS: IssueView[] = [];
const WORKSPACE_SCOPE = { scope_type: "workspace" } as const;

// Star + name are the core set (<@2xl); the rest appear once there is room.
const GRID_COLS =
  "grid-cols-[0.75rem_2rem_minmax(160px,1fr)_2rem_0.75rem] " +
  "@2xl:grid-cols-[0.75rem_2rem_minmax(200px,1fr)_11rem_5rem_10rem_10rem_2rem_0.75rem]";

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
  const rowLink = useRowLink();
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

  const starButton = (id: string, name: string) => {
    const starred = actions.sidebar.isStarred(id);
    return (
      <ListGridCell className="justify-center px-0" {...rowLinkInteractiveProps}>
        <IconButton
          label={
            starred
              ? t(($) => $.views.directory.unstar, { name })
              : t(($) => $.views.directory.star, { name })
          }
          aria-pressed={starred}
          size="icon-xs"
          className={starred ? "text-warning" : "text-muted-foreground"}
          onClick={() => (starred ? actions.sidebar.unstar(id) : actions.sidebar.star(id))}
        >
          <Star className={cn(starred && "fill-current")} />
        </IconButton>
      </ListGridCell>
    );
  };

  const quiet = <span className="text-caption text-faint-foreground">—</span>;

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
      <div className="min-h-0 flex-1 overflow-auto @container">
        <ListGrid className={GRID_COLS} style={{ paddingBottom: LIST_GRID_BOTTOM_CLEARANCE }}>
          <ListGridHeader>
            <ListGridHeaderCell className="px-0" />
            <ListGridHeaderCell>{t(($) => $.views.directory.col_name)}</ListGridHeaderCell>
            <ListGridHeaderCell className="hidden @2xl:flex">{t(($) => $.views.directory.col_location)}</ListGridHeaderCell>
            <ListGridHeaderCell className="hidden @2xl:flex" align="right">
              {t(($) => $.views.directory.col_filters)}
            </ListGridHeaderCell>
            <ListGridHeaderCell className="hidden @2xl:flex">{t(($) => $.views.directory.col_visibility)}</ListGridHeaderCell>
            <ListGridHeaderCell className="hidden @2xl:flex">{t(($) => $.views.directory.col_owner)}</ListGridHeaderCell>
            <ListGridHeaderCell className="px-0" />
          </ListGridHeader>
          {shownBuiltins.length === 0 && shownViews.length === 0 ? (
            <div className="col-span-full py-16 text-center text-body text-muted-foreground">
              {t(($) => $.views.directory.no_matches)}
            </div>
          ) : null}
          {shownBuiltins.map(({ key, name }) => {
            const href = builtinNav.hrefFor(key);
            const link = rowLink(href, name, () => builtinNav.prepare(key));
            return (
              <ListGridRow key={key} className="h-10 cursor-pointer" {...link}>
                {starButton(builtinViewItemId(key), name)}
                <ListGridCell className="gap-2">
                  {BUILTIN_ICON[key]}
                  <span className="truncate text-body font-medium">{name}</span>
                </ListGridCell>
                <ListGridCell className="hidden @2xl:flex">{quiet}</ListGridCell>
                <ListGridCell className="hidden justify-end @2xl:flex">{quiet}</ListGridCell>
                <ListGridCell className="hidden @2xl:flex">
                  <span className="truncate text-caption text-muted-foreground">{t(($) => $.views.workspace_members)}</span>
                </ListGridCell>
                <ListGridCell className="hidden text-caption text-muted-foreground @2xl:flex">
                  {t(($) => $.views.directory.owner_builtin)}
                </ListGridCell>
                <ListGridCell className="px-0" />
              </ListGridRow>
            );
          })}
          {shownViews.map((view) => {
            const target = issueViewTarget(wsId, view, paths);
            const link = rowLink(target.href, view.name, () => setActive(target.containerKey, view.id));
            const filterCount = issueViewFilterCount(view.query);
            const canManage = actions.canManage(view);
            return (
              <ListGridRow key={view.id} className="h-10 cursor-pointer" {...link}>
                {starButton(savedViewItemId(view.id), view.name)}
                <ListGridCell className="gap-2">
                  <Layers className="size-4 shrink-0 text-muted-foreground" />
                  <span className="truncate text-body font-medium">{view.name}</span>
                </ListGridCell>
                <ListGridCell className="hidden @2xl:flex">
                  <span className="truncate text-caption text-muted-foreground">{location(view)}</span>
                </ListGridCell>
                <ListGridCell className="hidden justify-end tabular-nums text-caption text-muted-foreground @2xl:flex">
                  {filterCount > 0 ? filterCount : quiet}
                </ListGridCell>
                <ListGridCell className="hidden @2xl:flex">
                  <span className="truncate text-caption text-muted-foreground">
                    {view.visibility === "workspace" ? t(($) => $.views.workspace_members) : t(($) => $.views.only_me)}
                  </span>
                </ListGridCell>
                <ListGridCell className="hidden gap-1.5 @2xl:flex">
                  <ActorAvatar actorType="member" actorId={view.owner_id} size="sm" />
                  <span className="min-w-0 truncate text-caption text-muted-foreground">
                    {memberName.get(view.owner_id) ?? ""}
                  </span>
                </ListGridCell>
                <ListGridCell className="justify-center px-0" {...rowLinkInteractiveProps}>
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <IconButton
                          label={t(($) => $.views.directory.row_actions, { name: view.name })}
                          size="icon-xs"
                          className="text-muted-foreground opacity-0 group-hover/row:opacity-100 data-[popup-open]:opacity-100 [@media(pointer:coarse)]:opacity-100"
                        />
                      }
                    >
                      <MoreHorizontal />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="w-auto min-w-44">
                      <DropdownMenuItem onClick={() => actions.duplicate(view, "header")}>
                        <Copy className="size-3.5" />
                        {t(($) => $.views.duplicate)}
                      </DropdownMenuItem>
                      <DropdownMenuItem onClick={() => actions.copyLink(target.href)}>
                        <Link2 className="size-3.5" />
                        {t(($) => $.views.copy_link)}
                      </DropdownMenuItem>
                      {canManage && (
                        <>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem variant="destructive" onClick={() => setDeleteTarget(view)}>
                            <Trash2 className="size-3.5" />
                            {t(($) => $.views.delete)}
                          </DropdownMenuItem>
                        </>
                      )}
                    </DropdownMenuContent>
                  </DropdownMenu>
                </ListGridCell>
              </ListGridRow>
            );
          })}
        </ListGrid>
      </div>
      <DeleteViewDialog
        view={deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        onConfirm={actions.remove}
      />
    </div>
  );
}
