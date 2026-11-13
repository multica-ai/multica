import { useMemo } from "react";
import { useNavigation, useRoute, type RouteProp } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { baselineFromQuery } from "@multica/core/issue-views/baseline";
import { issueViewEverywhereOptions } from "@multica/core/issue-views/queries";
import { useSidebarViews, type SidebarViewItem } from "@multica/core/issue-views/sidebar-views";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { todayDateOnly } from "@multica/core/issues/date";
import { sidebarIssuesOptions } from "@multica/core/issues/queries";
import type { SidebarIssuesRequest } from "@multica/core/issues/sidebar-list";
import { useIssueUnreadIds } from "@multica/core/issues/unread";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Project } from "@multica/core/types";
import { groupIssuesByStatus } from "../../model/issue-groups";
import { useActorResolver } from "../../model/people";
import { useStatusPresentation, type StatusPresentation } from "../../model/status";
import { findViewItem, viewDetail, viewItemLabel, viewItemNeedsUnread, viewItemRequest } from "../../model/views";
import { useWorkspace } from "../../model/workspace-context";
import type { RootStackParamList } from "../../navigation/types";
import { useStrings, type Strings } from "../../strings";
import { SavedViewView, type FilterChipModel } from "./SavedViewView";

const LIMIT = 200;
const NO_IDS: string[] = [];
/** Stands in for the request while it can't be built yet; the query stays disabled. */
const IDLE_REQUEST: SidebarIssuesRequest = { table: { scope: { kind: "workspace" }, filters: {}, sort: { field: "position", direction: "asc" } } };

function filterChips(item: SidebarViewItem | null, status: (key: string) => StatusPresentation, projects: readonly Project[], strings: Strings): FilterChipModel[] {
  if (item?.kind !== "view") return [];
  const { raw } = baselineFromQuery(item.view.query);
  const chips: FilterChipModel[] = [];
  if (raw.statusFilters.length > 0) chips.push({ icon: "Filter", label: raw.statusFilters.map((key) => status(key).label).join(strings.schedule.describe.join) });
  const titles = raw.projectFilters.map((id) => projects.find((project) => project.id === id)?.title).filter(Boolean);
  if (titles.length > 0) chips.push({ icon: "FolderKanban", label: titles.join(strings.schedule.describe.join) });
  return chips;
}

export function SavedViewScreen() {
  const navigation = useNavigation();
  const { itemId } = useRoute<RouteProp<RootStackParamList, "SavedView">>().params;
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const userId = useAuthStore((state) => state.user?.id);
  const resolveActor = useActorResolver();
  const status = useStatusPresentation(workspace.id);
  const catalog = useIssueStatuses(workspace.id);
  const views = useQuery(issueViewEverywhereOptions(workspace.id));
  const projects = useQuery(projectListOptions(workspace.id));
  const sidebar = useSidebarViews(workspace.id, userId);

  const item = findViewItem(itemId, views.data ?? []);
  const needsUnread = item ? viewItemNeedsUnread(item) : false;
  const unread = useIssueUnreadIds(workspace.id, needsUnread);
  const ready = item !== null && !catalog.isPending && (!needsUnread || unread.data !== undefined);
  const request = ready ? viewItemRequest(item, catalog.statuses, todayDateOnly(), unread.data ?? NO_IDS) : null;
  const list = useQuery({
    ...sidebarIssuesOptions(workspace.id, itemId, request ?? IDLE_REQUEST, LIMIT),
    enabled: request !== null,
  });

  const groups = useMemo(() => groupIssuesByStatus(list.data?.issues ?? [], catalog, status, resolveActor), [list.data, catalog, status, resolveActor]);
  // A deleted view, or one whose scope the list can't express.
  const missing = (!item && views.isSuccess) || (ready && request === null);
  const starred = sidebar.isStarred(itemId);
  const total = list.data?.total ?? 0;

  return (
    <SavedViewView
      state={missing ? "missing" : list.data ? "ready" : list.isError || views.isError || unread.isError ? "error" : "loading"}
      title={item ? viewItemLabel(item, strings) : ""}
      subtitle={item ? `${viewDetail(item, userId, strings)} · ${strings.views.count(total)}` : ""}
      filters={filterChips(item, status, projects.data ?? [], strings)}
      groups={groups}
      starred={starred}
      refreshing={list.isRefetching}
      onRefresh={() => void list.refetch()}
      onToggleStar={() => (starred ? sidebar.unstar(itemId) : sidebar.star(itemId))}
      onBack={() => navigation.goBack()}
      onRetry={() => {
        void views.refetch();
        void list.refetch();
        unread.refetch();
      }}
      onOpen={(issue) => navigation.navigate("Issue", { issueId: issue.id })}
    />
  );
}
