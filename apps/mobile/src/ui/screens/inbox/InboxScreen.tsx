import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useMarkAllInboxRead, useMarkInboxRead } from "@multica/core/inbox/mutations";
import { deduplicateInboxItems, inboxListOptions } from "@multica/core/inbox/queries";
import { useActorResolver } from "../../model/people";
import { useStatusPresentation } from "../../model/status";
import { useWorkspace } from "../../model/workspace-context";
import { useDetail } from "../../shell/detail";
import { useStrings } from "../../strings";
import { inboxRows } from "./inbox-rows";
import { InboxView } from "./InboxView";

export function InboxScreen() {
  const detail = useDetail();
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const resolveActor = useActorResolver();
  const status = useStatusPresentation(workspace.id);
  const list = useQuery({ ...inboxListOptions(workspace.id), select: deduplicateInboxItems });
  const markRead = useMarkInboxRead();
  const markAllRead = useMarkAllInboxRead();

  const rows = useMemo(
    () => inboxRows(list.data ?? [], resolveActor, (key) => status(key).label, strings),
    [list.data, resolveActor, status, strings],
  );
  const hasUnread = rows.some((row) => row.unread);

  return (
    <InboxView
      state={list.data ? "ready" : list.isError ? "error" : "loading"}
      rows={rows}
      refreshing={list.isRefetching}
      onRefresh={() => void list.refetch()}
      onRetry={() => void list.refetch()}
      selectedIssueId={detail.selected?.kind === "issue" ? detail.selected.issueId : null}
      onMarkAllRead={hasUnread ? () => markAllRead.mutate() : undefined}
      onOpen={(row) => {
        if (row.unread) markRead.mutate(row.id);
        if (row.issueId) detail.open({ kind: "issue", issueId: row.issueId });
      }}
    />
  );
}
