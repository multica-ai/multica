import type { IssueView } from "../api/schemas";
import { baselineFromQuery, type IssueViewBaseline } from "./baseline";

/**
 * The built-in views that are All issues under a fixed filter. They are not
 * stored on the server: the workspace issues page opens them through the same
 * `?view=` id as a saved view, and these definitions stand in for the saved
 * ones.
 */
export type FilteredBuiltinViewKey = "my_work" | "working" | "subscribed";

const QUERIES: Record<FilteredBuiltinViewKey, Record<string, unknown>> = {
  // Assigned to the viewer or to an agent they own.
  my_work: { relationFilters: ["assigned", "my_agents"] },
  // An agent is running on it.
  working: { agentRunningFilter: true },
  subscribed: { relationFilters: ["subscribed"] },
};

function builtinView(key: FilteredBuiltinViewKey): IssueView {
  return {
    id: key,
    workspace_id: "",
    owner_id: "",
    name: "",
    scope_type: "workspace",
    scope_id: null,
    scope_variant: null,
    visibility: "private",
    definition_version: 1,
    query: QUERIES[key],
    display: {},
    revision: 1,
    created_at: "",
    updated_at: "",
  };
}

export const FILTERED_BUILTIN_VIEWS: Record<FilteredBuiltinViewKey, IssueView> = {
  my_work: builtinView("my_work"),
  working: builtinView("working"),
  subscribed: builtinView("subscribed"),
};

/**
 * Saved views can be edited, so their filters stay removable. These have no
 * editor: their condition is fixed, and clearing filters returns to it.
 */
const BASELINES: Record<FilteredBuiltinViewKey, IssueViewBaseline> = {
  my_work: baselineFromQuery(QUERIES.my_work),
  working: baselineFromQuery(QUERIES.working),
  subscribed: baselineFromQuery(QUERIES.subscribed),
};

export function isFilteredBuiltinViewKey(id: string | null | undefined): id is FilteredBuiltinViewKey {
  return id === "my_work" || id === "working" || id === "subscribed";
}

/** The built-in a workspace view id names, or `null` for a saved view. */
export function filteredBuiltinView(id: string | null | undefined): IssueView | null {
  return isFilteredBuiltinViewKey(id) ? FILTERED_BUILTIN_VIEWS[id] : null;
}

export function filteredBuiltinBaseline(view: Pick<IssueView, "id"> | null | undefined): IssueViewBaseline | null {
  return isFilteredBuiltinViewKey(view?.id) ? BASELINES[view.id] : null;
}
