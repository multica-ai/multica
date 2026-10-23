/**
 * Issue filters for a project's issue list on the project page: status,
 * priority and "show completed", one set per project. Web keeps a view per
 * `project:<id>` surface (packages/core/issues/stores/surface-view-store.ts).
 *
 * Each project's filters are stored under their own key in the shared
 * view-prefs storage and load when the project opens, so no single snapshot
 * grows with the number of projects. Scope changes clear the loaded set; see
 * `./view-prefs-storage.ts`.
 */
import { create } from "zustand";
import type { IssuePriority, IssueStatus } from "@multica/core/types";
import { mergeIssueFilterPrefs, type IssueFilterPrefs } from "./issue-view-prefs";
import { onViewPrefsScopeChange, viewPrefsStorage } from "./view-prefs-storage";

const STORAGE_NAME = "multica_project_issues_view";

export const DEFAULT_PROJECT_ISSUES_VIEW: Readonly<IssueFilterPrefs> = {
  statusFilters: [],
  priorityFilters: [],
  showCompleted: false,
};

interface ProjectIssuesViewState {
  byProject: Record<string, IssueFilterPrefs>;
  /** Bumped on every scope change. Screens list it as an effect dependency
   *  so they reload under the new scope whatever order effects run in, and
   *  a load started under the previous scope cannot land in the new one. */
  generation: number;
  toggleStatusFilter: (projectId: string, status: IssueStatus) => void;
  togglePriorityFilter: (projectId: string, priority: IssuePriority) => void;
  setShowCompleted: (projectId: string, showCompleted: boolean) => void;
  clearFilters: (projectId: string) => void;
}

function toggle<T>(list: T[], value: T): T[] {
  return list.includes(value)
    ? list.filter((item) => item !== value)
    : [...list, value];
}

export const useProjectIssuesViewStore = create<ProjectIssuesViewState>()(
  (set, get) => {
    const update = (
      projectId: string,
      change: (prefs: IssueFilterPrefs) => Partial<IssueFilterPrefs>,
    ) => {
      const current = get().byProject[projectId] ?? DEFAULT_PROJECT_ISSUES_VIEW;
      const next = { ...current, ...change(current) };
      set({ byProject: { ...get().byProject, [projectId]: next } });
      void viewPrefsStorage.setItem(
        `${STORAGE_NAME}.${projectId}`,
        JSON.stringify(next),
      );
    };
    return {
      byProject: {},
      generation: 0,
      toggleStatusFilter: (projectId, status) =>
        update(projectId, (p) => ({ statusFilters: toggle(p.statusFilters, status) })),
      togglePriorityFilter: (projectId, priority) =>
        update(projectId, (p) => ({
          priorityFilters: toggle(p.priorityFilters, priority),
        })),
      setShowCompleted: (projectId, showCompleted) =>
        update(projectId, () => ({ showCompleted })),
      clearFilters: (projectId) =>
        update(projectId, () => ({ statusFilters: [], priorityFilters: [] })),
    };
  },
);

onViewPrefsScopeChange(() => {
  useProjectIssuesViewStore.setState((state) => ({
    byProject: {},
    generation: state.generation + 1,
  }));
});

/** Loads a project's saved filters once per scope; call when the project opens. */
export async function loadProjectIssuesView(projectId: string) {
  if (projectId in useProjectIssuesViewStore.getState().byProject) return;
  const startedIn = useProjectIssuesViewStore.getState().generation;
  let raw: string | null = null;
  try {
    raw = await viewPrefsStorage.getItem(`${STORAGE_NAME}.${projectId}`);
  } catch {
    // Unreadable storage counts as nothing saved.
  }
  const { byProject, generation } = useProjectIssuesViewStore.getState();
  // A change made while loading is newer than the saved value.
  if (startedIn !== generation || projectId in byProject || !raw) return;
  let saved: unknown = null;
  try {
    saved = JSON.parse(raw);
  } catch {
    return;
  }
  useProjectIssuesViewStore.setState({
    byProject: {
      ...byProject,
      [projectId]: mergeIssueFilterPrefs(saved, { ...DEFAULT_PROJECT_ISSUES_VIEW }),
    },
  });
}
