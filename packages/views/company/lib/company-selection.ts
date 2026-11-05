/**
 * The company hub's drill-down state, kept in the URL so a level can be linked
 * to and survives a reload. Every level stays on the same page: a department
 * and a squad are query params, not routes.
 */

/** The department key for squads that sit on no floor. */
export const UNSEATED_DEPARTMENT = "unseated";

/** The floor plan is the default view; the org chart is opened with `view=overview`. */
export type CompanyView = "plan" | "overview";

export interface CompanySelection {
  view: CompanyView;
  department: string | null;
  squad: string | null;
  agent: string | null;
}

/** Reads the selection from the page's query string; unknown values fall back to the top level. */
export function readCompanySelection(params: { get(name: string): string | null }): CompanySelection {
  return {
    view: params.get("view") === "overview" ? "overview" : "plan",
    department: params.get("dept") || null,
    squad: params.get("squad") || null,
    agent: params.get("agent") || null,
  };
}

/** The query string for a selection. The default view and empty levels are left out. */
export function companySelectionQuery(selection: CompanySelection): string {
  const params = new URLSearchParams();
  if (selection.view === "overview") params.set("view", "overview");
  if (selection.department) params.set("dept", selection.department);
  if (selection.squad) params.set("squad", selection.squad);
  if (selection.agent) params.set("agent", selection.agent);
  const query = params.toString();
  return query ? `?${query}` : "";
}

/** How many levels are open: 0 the departments, 1 a department's squads, 2 a squad, 3 an agent. */
export function companyDepth(selection: CompanySelection): 0 | 1 | 2 | 3 {
  if (selection.agent && selection.squad) return 3;
  if (selection.squad) return 2;
  if (selection.department) return 1;
  return 0;
}
