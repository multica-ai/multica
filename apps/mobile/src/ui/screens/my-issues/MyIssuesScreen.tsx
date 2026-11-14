import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { issueKeys } from "@multica/core/issues/queries";
import { subscribedIssueIdsOptions } from "@multica/core/issues/subscribed";
import type { ListIssuesParams } from "@multica/core/types";
import { issueRowModel, sortByPhase } from "../../model/issues";
import { useActorResolver } from "../../model/people";
import { useStatusPresentation } from "../../model/status";
import { useWorkspace } from "../../model/workspace-context";
import { useDetail } from "../../shell/detail";
import { MyIssuesView, type MyIssuesRelation } from "./MyIssuesView";

const LIMIT = 100;

export function MyIssuesScreen() {
  const detail = useDetail();
  const { workspace } = useWorkspace();
  const userId = useAuthStore((state) => state.user?.id ?? "");
  const [relation, setRelation] = useState<MyIssuesRelation>("assigned");
  const resolveActor = useActorResolver();
  const status = useStatusPresentation(workspace.id);
  const catalog = useIssueStatuses(workspace.id);

  const subscribed = useQuery({ ...subscribedIssueIdsOptions(workspace.id), enabled: relation === "subscribed" });
  const filter: ListIssuesParams | null =
    relation === "assigned"
      ? { assignee_id: userId }
      : relation === "created"
        ? { created_by_user_id: userId }
        : subscribed.data
          ? { ids: subscribed.data }
          : null;

  // Nested under the workspace issue key, so core's reconnect and workspace refreshes reach it.
  const list = useQuery({
    queryKey: [...issueKeys.all(workspace.id), "mobile-my", relation, filter],
    queryFn: () => api.listIssues({ ...filter, limit: LIMIT, sort_by: "last_activity", sort_direction: "desc" }),
    enabled: filter !== null && (filter.ids === undefined || filter.ids.length > 0),
    staleTime: 30_000,
  });

  const issues = useMemo(() => {
    if (filter?.ids?.length === 0) return [];
    const sorted = sortByPhase(list.data?.issues ?? [], (issue) => catalog.categoryOf(issue.status));
    return sorted.map((issue) => issueRowModel(issue, status, resolveActor));
  }, [list.data, filter?.ids?.length, catalog, status, resolveActor]);

  const empty = filter?.ids?.length === 0;
  const failed = list.isError || subscribed.isError;
  return (
    <MyIssuesView
      relation={relation}
      onRelationChange={setRelation}
      state={empty || list.data ? "ready" : failed ? "error" : "loading"}
      issues={issues}
      refreshing={list.isRefetching}
      onRefresh={() => void list.refetch()}
      onRetry={() => {
        void subscribed.refetch();
        void list.refetch();
      }}
      selectedId={detail.selected?.kind === "issue" ? detail.selected.issueId : null}
      onOpen={(issue) => detail.open({ kind: "issue", issueId: issue.id })}
    />
  );
}
