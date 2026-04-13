import type { QueryClient } from "@tanstack/react-query";
import { issueKeys } from "./queries";
import type { Issue } from "../types";
import type { ListIssuesResponse } from "../types";

export function onIssueCreated(
  qc: QueryClient,
  wsId: string,
  issue: Issue,
) {
  qc.setQueryData<ListIssuesResponse>(issueKeys.list(wsId), (old) => {
    if (!old || old.issues.some((i) => i.id === issue.id)) return old;
    return {
      ...old,
      issues: [...old.issues, issue],
      total: old.total + 1,
      doneTotal: (old.doneTotal ?? 0) + (issue.status === "done" ? 1 : 0),
    };
  });
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  if (issue.parent_issue_id) {
    qc.invalidateQueries({ queryKey: issueKeys.children(wsId, issue.parent_issue_id) });
    qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
  }
}

export function onIssueUpdated(
  qc: QueryClient,
  wsId: string,
  issue: Partial<Issue> & { id: string },
) {
  // Look up the parent before mutating list state, so we can also keep the
  // parent's children cache in sync (powers the sub-issues list shown on
  // the parent issue page).
  const listData = qc.getQueryData<ListIssuesResponse>(issueKeys.list(wsId));
  const detailData = qc.getQueryData<Issue>(issueKeys.detail(wsId, issue.id));
  const parentId =
    issue.parent_issue_id ??
    detailData?.parent_issue_id ??
    listData?.issues.find((i) => i.id === issue.id)?.parent_issue_id ??
    null;

  qc.setQueryData<ListIssuesResponse>(issueKeys.list(wsId), (old) => {
    if (!old) return old;
    const prev = old.issues.find((i) => i.id === issue.id);
    const wasDone = prev?.status === "done";
    const isDone = issue.status === "done";
    // Only adjust doneTotal when status field is present and actually changed
    let doneDelta = 0;
    if (issue.status !== undefined) {
      if (!wasDone && isDone) doneDelta = 1;
      else if (wasDone && !isDone) doneDelta = -1;
    }
    return {
      ...old,
      issues: old.issues.map((i) =>
        i.id === issue.id ? { ...i, ...issue } : i,
      ),
      doneTotal: (old.doneTotal ?? 0) + doneDelta,
    };
  });
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issue.id), (old) =>
    old ? { ...old, ...issue } : old,
  );
  if (parentId) {
    qc.setQueryData<Issue[]>(issueKeys.children(wsId, parentId), (old) =>
      old?.map((c) => (c.id === issue.id ? { ...c, ...issue } : c)),
    );
    if (issue.status !== undefined || issue.parent_issue_id !== undefined) {
      qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
    }
  }
}

export function onIssueDeleted(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  // Look up the issue before removing it to check for parent_issue_id
  const listData = qc.getQueryData<ListIssuesResponse>(issueKeys.list(wsId));
  const deleted = listData?.issues.find((i) => i.id === issueId);

  qc.setQueryData<ListIssuesResponse>(issueKeys.list(wsId), (old) => {
    if (!old) return old;
    const del = old.issues.find((i) => i.id === issueId);
    return {
      ...old,
      issues: old.issues.filter((i) => i.id !== issueId),
      total: old.total - 1,
      doneTotal: (old.doneTotal ?? 0) - (del?.status === "done" ? 1 : 0),
    };
  });
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  qc.removeQueries({ queryKey: issueKeys.detail(wsId, issueId) });
  qc.removeQueries({ queryKey: issueKeys.timeline(issueId) });
  qc.removeQueries({ queryKey: issueKeys.reactions(issueId) });
  qc.removeQueries({ queryKey: issueKeys.subscribers(issueId) });
  qc.removeQueries({ queryKey: issueKeys.children(wsId, issueId) });
  if (deleted?.parent_issue_id) {
    qc.invalidateQueries({ queryKey: issueKeys.children(wsId, deleted.parent_issue_id) });
    qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
  }
}
