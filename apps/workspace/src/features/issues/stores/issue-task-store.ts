"use client";

import { create } from "zustand";
import type { AgentTask } from "@/shared/types";
import { getAppQueryClient } from "@/shared/query";
import { issueActiveTaskQueryOptions } from "../queries";

interface IssueTaskSnapshot {
  task: AgentTask | null;
  loaded: boolean;
  summary: string | null;
  step: number | null;
  total: number | null;
}

interface IssueTaskState {
  byIssueId: Record<string, IssueTaskSnapshot>;
  taskIssueIndex: Record<string, string>;
  observedIssueRefs: Record<string, number>;
  registerIssue: (issueId: string) => void;
  unregisterIssue: (issueId: string) => void;
  refreshIssue: (issueId: string, options?: { force?: boolean }) => Promise<void>;
  refreshObservedIssues: () => Promise<void>;
  linkTaskToIssue: (taskId: string, issueId: string) => void;
  setProgress: (taskId: string, summary: string, step?: number, total?: number) => void;
  clearIssueTask: (issueId: string) => void;
}

const inflightRequests = new Map<string, Promise<void>>();

function emptySnapshot(): IssueTaskSnapshot {
  return {
    task: null,
    loaded: false,
    summary: null,
    step: null,
    total: null,
  };
}

export const useIssueTaskStore = create<IssueTaskState>((set, get) => ({
  byIssueId: {},
  taskIssueIndex: {},
  observedIssueRefs: {},

  registerIssue: (issueId) => {
    set((state) => ({
      observedIssueRefs: {
        ...state.observedIssueRefs,
        [issueId]: (state.observedIssueRefs[issueId] ?? 0) + 1,
      },
    }));
  },

  unregisterIssue: (issueId) => {
    set((state) => {
      const current = state.observedIssueRefs[issueId] ?? 0;
      if (current <= 1) {
        const nextRefs = { ...state.observedIssueRefs };
        delete nextRefs[issueId];
        return { observedIssueRefs: nextRefs };
      }
      return {
        observedIssueRefs: {
          ...state.observedIssueRefs,
          [issueId]: current - 1,
        },
      };
    });
  },

  refreshIssue: async (issueId, options) => {
    const force = options?.force ?? false;
    if (!force) {
      const existing = inflightRequests.get(issueId);
      if (existing) {
        return existing;
      }
    }

    const queryClient = getAppQueryClient();
    const queryOptions = issueActiveTaskQueryOptions(issueId);

    const request = (async () => {
      if (force) {
        await queryClient.invalidateQueries({
          queryKey: queryOptions.queryKey,
          exact: true,
        });
      }

      return queryClient.fetchQuery(queryOptions);
    })()
      .then((task) => {
        set((state) => {
          const prev = state.byIssueId[issueId] ?? emptySnapshot();
          const nextIndex = { ...state.taskIssueIndex };

          if (prev.task?.id) {
            delete nextIndex[prev.task.id];
          }

          if (task?.id) {
            nextIndex[task.id] = issueId;
          }

          const isSameTask = prev.task?.id && prev.task.id === task?.id;

          return {
            byIssueId: {
              ...state.byIssueId,
              [issueId]: {
                task,
                loaded: true,
                summary: isSameTask ? prev.summary : null,
                step: isSameTask ? prev.step : null,
                total: isSameTask ? prev.total : null,
              },
            },
            taskIssueIndex: nextIndex,
          };
        });
      })
      .catch(() => {
        set((state) => ({
          byIssueId: {
            ...state.byIssueId,
            [issueId]: {
              ...(state.byIssueId[issueId] ?? emptySnapshot()),
              loaded: true,
            },
          },
        }));
      })
      .finally(() => {
        if (inflightRequests.get(issueId) === request) {
          inflightRequests.delete(issueId);
        }
      });

    inflightRequests.set(issueId, request);
    return request;
  },

  refreshObservedIssues: async () => {
    const issueIds = Object.keys(get().observedIssueRefs);
    await Promise.all(
      issueIds.map((issueId) => get().refreshIssue(issueId, { force: true })),
    );
  },

  linkTaskToIssue: (taskId, issueId) => {
    set((state) => ({
      taskIssueIndex: {
        ...state.taskIssueIndex,
        [taskId]: issueId,
      },
    }));
  },

  setProgress: (taskId, summary, step, total) => {
    const issueId = get().taskIssueIndex[taskId];
    if (!issueId) return;

    set((state) => {
      const current = state.byIssueId[issueId];
      if (!current?.task) return state;

      return {
        byIssueId: {
          ...state.byIssueId,
          [issueId]: {
            ...current,
            task: {
              ...current.task,
              status:
                current.task.status === "completed" ||
                current.task.status === "failed" ||
                current.task.status === "cancelled"
                  ? current.task.status
                  : "running",
            },
            summary,
            step: step ?? null,
            total: total ?? null,
          },
        },
      };
    });
  },

  clearIssueTask: (issueId) => {
    set((state) => {
      const current = state.byIssueId[issueId];
      const nextIndex = { ...state.taskIssueIndex };
      if (current?.task?.id) {
        delete nextIndex[current.task.id];
      }

      return {
        byIssueId: {
          ...state.byIssueId,
          [issueId]: {
            task: null,
            loaded: true,
            summary: null,
            step: null,
            total: null,
          },
        },
        taskIssueIndex: nextIndex,
      };
    });
  },
}));