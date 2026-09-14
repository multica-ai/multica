// @vitest-environment jsdom

/**
 * Component coverage for manual plan authoring.
 *
 * WHAT THIS FILE COVERS — and, just as importantly, what it does not.
 *
 * `renderPane` replaces the API singleton with fake methods (`setApiInstance`
 * below), so nothing here executes `ApiClient` and nothing here touches a URL
 * or an HTTP verb. What it does prove is the layer above that: which client
 * METHOD each affordance calls, with which arguments, in which order, and only
 * after the right interaction. An earlier version of this header claimed the
 * flow hit "the real client method" and exercised the URL/verb table; that was
 * false, and QC was right to fail it.
 *
 * The four things pinned here:
 *
 * 1. **Flag gating.** With `project_plans` off, no authoring affordance is
 *    visible or reachable (AC 3).
 * 2. **Argument shape.** Create sends only `{kind,title,description}` — no
 *    field through which a manual plan could claim issue provenance (AC 5) —
 *    and appended items carry a position derived from siblings.
 * 3. **Request timing.** No destructive or version-rotating write leaves the
 *    client until its confirmation is pressed (AC 2, AC 8).
 * 4. **Honest failure.** A 409 renders as a conflict rather than a generic
 *    error, a failed candidate query is not reported as "no matches" (AC 7),
 *    and the dialog stays open either way.
 *
 * Coverage that lives elsewhere, deliberately, so it is not re-run through a
 * DOM mount:
 *
 * - `@multica/core/project-plan/write-routes.test.ts` — the route table. That
 *   is transcription coverage: it asserts the paths and verbs match the
 *   server's contract literally. It does not prove `ApiClient` uses them.
 * - `@multica/core/api/client.test.ts` — `describe("project plan writes")`
 *   drives the real `ApiClient` against a stubbed `fetch` and is the only
 *   place the method → URL/verb wiring is actually executed.
 * - `authoring/plan-ordering.test.ts` — the position/permutation matrix.
 * - `@multica/core/project-plan/errors.test.ts` — the error classification
 *   matrix.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import { ApiError, type ApiClient } from "@multica/core/api/client";
import { configStore } from "@multica/core/config";
import { PROJECT_PLANS_FLAG } from "@multica/core/feature-flags";
import type { Issue, ProjectPlanOverview } from "@multica/core/types";
import { TooltipProvider } from "@multica/ui/components/ui/tooltip";
import { NavigationProvider, type NavigationAdapter } from "../../../navigation";
import { renderWithI18n } from "../../../test/i18n";
import { PlanModePane } from "./plan-mode-pane";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

const navigation: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/projects/project-1",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => `https://app.example${path}`,
};

const STATUSES = {
  statuses: [
    {
      id: "s-todo", workspace_id: "ws-1", key: "todo", name: "Todo", description: "",
      category: "todo", color: "#888", is_system: true, position: 0, archived_at: null,
      created_at: "", updated_at: "",
    },
  ],
  categories: [],
  total: 1,
};

function makeOverview(overrides: Partial<ProjectPlanOverview> = {}): ProjectPlanOverview {
  return {
    plan: {
      id: "plan-1", workspace_id: "ws-1", project_id: "project-1", version: 2,
      kind: "prd", origin: "manual", title: "Hand-authored plan", description: "",
      attributes: null, source_issue_id: null, superseded: false, superseded_at: null,
      created_by_type: "member", created_by_id: "member-1", created_at: "", updated_at: "",
    },
    rollup: {
      tasks_done: 0, tasks_total: 0, percent: 0,
      parts_covered: 0, parts_total: 1, parts_without_tasks: 1,
    },
    phases: [
      {
        id: "phase-1", title: "Phase 1 — Foundations", description: "", attributes: null,
        position: 0, rollup: { tasks_done: 0, tasks_total: 0, percent: 0 },
        parts: [
          {
            id: "part-1", title: "Schema", description: "", acceptance_criteria: "",
            attributes: null, position: 0, coverage_state: "no_tasks_yet",
            rollup: { tasks_done: 0, tasks_total: 0, percent: 0 },
            issues: [], created_at: "", updated_at: "",
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

/** One linkable issue in the plan's project, typed rather than cast. */
function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "issue-9", workspace_id: "ws-1", number: 9, identifier: "LOCO-9",
    title: "Recovered issue", description: null, status: "todo", status_category: "todo",
    priority: "none", assignee_type: null, assignee_id: null,
    creator_type: "member", creator_id: "member-1", parent_issue_id: null,
    project_id: "project-1", position: 0, stage: null, start_date: null, due_date: null,
    metadata: {}, properties: {}, created_at: "", updated_at: "",
    ...overrides,
  };
}

function renderPane(api: Partial<ApiClient>, mode: "plan_document" = "plan_document") {
  setApiInstance({ listIssueStatuses: async () => STATUSES, ...api } as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <TooltipProvider delay={0}>
        <NavigationProvider value={navigation}>
          <PlanModePane mode={mode} projectId="project-1" />
        </NavigationProvider>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

function setFlag(on: boolean) {
  configStore.setState({ featureFlags: { [PROJECT_PLANS_FLAG]: on } });
}

beforeEach(() => setFlag(true));
afterEach(() => {
  cleanup();
  configStore.setState({ featureFlags: {} });
  vi.clearAllMocks();
});

describe("flag gating (acceptance criteria #3)", () => {
  it("offers no create-plan entry point on the empty state with the flag off", async () => {
    setFlag(false);
    renderPane({ getActiveProjectPlan: async () => null });
    await screen.findByTestId("plan-no-plan-state");
    expect(screen.queryByTestId("plan-authoring-create-plan")).toBeNull();
  });

  it("offers no plan, phase, or part affordance on a populated plan with the flag off", async () => {
    setFlag(false);
    renderPane({ getActiveProjectPlan: async () => makeOverview() });
    await screen.findByText("Hand-authored plan");
    expect(screen.queryByTestId("plan-authoring-plan-menu")).toBeNull();
    expect(screen.queryByTestId("plan-authoring-add-phase")).toBeNull();
    expect(screen.queryByRole("button", { name: /Actions for phase/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Actions for part/ })).toBeNull();
  });

  it("shows all of them with the flag on", async () => {
    renderPane({ getActiveProjectPlan: async () => makeOverview() });
    await screen.findByText("Hand-authored plan");
    expect(screen.getByTestId("plan-authoring-plan-menu")).toBeTruthy();
    expect(screen.getByTestId("plan-authoring-add-phase")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Actions for phase/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Actions for part/ })).toBeTruthy();
  });

  it("hides authoring on a superseded version, which the service refuses to write to", async () => {
    renderPane({
      getActiveProjectPlan: async () =>
        makeOverview({
          plan: { ...makeOverview().plan, superseded: true, superseded_at: "2026-01-01T00:00:00Z" },
        }),
    });
    await screen.findByText("Hand-authored plan");
    expect(screen.queryByTestId("plan-authoring-plan-menu")).toBeNull();
    expect(screen.queryByTestId("plan-authoring-add-phase")).toBeNull();
  });
});

describe("creating a plan from the empty state", () => {
  it("posts a manual plan with no source provenance, then re-reads", async () => {
    const user = userEvent.setup();
    const createManualProjectPlan = vi.fn().mockResolvedValue(undefined);
    let plan: ProjectPlanOverview | null = null;
    renderPane({
      getActiveProjectPlan: async () => plan,
      createManualProjectPlan: async (projectId: string, data) => {
        createManualProjectPlan(projectId, data);
        plan = makeOverview();
      },
    });

    await user.click(await screen.findByTestId("plan-authoring-create-plan"));
    await user.type(screen.getByLabelText("Plan title"), "Q3 launch plan");
    await user.click(screen.getByRole("button", { name: "Create plan" }));

    await waitFor(() =>
      expect(createManualProjectPlan).toHaveBeenCalledWith("project-1", {
        kind: "prd",
        title: "Q3 launch plan",
        description: "",
      }),
    );
    // Provenance stays honest: a hand-authored plan sends nothing that could
    // make it claim an issue as its source (acceptance criteria #5).
    const [, body] = createManualProjectPlan.mock.calls[0]!;
    expect(Object.keys(body as object).sort()).toEqual(["description", "kind", "title"]);
  });

  it("will not submit an empty title", async () => {
    const user = userEvent.setup();
    renderPane({ getActiveProjectPlan: async () => null });
    await user.click(await screen.findByTestId("plan-authoring-create-plan"));
    expect((screen.getByRole("button", { name: "Create plan" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
  });
});

describe("adding phases and parts", () => {
  it("appends a phase one past the highest position in use", async () => {
    const user = userEvent.setup();
    const createProjectPlanPhase = vi.fn().mockResolvedValue(undefined);
    renderPane({ getActiveProjectPlan: async () => makeOverview(), createProjectPlanPhase });

    await user.click(await screen.findByTestId("plan-authoring-add-phase"));
    await user.type(screen.getByLabelText("Phase title"), "Phase 2 — Rollout");
    await user.click(screen.getByRole("button", { name: "Add phase" }));

    await waitFor(() =>
      expect(createProjectPlanPhase).toHaveBeenCalledWith("project-1", "plan-1", {
        title: "Phase 2 — Rollout",
        description: "",
        position: 1,
      }),
    );
  });

  it("adds a part to its phase and says up front that it will read as no-tasks-yet", async () => {
    const user = userEvent.setup();
    const createProjectPlanPart = vi.fn().mockResolvedValue(undefined);
    renderPane({ getActiveProjectPlan: async () => makeOverview(), createProjectPlanPart });

    await user.click(await screen.findByRole("button", { name: /Add part to Phase 1/ }));
    const dialog = await screen.findByRole("dialog");
    // The dialog names the coverage state a new part will land in, so the amber
    // treatment that follows is expected rather than surprising.
    expect(dialog.textContent).toContain("No tasks yet");
    await user.type(screen.getByLabelText("Part title"), "Migration");
    await user.click(screen.getByRole("button", { name: "Add part" }));

    await waitFor(() =>
      expect(createProjectPlanPart).toHaveBeenCalledWith("project-1", "plan-1", "phase-1", {
        title: "Migration",
        description: "",
        acceptance_criteria: "",
        position: 1,
      }),
    );
  });
});

describe("destructive actions are confirmed", () => {
  it("does not delete a part until the confirmation is accepted", async () => {
    const user = userEvent.setup();
    const deleteProjectPlanPart = vi.fn().mockResolvedValue(undefined);
    renderPane({ getActiveProjectPlan: async () => makeOverview(), deleteProjectPlanPart });

    await user.click(await screen.findByRole("button", { name: /Actions for part/ }));
    await user.click(await screen.findByRole("menuitem", { name: "Delete part" }));

    // Confirmation is on screen and nothing has been sent yet.
    expect(await screen.findByText(/Delete “Schema”\?/)).toBeTruthy();
    expect(deleteProjectPlanPart).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Delete part" }));
    await waitFor(() =>
      expect(deleteProjectPlanPart).toHaveBeenCalledWith("project-1", "plan-1", "part-1"),
    );
  });

  it("states that unlinking is not deleting, so the confirmation is not misread", async () => {
    const user = userEvent.setup();
    renderPane({ getActiveProjectPlan: async () => makeOverview() });
    await user.click(await screen.findByTestId("plan-authoring-plan-menu"));
    await user.click(await screen.findByRole("menuitem", { name: "Delete plan" }));
    expect(await screen.findByText(/The issues themselves are not deleted/)).toBeTruthy();
  });

  it("does not delete a phase until the confirmation is accepted", async () => {
    const user = userEvent.setup();
    const deleteProjectPlanPhase = vi.fn().mockResolvedValue(undefined);
    renderPane({ getActiveProjectPlan: async () => makeOverview(), deleteProjectPlanPhase });

    await user.click(await screen.findByRole("button", { name: /Actions for phase/ }));
    await user.click(await screen.findByRole("menuitem", { name: "Delete phase" }));

    expect(await screen.findByRole("alertdialog")).toBeTruthy();
    expect(deleteProjectPlanPhase).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Delete phase" }));
    await waitFor(() =>
      expect(deleteProjectPlanPhase).toHaveBeenCalledWith("project-1", "plan-1", "phase-1"),
    );
  });

  it("does not delete the plan until the confirmation is accepted", async () => {
    const user = userEvent.setup();
    const deleteProjectPlan = vi.fn().mockResolvedValue(undefined);
    renderPane({ getActiveProjectPlan: async () => makeOverview(), deleteProjectPlan });

    await user.click(await screen.findByTestId("plan-authoring-plan-menu"));
    await user.click(await screen.findByRole("menuitem", { name: "Delete plan" }));

    expect(await screen.findByRole("alertdialog")).toBeTruthy();
    expect(deleteProjectPlan).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Delete plan" }));
    await waitFor(() => expect(deleteProjectPlan).toHaveBeenCalledWith("project-1", "plan-1"));
  });

  it("cancelling a confirmation sends nothing at all", async () => {
    const user = userEvent.setup();
    const deleteProjectPlan = vi.fn().mockResolvedValue(undefined);
    renderPane({ getActiveProjectPlan: async () => makeOverview(), deleteProjectPlan });

    await user.click(await screen.findByTestId("plan-authoring-plan-menu"));
    await user.click(await screen.findByRole("menuitem", { name: "Delete plan" }));
    await user.click(await screen.findByRole("button", { name: "Cancel" }));

    expect(deleteProjectPlan).not.toHaveBeenCalled();
  });
});

describe("supersede goes through its own confirmation (AC 8)", () => {
  /**
   * Supersede both collects input and rotates which version is active, so it
   * is a two-stage flow: a form that sends nothing, then a confirmation that
   * does. The bug this replaces fired the request straight off the form's
   * submit button.
   */
  async function openSupersedeForm(user: ReturnType<typeof userEvent.setup>) {
    await user.click(await screen.findByTestId("plan-authoring-plan-menu"));
    await user.click(await screen.findByRole("menuitem", { name: "Supersede plan" }));
  }

  it("sends nothing when the form stage is submitted", async () => {
    const user = userEvent.setup();
    const supersedeProjectPlan = vi.fn().mockResolvedValue(undefined);
    renderPane({ getActiveProjectPlan: async () => makeOverview(), supersedeProjectPlan });

    await openSupersedeForm(user);
    await user.clear(screen.getByLabelText("Plan title"));
    await user.type(screen.getByLabelText("Plan title"), "Rewritten plan");
    // The form stage advances rather than saving, so its button says Continue.
    await user.click(screen.getByRole("button", { name: "Continue" }));

    // Stage 2 is on screen and still nothing has left the client.
    expect(await screen.findByRole("alertdialog")).toBeTruthy();
    expect(supersedeProjectPlan).not.toHaveBeenCalled();
  });

  it("sends only after the confirmation is accepted, carrying the edited fields", async () => {
    const user = userEvent.setup();
    const supersedeProjectPlan = vi.fn().mockResolvedValue(undefined);
    renderPane({ getActiveProjectPlan: async () => makeOverview(), supersedeProjectPlan });

    await openSupersedeForm(user);
    await user.clear(screen.getByLabelText("Plan title"));
    await user.type(screen.getByLabelText("Plan title"), "Rewritten plan");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await user.click(await screen.findByRole("button", { name: "Supersede" }));

    await waitFor(() =>
      expect(supersedeProjectPlan).toHaveBeenCalledWith("project-1", "plan-1", {
        title: "Rewritten plan",
        description: "",
      }),
    );
  });

  it("sends nothing when the confirmation is cancelled", async () => {
    const user = userEvent.setup();
    const supersedeProjectPlan = vi.fn().mockResolvedValue(undefined);
    renderPane({ getActiveProjectPlan: async () => makeOverview(), supersedeProjectPlan });

    await openSupersedeForm(user);
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await user.click(await screen.findByRole("button", { name: "Cancel" }));

    expect(supersedeProjectPlan).not.toHaveBeenCalled();
  });

  it("names the version being archived and the one replacing it", async () => {
    const user = userEvent.setup();
    renderPane({ getActiveProjectPlan: async () => makeOverview() });
    await openSupersedeForm(user);
    await user.click(screen.getByRole("button", { name: "Continue" }));
    // makeOverview() is version 2. Nothing here is interpolation-leaked.
    const confirm = await screen.findByRole("alertdialog");
    expect(confirm.textContent).toContain("Archives version 2");
    expect(confirm.textContent).toContain("version 3");
    expect(confirm.textContent).not.toContain("{{");
  });
});

describe("a failed candidate query is not reported as an empty one (AC 7)", () => {
  function overviewWithLinkablePart() {
    return makeOverview();
  }

  it("shows honest failure copy and a retry, not \"no issues match\"", async () => {
    const user = userEvent.setup();
    renderPane({
      getActiveProjectPlan: async () => overviewWithLinkablePart(),
      listIssues: async () => {
        throw new ApiError("API error: 500", 500, "Internal Server Error", {});
      },
    });

    await user.click(await screen.findByRole("button", { name: "Link issues" }));

    const failure = await screen.findByTestId("plan-link-candidates-error");
    expect(failure.textContent).toContain("Couldn't search this project's issues");
    // The empty-result message must NOT be what the reader sees.
    expect(screen.queryByTestId("plan-link-candidates-empty")).toBeNull();
    expect(screen.queryByText(/No issues in this project match/)).toBeNull();
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
  });

  it("retry re-runs the query and recovers once it succeeds", async () => {
    const user = userEvent.setup();
    let fail = true;
    renderPane({
      getActiveProjectPlan: async () => overviewWithLinkablePart(),
      listIssues: async () => {
        if (fail) throw new ApiError("API error: 500", 500, "Internal Server Error", {});
        return { issues: [makeIssue()], total: 1 };
      },
    });

    await user.click(await screen.findByRole("button", { name: "Link issues" }));
    await screen.findByTestId("plan-link-candidates-error");

    fail = false;
    await user.click(screen.getByRole("button", { name: "Try again" }));

    expect(await screen.findByText("Recovered issue")).toBeTruthy();
    expect(screen.queryByTestId("plan-link-candidates-error")).toBeNull();
  });

  it("still says \"no matches\" when the query succeeds and returns nothing", async () => {
    const user = userEvent.setup();
    renderPane({
      getActiveProjectPlan: async () => overviewWithLinkablePart(),
      listIssues: async () => ({ issues: [], total: 0 }),
    });

    await user.click(await screen.findByRole("button", { name: "Link issues" }));

    expect(await screen.findByTestId("plan-link-candidates-empty")).toBeTruthy();
    expect(screen.queryByTestId("plan-link-candidates-error")).toBeNull();
  });
});

describe("dialog layout containment", () => {
  const longTitle =
    "Publish the plans default-on commit onto the integration branch after every required verification";

  it("lets the link-issues grid item shrink around long issue titles", async () => {
    const user = userEvent.setup();
    renderPane({
      getActiveProjectPlan: async () => makeOverview(),
      listIssues: async () => ({
        issues: [
          makeIssue({
            title: longTitle,
          }),
        ],
        total: 1,
      }),
    });

    await user.click(await screen.findByRole("button", { name: "Link issues" }));
    await screen.findByText(/Publish the plans default-on commit/);

    const dialogBody = screen.getByText("Linked issues").parentElement?.parentElement;
    expect(dialogBody?.classList.contains("min-w-0")).toBe(true);
  });

  it("reveals the complete candidate title on pointer hover and keyboard focus", async () => {
    const user = userEvent.setup();
    renderPane({
      getActiveProjectPlan: async () => makeOverview(),
      listIssues: async () => ({ issues: [makeIssue({ title: longTitle })], total: 1 }),
    });

    await user.click(await screen.findByRole("button", { name: "Link issues" }));
    const row = await screen.findByRole("button", { name: longTitle });

    await user.hover(row);
    await waitFor(() =>
      expect(document.querySelector('[data-slot="tooltip-content"]')?.textContent).toBe(longTitle),
    );

    await user.unhover(row);
    await waitFor(() =>
      expect(document.querySelector('[data-slot="tooltip-content"]')).toBeNull(),
    );

    const search = screen.getByPlaceholderText("Search this project's issues…");
    search.focus();
    await user.tab();

    expect(row).toHaveFocus();
    await waitFor(() =>
      expect(document.querySelector('[data-slot="tooltip-content"]')?.textContent).toBe(longTitle),
    );
  });
});

describe("honest error surfacing (acceptance criteria: a 409 reads as a conflict)", () => {
  it("renders a 409 as a conflict and keeps the dialog open", async () => {
    const user = userEvent.setup();
    renderPane({
      getActiveProjectPlan: async () => null,
      createManualProjectPlan: async () => {
        throw new ApiError("API error: 409", 409, "Conflict", {
          code: "active_plan_exists",
          error: "this project already has an active plan",
        });
      },
    });

    await user.click(await screen.findByTestId("plan-authoring-create-plan"));
    await user.type(screen.getByLabelText("Plan title"), "Second plan");
    await user.click(screen.getByRole("button", { name: "Create plan" }));

    const notice = await screen.findByTestId("plan-write-conflict");
    expect(notice.textContent).toContain("This plan changed");
    // The server's own specific wording, not a generic "couldn't save".
    expect(notice.textContent).toContain("this project already has an active plan");
    expect(screen.queryByTestId("plan-write-error")).toBeNull();
    // Still open, with the typed title intact, so the reader can retry.
    expect((screen.getByLabelText("Plan title") as HTMLInputElement).value).toBe("Second plan");
  });

  it("renders a 500 as a failure rather than as a conflict", async () => {
    const user = userEvent.setup();
    renderPane({
      getActiveProjectPlan: async () => null,
      createManualProjectPlan: async () => {
        throw new ApiError("API error: 500", 500, "Internal Server Error", {});
      },
    });

    await user.click(await screen.findByTestId("plan-authoring-create-plan"));
    await user.type(screen.getByLabelText("Plan title"), "Anything");
    await user.click(screen.getByRole("button", { name: "Create plan" }));

    const notice = await screen.findByTestId("plan-write-error");
    expect(notice.textContent).toContain("Couldn't save");
    expect(screen.queryByTestId("plan-write-conflict")).toBeNull();
  });
});

describe("a hand-authored plan renders honestly in the panes", () => {
  it("shows a part with no linked issues as no-tasks-yet, not as complete or 0%", async () => {
    renderPane({ getActiveProjectPlan: async () => makeOverview() });
    await screen.findByText("Hand-authored plan");
    // The amber dashed state, by its own label — never "Done", never a 0/0 bar.
    expect(screen.getByText("No tasks yet")).toBeTruthy();
    expect(screen.queryByText("Done")).toBeNull();
    expect(screen.getByText(/Identified in the plan — no issues created yet/)).toBeTruthy();
  });
});
