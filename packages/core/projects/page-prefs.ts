"use client";

import { useCallback, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { parseWithFallback } from "../api/schema";
import {
  issueViewPreferenceOptions,
  useUpdateIssueViewPreference,
  type IssueViewPreferenceDoc,
} from "../issue-views/preferences";
import {
  PROJECT_COLUMN_KEYS,
  PROJECT_GROUP_BY_VALUES,
  PROJECT_SORT_DEFAULT_DIRECTION,
  useProjectViewStore,
  type ProjectColumnKey,
  type ProjectGroupBy,
  type ProjectLayout,
  type ProjectListFilters,
  type ProjectSortDirection,
  type ProjectSortField,
  type ProjectViewState,
} from "./stores/view-store";

export const PROJECTS_PAGE_SCOPE = { scope_type: "projects_page" } as const;

/** The All Projects page's display settings, kept per user so every device opens it the same way. */
export interface ProjectsPagePrefs {
  layout: ProjectLayout;
  groupBy: ProjectGroupBy;
  sortField: ProjectSortField;
  sortDirection: ProjectSortDirection;
  hiddenColumns: ProjectColumnKey[];
  filters: ProjectListFilters;
}

const oneOf = <T extends string>(values: readonly T[]) =>
  z.string().refine((v): v is T => (values as readonly string[]).includes(v)).optional().catch(undefined);

const strings = z.array(z.unknown()).catch([]).transform((v) => v.filter((s): s is string => typeof s === "string"));

// Another client version may have saved values this one does not know; those
// fields come out undefined and this device's own value stays.
const ProjectsPagePrefsDocSchema = z.object({
  layout: oneOf<ProjectLayout>(["table", "board"]),
  groupBy: oneOf(PROJECT_GROUP_BY_VALUES),
  sortField: oneOf(Object.keys(PROJECT_SORT_DEFAULT_DIRECTION) as ProjectSortField[]),
  sortDirection: oneOf<ProjectSortDirection>(["asc", "desc"]),
  hiddenColumns: z
    .array(z.unknown())
    .optional()
    .catch(undefined)
    .transform((v) => v?.filter((c): c is ProjectColumnKey => PROJECT_COLUMN_KEYS.includes(c as ProjectColumnKey))),
  filters: z
    .object({ statuses: strings, priorities: strings, leads: strings })
    .optional()
    .catch(undefined),
});

type ProjectsPagePrefsPatch = Partial<ProjectsPagePrefs>;

export function projectsPagePrefsFromDoc(doc: Record<string, unknown>): ProjectsPagePrefsPatch {
  const parsed = parseWithFallback<Record<string, unknown>>(doc, ProjectsPagePrefsDocSchema, {}, {
    endpoint: "issue-view-preferences:projects_page",
  });
  return Object.fromEntries(
    Object.entries(parsed).filter(([, value]) => value !== undefined),
  ) as ProjectsPagePrefsPatch;
}

export function projectsPagePrefsOf(state: ProjectViewState): ProjectsPagePrefs {
  return {
    layout: state.layout,
    groupBy: state.groupBy,
    sortField: state.sortField,
    sortDirection: state.sortDirection,
    hiddenColumns: state.hiddenColumns,
    filters: state.filters,
  };
}

/**
 * Keeps the page's view store in step with the user's saved settings: a saved
 * copy, on load or when another device changes it, replaces this device's;
 * until one exists this device's own settings stand in. Returns `save`, which
 * the page calls after each change the user makes. Search is not saved.
 */
export function useProjectsPagePrefsSync(wsId: string): () => void {
  const { data, isSuccess } = useQuery(issueViewPreferenceOptions(wsId, PROJECTS_PAGE_SCOPE));
  const write = useUpdateIssueViewPreference(wsId, PROJECTS_PAGE_SCOPE);
  const saved = data?.updated_at ? data.prefs : null;

  useEffect(() => {
    if (saved) useProjectViewStore.setState(projectsPagePrefsFromDoc(saved));
  }, [saved]);

  // A write replaces the whole document, so it waits for the saved one to load
  // rather than overwrite settings this device has not seen yet.
  return useCallback(() => {
    if (!isSuccess) return;
    write(projectsPagePrefsOf(useProjectViewStore.getState()) as unknown as IssueViewPreferenceDoc);
  }, [isSuccess, write]);
}
