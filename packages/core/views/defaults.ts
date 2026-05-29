import type { CreateViewRequest, ViewFilters, ViewPage } from "../types";

/**
 * Default views seeded on first visit to a page that has none. Each entry is
 * the filter contract for one baseline tab; the page's old hardcoded scope
 * buttons (All / Members / Agents, etc.) become these saved views.
 *
 * `name` keys are translation keys resolved at seed time — the seeded view's
 * `name` is a display string, so the caller passes a translator. The first
 * entry of each page is the "default" tab (selected when no ?view= is present).
 *
 * Tokens ({me} / {my_agents} / {my_squads}) are server-expanded — the frontend
 * never substitutes the user's ids here, which is what lets a seeded view be
 * shared across the workspace.
 *
 * NOTE: the backend's POST /api/views has no `is_default` field, so these are
 * seeded as ordinary shared views (deletable). `shared: true` is required so
 * the baseline tabs are visible to every member, not just the seeder.
 */
export interface DefaultViewSpec {
  /** i18n key under the page's `views.defaults` namespace. */
  nameKey: string;
  filters: ViewFilters;
}

export const DEFAULT_VIEWS: Record<Exclude<ViewPage, "project">, DefaultViewSpec[]> = {
  issues: [
    { nameKey: "all", filters: {} },
    { nameKey: "members", filters: { assignee_types: ["member"] } },
    { nameKey: "agents", filters: { assignee_types: ["agent", "squad"] } },
  ],
  my_issues: [
    {
      nameKey: "all",
      filters: {
        any_of: [
          { assignee_filters: ["member:{me}"] },
          { creator_filters: ["member:{me}"] },
          { assignee_filters: ["agent:{my_agents}", "squad:{my_squads}"] },
        ],
      },
    },
    { nameKey: "assigned", filters: { assignee_filters: ["member:{me}"] } },
    { nameKey: "created", filters: { creator_filters: ["member:{me}"] } },
    {
      nameKey: "agents",
      filters: { assignee_filters: ["agent:{my_agents}", "squad:{my_squads}"] },
    },
  ],
};

/**
 * Build the CreateViewRequest payloads for a page's default views. `resolveName`
 * turns each spec's `nameKey` into a display name (i18n). `position` is the
 * array index so the seeded order matches DEFAULT_VIEWS.
 */
export function buildDefaultViewRequests(
  page: Exclude<ViewPage, "project">,
  projectId: string | null,
  resolveName: (nameKey: string) => string,
): CreateViewRequest[] {
  return DEFAULT_VIEWS[page].map((spec, i) => ({
    name: resolveName(spec.nameKey),
    page,
    project_id: projectId,
    filters: spec.filters,
    position: i,
    shared: true,
  }));
}
