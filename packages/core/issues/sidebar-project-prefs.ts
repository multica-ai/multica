"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useShallow } from "zustand/react/shallow";
import {
  issueViewPreferenceOptions,
  useUpdateIssueViewPreference,
  type IssueViewPreferenceDoc,
} from "../issue-views/preferences";
import {
  DEFAULT_SIDEBAR_ISSUE_SETTINGS,
  DEFAULT_SIDEBAR_PROJECT_SETTINGS,
  useSidebarTreeStore,
  type SidebarIssueSettings,
  type SidebarProjectSettings,
} from "./stores/sidebar-tree-store";

export const SIDEBAR_PROJECTS_SCOPE = { scope_type: "sidebar_projects" } as const;

/** The sidebar Projects section's settings, kept per user so every device shows the same list. */
export interface SidebarProjectPrefs {
  projectSettings: SidebarProjectSettings;
  /** How expanded rows, projects and built-in views alike, list their issues. */
  issueSettings: SidebarIssueSettings;
  /** Manual project order; projects missing from it follow in list order. */
  projectOrder: string[];
}

export interface SidebarProjectPrefsPatch {
  projectSettings?: Partial<SidebarProjectSettings>;
  issueSettings?: Partial<SidebarIssueSettings>;
  projectOrder?: string[];
}

const asRecord = (value: unknown): Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value) ? (value as Record<string, unknown>) : {};

/** Settings missing from a stored document take their defaults, so older documents keep working. */
export function sidebarProjectPrefsFromDoc(doc: Record<string, unknown>): SidebarProjectPrefs {
  return {
    projectSettings: { ...DEFAULT_SIDEBAR_PROJECT_SETTINGS, ...(asRecord(doc.projects) as Partial<SidebarProjectSettings>) },
    issueSettings: { ...DEFAULT_SIDEBAR_ISSUE_SETTINGS, ...(asRecord(doc.issues) as Partial<SidebarIssueSettings>) },
    projectOrder: Array.isArray(doc.order) ? doc.order.filter((id): id is string => typeof id === "string") : [],
  };
}

export function sidebarProjectPrefsToDoc(prefs: SidebarProjectPrefs): IssueViewPreferenceDoc {
  return { projects: prefs.projectSettings, issues: prefs.issueSettings, order: prefs.projectOrder };
}

/**
 * The user's Projects section settings. Until their server copy is first
 * saved, the settings this device kept locally stand in for it.
 */
export function useSidebarProjectPrefs(wsId: string): SidebarProjectPrefs & { loaded: boolean } {
  const { data, isSuccess } = useQuery(issueViewPreferenceOptions(wsId, SIDEBAR_PROJECTS_SCOPE));
  const local = useSidebarTreeStore(
    useShallow((s) => ({ projectSettings: s.projectSettings, issueSettings: s.issueSettings, projectOrder: s.projectOrder })),
  );
  const saved = data?.updated_at ? data.prefs : null;
  const prefs = useMemo(() => (saved ? sidebarProjectPrefsFromDoc(saved) : local), [saved, local]);
  return { ...prefs, loaded: isSuccess };
}

/**
 * Saves a change for every device. A write replaces the whole document, so
 * it waits until the saved one has loaded rather than replace it with defaults.
 */
export function useUpdateSidebarProjectPrefs(wsId: string) {
  const { projectSettings, issueSettings, projectOrder, loaded } = useSidebarProjectPrefs(wsId);
  const { mutate } = useUpdateIssueViewPreference(wsId, SIDEBAR_PROJECTS_SCOPE);
  return useCallback(
    (patch: SidebarProjectPrefsPatch) => {
      if (!loaded) return;
      mutate(
        sidebarProjectPrefsToDoc({
          projectSettings: { ...projectSettings, ...patch.projectSettings },
          issueSettings: { ...issueSettings, ...patch.issueSettings },
          projectOrder: patch.projectOrder ?? projectOrder,
        }),
      );
    },
    [loaded, mutate, projectSettings, issueSettings, projectOrder],
  );
}
