"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useShallow } from "zustand/react/shallow";
import { parseWithFallback } from "../api/schema";
import { SidebarProjectPrefsDocSchema, type SidebarProjectPrefsDoc } from "../api/schemas";
import { PROJECT_STATUS_ORDER } from "../projects/config";
import type { ProjectStatus } from "../types";
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

const EMPTY_DOC: SidebarProjectPrefsDoc = { projects: {}, issues: {}, order: [] };

const isProjectStatus = (status: string): status is ProjectStatus =>
  (PROJECT_STATUS_ORDER as readonly string[]).includes(status);

function withDefaults<T extends object>(defaults: T, saved: { [K in keyof T]?: T[K] }): T {
  const merged = { ...defaults };
  for (const key of Object.keys(saved) as (keyof T)[]) {
    const value = saved[key];
    if (value !== undefined) merged[key] = value;
  }
  return merged;
}

/**
 * Settings a stored document lacks, or holds in a shape this client does not
 * know, take their defaults, so older and newer documents both keep working.
 */
export function sidebarProjectPrefsFromDoc(doc: Record<string, unknown>): SidebarProjectPrefs {
  const { projects, issues, order } = parseWithFallback(doc, SidebarProjectPrefsDocSchema, EMPTY_DOC, {
    endpoint: "issue-view-preferences:sidebar_projects",
  });
  return {
    projectSettings: withDefaults(DEFAULT_SIDEBAR_PROJECT_SETTINGS, {
      ...projects,
      statuses: projects.statuses?.filter(isProjectStatus),
    }),
    issueSettings: withDefaults(DEFAULT_SIDEBAR_ISSUE_SETTINGS, issues),
    projectOrder: order,
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
  const save = useUpdateIssueViewPreference(wsId, SIDEBAR_PROJECTS_SCOPE);
  return useCallback(
    (patch: SidebarProjectPrefsPatch) => {
      if (!loaded) return;
      save(
        sidebarProjectPrefsToDoc({
          projectSettings: { ...projectSettings, ...patch.projectSettings },
          issueSettings: { ...issueSettings, ...patch.issueSettings },
          projectOrder: patch.projectOrder ?? projectOrder,
        }),
      );
    },
    [loaded, save, projectSettings, issueSettings, projectOrder],
  );
}
