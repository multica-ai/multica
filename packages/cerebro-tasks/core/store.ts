"use client";

import { create } from "zustand";
import {
  DEFAULT_TASKS_FILTER,
  DEFAULT_VISIBLE_COLUMNS,
  type ColumnId,
  type TaskStatus,
  type TaskTimeRange,
  type TaskType,
  type TasksFilter,
} from "./types";

// Filter + pagination state for the cerebro tasks page. Matches the
// dashboard pattern: ephemeral UI state (which agent/status/range/page is
// currently selected) lives in Zustand, server state lives in TanStack
// Query keyed on this filter.
interface TasksPageState extends TasksFilter {
  visibleColumns: Record<ColumnId, boolean>;
  setAgentId: (id: string | null) => void;
  setIssueId: (id: string | null) => void;
  setProjectId: (id: string | null) => void;
  setStatus: (status: TaskStatus | null) => void;
  setType: (type: TaskType | null) => void;
  setRange: (range: TaskTimeRange) => void;
  setCustomFrom: (from: string | null) => void;
  setCustomTo: (to: string | null) => void;
  setSearch: (search: string) => void;
  setLimit: (limit: number) => void;
  setColumnVisible: (column: ColumnId, visible: boolean) => void;
  nextPage: () => void;
  prevPage: () => void;
  goToPage: (offset: number) => void;
  reset: () => void;
}

export const useCerebroTasksStore = create<TasksPageState>((set) => ({
  ...DEFAULT_TASKS_FILTER,
  visibleColumns: { ...DEFAULT_VISIBLE_COLUMNS },
  setAgentId: (agentId) => set({ agentId, offset: 0 }),
  setIssueId: (issueId) => set({ issueId, offset: 0 }),
  setProjectId: (projectId) => set({ projectId, offset: 0 }),
  setStatus: (status) => set({ status, offset: 0 }),
  setType: (type) => set({ type, offset: 0 }),
  setRange: (range) => set({ range, offset: 0 }),
  setCustomFrom: (customFrom) => set({ customFrom, offset: 0 }),
  setCustomTo: (customTo) => set({ customTo, offset: 0 }),
  setSearch: (search) => set({ search, offset: 0 }),
  setLimit: (limit) => set({ limit, offset: 0 }),
  setColumnVisible: (column, visible) =>
    set((s) => ({ visibleColumns: { ...s.visibleColumns, [column]: visible } })),
  nextPage: () => set((s) => ({ offset: s.offset + s.limit })),
  prevPage: () => set((s) => ({ offset: Math.max(0, s.offset - s.limit) })),
  goToPage: (offset) => set({ offset: Math.max(0, offset) }),
  reset: () => set({ ...DEFAULT_TASKS_FILTER, visibleColumns: { ...DEFAULT_VISIBLE_COLUMNS } }),
}));
