"use client";

import { useMemo } from "react";
import { create } from "zustand";
import type { Issue, ListIssuesResponse } from "@/shared/types";
import { toast } from "sonner";
import { getAppQueryClient, queryKeys } from "@/shared/query";
import { useWorkspaceStore } from "@/features/workspace";
import { issuesListQueryOptions, useIssuesListQuery } from "./queries";

interface IssueSessionState {
  activeIssueId: string | null;
  setActiveIssue: (id: string | null) => void;
}

interface IssueState extends IssueSessionState {
  issues: Issue[];
  loading: boolean;
  fetch: () => Promise<void>;
  setIssues: (issues: Issue[]) => void;
  addIssue: (issue: Issue) => void;
  updateIssue: (id: string, updates: Partial<Issue>) => void;
  removeIssue: (id: string) => void;
}

type IssueStoreSelector<T> = (state: IssueState) => T;

interface IssueStoreHook {
  <T>(selector: IssueStoreSelector<T>): T;
  getState: () => IssueState;
  subscribe: (listener: (state: IssueState) => void) => () => void;
}

const useIssueSessionStore = create<IssueSessionState>((set) => ({
  activeIssueId: null,
  setActiveIssue: (id) => set({ activeIssueId: id }),
}));

function getIssueSnapshot(): IssueState {
  const workspace = useWorkspaceStore.getState().workspace;
  const data = workspace
    ? getAppQueryClient().getQueryData<ListIssuesResponse>(
        queryKeys.issues.list(workspace.id, { limit: 200 }),
      )
    : null;

  return {
    ...useIssueSessionStore.getState(),
    issues: data?.issues ?? [],
    loading: false,
    fetch: async () => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      try {
        await getAppQueryClient().fetchQuery(issuesListQueryOptions(nextWorkspace.id));
      } catch (error) {
        toast.error(error instanceof Error ? error.message : "Failed to load issues");
      }
    },
    setIssues: (issues) => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      getAppQueryClient().setQueryData<ListIssuesResponse>(
        queryKeys.issues.list(nextWorkspace.id, { limit: 200 }),
        { issues, total: issues.length },
      );
    },
    addIssue: (issue) => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      getAppQueryClient().setQueryData<ListIssuesResponse>(
        queryKeys.issues.list(nextWorkspace.id, { limit: 200 }),
        (existing) => {
          if (!existing) return { issues: [issue], total: 1 };
          if (existing.issues.some((item) => item.id === issue.id)) return existing;
          return { ...existing, issues: [...existing.issues, issue], total: existing.total + 1 };
        },
      );
    },
    updateIssue: (id, updates) => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      getAppQueryClient().setQueryData<ListIssuesResponse>(
        queryKeys.issues.list(nextWorkspace.id, { limit: 200 }),
        (existing) => {
          if (!existing) return existing;
          return {
            ...existing,
            issues: existing.issues.map((issue) => (issue.id === id ? { ...issue, ...updates } : issue)),
          };
        },
      );
      getAppQueryClient().setQueryData<Issue | null>(queryKeys.issues.detail(id), (existing) =>
        existing ? { ...existing, ...updates } : existing,
      );
    },
    removeIssue: (id) => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      getAppQueryClient().setQueryData<ListIssuesResponse>(
        queryKeys.issues.list(nextWorkspace.id, { limit: 200 }),
        (existing) => {
          if (!existing) return existing;
          return {
            ...existing,
            issues: existing.issues.filter((issue) => issue.id !== id),
            total: Math.max(0, existing.total - 1),
          };
        },
      );
      getAppQueryClient().removeQueries({ queryKey: queryKeys.issues.detail(id) });
    },
  };
}

export const useIssueStore = ((selector: IssueStoreSelector<unknown>) => {
  const sessionState = useIssueSessionStore();
  const issuesQuery = useIssuesListQuery();

  const snapshot = useMemo<IssueState>(
    () => ({
      ...sessionState,
      issues: issuesQuery.data?.issues ?? [],
      loading: issuesQuery.isPending,
      fetch: getIssueSnapshot().fetch,
      setIssues: getIssueSnapshot().setIssues,
      addIssue: getIssueSnapshot().addIssue,
      updateIssue: getIssueSnapshot().updateIssue,
      removeIssue: getIssueSnapshot().removeIssue,
    }),
    [issuesQuery.data?.issues, issuesQuery.isPending, sessionState],
  );

  return selector(snapshot);
}) as IssueStoreHook;

useIssueStore.getState = getIssueSnapshot;
useIssueStore.subscribe = (listener) =>
  useIssueSessionStore.subscribe(() => {
    listener(getIssueSnapshot());
  });
