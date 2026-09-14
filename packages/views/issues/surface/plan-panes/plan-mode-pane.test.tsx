// @vitest-environment jsdom

/**
 * Component-level coverage for the four Plan states. The controller's
 * fallback is tested in isolation; this file mounts
 * the actual rendered component so a regression that collapses two of the
 * four states — e.g. "no plan" rendering while the request is still in
 * flight, or a failed request silently reading as "no plan" — fails a test
 * instead of shipping.
 */

import type { ReactElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type { ProjectPlanOverview, ProjectPlanPart, ProjectPlanPhase } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../../navigation";
import { renderWithI18n } from "../../../test/i18n";
import { PlanModePane } from "./plan-mode-pane";
import { PlanDocumentPane } from "./plan-document-pane";
import { PlanAuthoringProvider } from "./authoring/plan-authoring-context";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

const navigation: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/projects/p-1",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => `https://app.example${path}`,
};

function makeOverview(overrides: Partial<ProjectPlanOverview> = {}): ProjectPlanOverview {
  return {
    plan: {
      id: "plan-1", workspace_id: "ws-1", project_id: "project-1", version: 1,
      kind: "prd", origin: "orchestrator", title: "Launch Plan", description: "",
      attributes: null, source_issue_id: null, superseded: false, superseded_at: null,
      created_by_type: "agent", created_by_id: "agent-1", created_at: "", updated_at: "",
    },
    rollup: { tasks_done: 1, tasks_total: 2, percent: 50, parts_covered: 1, parts_total: 1, parts_without_tasks: 0 },
    phases: [
      {
        id: "phase-1", title: "Phase 1 — Foundations", description: "", attributes: null, position: 0,
        rollup: { tasks_done: 1, tasks_total: 2, percent: 50 },
        parts: [
          {
            id: "part-1", title: "Schema", description: "", acceptance_criteria: "", attributes: null,
            position: 0, coverage_state: "in_progress",
            rollup: { tasks_done: 1, tasks_total: 2, percent: 50 },
            // Deliberately no "done"-status issue here: the coverage badge
            // for a `complete` part also renders the label "Done" (see
            // en/issues.json plan.coverage_state.complete), and the tests
            // below assert on that label — an issue status chip with the
            // same text would make `getByText("Done")` ambiguous.
            issues: [
              { id: "issue-2", number: 2, identifier: "LOCO-2", title: "Validate schema", status: "todo", status_category: "todo", assignee_type: null, assignee_id: null, deleted: false },
            ],
            created_at: "", updated_at: "",
          },
        ],
        created_at: "", updated_at: "",
      },
    ],
    dependencies: [],
    uncovered_parts: [],
    ...overrides,
  };
}

/** Wraps a component with the same providers `renderPane` uses, minus the API mock — for panes given `overview` directly rather than through `usePlanOverview`. */
function renderWithProviders(ui: ReactElement) {
  setApiInstance({
    listIssueStatuses: async () => ({
      statuses: [
        { id: "s-todo", workspace_id: "ws-1", key: "todo", name: "Todo", description: "", category: "todo", color: "#888", is_system: true, position: 0, archived_at: null, created_at: "", updated_at: "" },
      ],
      categories: [],
      total: 1,
    }),
  } as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={navigation}>
        {/* A pane rendered directly still needs the authoring provider —
            `usePlanAuthoring` throws without one rather than silently
            swallowing actions. `enabled: false` is the flag-off shape, which
            renders no authoring control and so leaves this file's assertions
            about the read presentation exactly as they were. */}
        <PlanAuthoringProvider enabled={false} projectId="project-1" overview={null}>
          {ui}
        </PlanAuthoringProvider>
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

function renderPane(
  getActiveProjectPlan: ApiClient["getActiveProjectPlan"],
  mode: "plan_document" | "plan_pipeline" | "plan_coverage" = "plan_document",
) {
  setApiInstance({
    getActiveProjectPlan,
    listIssueStatuses: async () => ({
      statuses: [
        { id: "s-todo", workspace_id: "ws-1", key: "todo", name: "Todo", description: "", category: "todo", color: "#888", is_system: true, position: 0, archived_at: null, created_at: "", updated_at: "" },
        { id: "s-done", workspace_id: "ws-1", key: "done", name: "Done", description: "", category: "done", color: "#888", is_system: true, position: 0, archived_at: null, created_at: "", updated_at: "" },
      ],
      categories: [],
      total: 2,
    }),
  } as unknown as ApiClient);

  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={navigation}>
        <PlanModePane mode={mode} projectId="project-1" />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

/** All 5 coverage states plus a phase-level blocking dependency, for the Pipeline/Coverage smoke tests below. */
function makeFullOverview(): ProjectPlanOverview {
  const base = makeOverview();
  const templatePart = base.phases[0]!.parts[0]!;
  const parts: ProjectPlanPart[] = [
    { ...templatePart, id: "part-2", title: "Complete part", coverage_state: "complete", rollup: { tasks_done: 2, tasks_total: 2, percent: 100 } },
    { ...templatePart, id: "part-3", title: "Not started part", coverage_state: "not_started", rollup: { tasks_done: 0, tasks_total: 2, percent: 0 } },
    { ...templatePart, id: "part-4", title: "No tasks part", coverage_state: "no_tasks_yet", rollup: { tasks_done: 0, tasks_total: 0, percent: 0 }, issues: [] },
    { ...templatePart, id: "part-5", title: "Covered, no active tasks", coverage_state: "covered_no_active_tasks", rollup: { tasks_done: 0, tasks_total: 0, percent: 0 } },
    // Same 0-done/2-total rollup as "Not started part" above — the API
    // distinguishes these by `coverage_state` alone (some issue is actively
    // started even though none are done), so the badge, not the progress
    // bar, is what must tell them apart.
    { ...templatePart, id: "part-6", title: "In progress zero part", coverage_state: "in_progress", rollup: { tasks_done: 0, tasks_total: 2, percent: 0 } },
  ];
  const phase2: ProjectPlanPhase = {
    id: "phase-2", title: "Phase 2 — Rollout", description: "", attributes: null, position: 1,
    rollup: { tasks_done: 0, tasks_total: 0, percent: 0 },
    parts,
    created_at: "", updated_at: "",
  };
  return {
    ...base,
    phases: [...base.phases, phase2],
    dependencies: [
      {
        id: "dep-1",
        blocked: { type: "phase", id: "phase-2", title: "Phase 2 — Rollout", missing: false },
        blocking: { type: "phase", id: "phase-1", title: "Phase 1 — Foundations", missing: false },
      },
    ],
  };
}

describe("PlanModePane", () => {
  afterEach(cleanup);

  it("shows the loading state before the request settles", () => {
    renderPane(() => new Promise(() => {}));
    expect(screen.getByTestId("plan-loading-state")).toBeInTheDocument();
    expect(screen.queryByTestId("plan-no-plan-state")).not.toBeInTheDocument();
  });

  it("renders true no-plan distinctly from plan-present, on a genuine 404 (null)", async () => {
    renderPane(() => Promise.resolve(null));
    await waitFor(() => expect(screen.getByTestId("plan-no-plan-state")).toBeInTheDocument());
    expect(screen.queryByTestId("plan-error-state")).not.toBeInTheDocument();
    expect(screen.queryByText("Launch Plan")).not.toBeInTheDocument();
  });

  it("renders the plan when the API returns one, distinct from no-plan", async () => {
    renderPane(() => Promise.resolve(makeOverview()));
    await waitFor(() => expect(screen.getByText("Launch Plan")).toBeInTheDocument());
    expect(screen.queryByTestId("plan-no-plan-state")).not.toBeInTheDocument();
    expect(screen.queryByTestId("plan-error-state")).not.toBeInTheDocument();
  });

  it("renders the error state — not no-plan — when the request fails", async () => {
    renderPane(() => Promise.reject(new Error("network error")));
    await waitFor(() => expect(screen.getByTestId("plan-error-state")).toBeInTheDocument());
    expect(screen.queryByTestId("plan-no-plan-state")).not.toBeInTheDocument();
  });

  // Asserts on the state LABELS themselves, not just part titles — a
  // regression that deleted or collapsed the badges (e.g. back to only
  // rendering for the 2 gap states) would still leave every part title in
  // the DOM and pass a title-only check, which is exactly what QC's Major
  // #14 flagged. Each label appears at least once per state present.
  it("renders all 5 coverage states distinctly in the Pipeline pane, with dependency gating", async () => {
    renderPane(() => Promise.resolve(makeFullOverview()), "plan_pipeline");
    await waitFor(() => expect(screen.getByText("Launch Plan")).toBeInTheDocument());
    expect(screen.getAllByText("Done").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Not started").length).toBeGreaterThan(0);
    expect(screen.getAllByText("In progress").length).toBeGreaterThan(0);
    expect(screen.getAllByText("No tasks yet").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Covered · no active tasks").length).toBeGreaterThan(0);
    expect(screen.getByText(/Blocked on Phase 1 — Foundations/)).toBeInTheDocument();
  });

  it("renders all 5 coverage states distinctly in the Coverage pane", async () => {
    renderPane(() => Promise.resolve(makeFullOverview()), "plan_coverage");
    await waitFor(() => expect(screen.getByText("Complete part")).toBeInTheDocument());
    expect(screen.getAllByText("Done").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Not started").length).toBeGreaterThan(0);
    expect(screen.getAllByText("In progress").length).toBeGreaterThan(0);
    expect(screen.getAllByText("No tasks yet").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Covered · no active tasks").length).toBeGreaterThan(0);
  });

  // The canonical regression case for QC Critical #4: two parts with the
  // identical 0-done/2-total rollup (so an identical progress bar) must
  // still read as different states because of the badge alone.
  it("Document pane: renders a state badge for all 5 coverage states, distinguishing in_progress-at-0% from not_started-at-0%", () => {
    renderWithProviders(<PlanDocumentPane overview={makeFullOverview()} />);

    expect(screen.getAllByText("Done").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Not started").length).toBeGreaterThan(0);
    expect(screen.getAllByText("In progress").length).toBeGreaterThan(0);
    expect(screen.getAllByText("No tasks yet").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Covered · no active tasks").length).toBeGreaterThan(0);

    const notStartedPart = screen.getByText("Not started part").closest("div")!;
    const inProgressZeroPart = screen.getByText("In progress zero part").closest("div")!;
    // Both parts show the identical "0/2" progress fraction...
    expect(notStartedPart.textContent).toContain("0");
    expect(inProgressZeroPart.textContent).toContain("0");
    // ...but each carries its own distinct badge, not the other's.
    expect(within(notStartedPart).getByText("Not started")).toBeInTheDocument();
    expect(within(notStartedPart).queryByText("In progress")).not.toBeInTheDocument();
    expect(within(inProgressZeroPart).getByText("In progress")).toBeInTheDocument();
    expect(within(inProgressZeroPart).queryByText("Not started")).not.toBeInTheDocument();
  });
});
