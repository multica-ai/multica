"use client";

import { create } from "zustand";
import {
  DEFAULT_GRANTS_FILTER,
  type GrantClassification,
  type GrantStatus,
  type GrantSubjectType,
  type GrantsFilter,
} from "./types";

// Ephemeral UI state for the permissions page. Server state lives in
// TanStack Query keyed on this filter — never copy a grant row into here.
interface PermissionsPageState extends GrantsFilter {
  setSubjectType: (v: GrantSubjectType | null) => void;
  setSubjectId: (v: string | null) => void;
  setResourceType: (v: string | null) => void;
  setStatus: (v: GrantStatus | null) => void;
  setClassification: (v: GrantClassification | null) => void;
  setSearch: (v: string) => void;
  nextPage: () => void;
  prevPage: () => void;
  goToPage: (offset: number) => void;
  reset: () => void;
}

export const usePermissionsStore = create<PermissionsPageState>((set) => ({
  ...DEFAULT_GRANTS_FILTER,
  setSubjectType: (subjectType) => set({ subjectType, offset: 0 }),
  setSubjectId: (subjectId) => set({ subjectId, offset: 0 }),
  setResourceType: (resourceType) => set({ resourceType, offset: 0 }),
  setStatus: (status) => set({ status, offset: 0 }),
  setClassification: (classification) => set({ classification, offset: 0 }),
  setSearch: (search) => set({ search, offset: 0 }),
  nextPage: () => set((s) => ({ offset: s.offset + s.limit })),
  prevPage: () => set((s) => ({ offset: Math.max(0, s.offset - s.limit) })),
  goToPage: (offset) => set({ offset: Math.max(0, offset) }),
  reset: () => set(DEFAULT_GRANTS_FILTER),
}));
