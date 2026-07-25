// FIR-3765 — the Workpad panel groups a plan's steps by markdown-heading phases
// and renders each phase as a collapsible section headed by a status icon.
// Everything starts folded: the panel itself is collapsed until the Workpad icon
// is clicked, and so is every phase. The status row is the only toggle (no
// disclosure arrow), and a phase's sub-steps render one shade lighter.
import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

const planRef = vi.hoisted(() => ({
  value: null as { id: string; title: string; body: string } | null,
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => true,
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey?: unknown[] }) =>
    opts.queryKey?.[0] === "versions"
      ? { data: [] }
      : { data: planRef.value ? [planRef.value] : [] },
}));

vi.mock("@multica/cerebro-artifacts/core", async () => {
  const wp =
    await vi.importActual<typeof import("../../core/workpad")>("../../core/workpad");
  return {
    parseWorkpadPhases: wp.parseWorkpadPhases,
    namedPhases: wp.namedPhases,
    workpadProgress: wp.workpadProgress,
    phaseStatus: wp.phaseStatus,
    selectIssuePlan: () => planRef.value,
    artifactsByIssueOptions: () => ({ queryKey: ["artifacts"] }),
    planVersionsOptions: () => ({ queryKey: ["versions"] }),
  };
});

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ documentDetail: () => "/doc" }),
}));
vi.mock("@multica/views/navigation", () => ({ useNavigation: () => ({}) }));
// Lightweight StatusIcon stub — surfaces the resolved status for assertions
// without pulling the full issues barrel into the component test.
vi.mock("@multica/views/issues/components", () => ({
  StatusIcon: ({ status }: { status: string }) => (
    <span data-testid="status-icon" data-status={status} />
  ),
}));
vi.mock("@multica/ui/components/ui/checkbox", () => ({
  Checkbox: (props: { checked?: boolean }) => (
    <input type="checkbox" readOnly checked={Boolean(props.checked)} />
  ),
}));

import { WorkpadPanel } from "./workpad-panel";

// Fase 1 is partially done (in progress), Fase 2 is untouched (todo).
const MULTI = [
  "## Fase 1: Byg",
  "- [ ] Byg A",
  "- [x] Byg B",
  "## Fase 2: Test",
  "- [ ] Test C",
].join("\n");

// Fase 1 fully done, Fase 2 in progress — neither starts expanded.
const WITH_DONE_PHASE = [
  "## Fase 1: Byg",
  "- [x] Byg A",
  "- [x] Byg B",
  "## Fase 2: Test",
  "- [x] Test C",
  "- [ ] Test D",
].join("\n");

const FLAT = "- [ ] Step one\n- [x] Step two";

// Opens the panel body the way a user does — via the Workpad icon.
function openPanel() {
  fireEvent.click(screen.getByRole("button", { name: "Toggle Workpad" }));
}

// Clicks a phase's status icon — the only toggle a phase row has.
function clickPhaseStatus(index: number) {
  const icon = screen.getAllByTestId("status-icon")[index];
  if (!icon) throw new Error(`no phase status icon at index ${index}`);
  fireEvent.click(icon);
}

describe("WorkpadPanel", () => {
  beforeEach(() => {
    planRef.value = null;
  });

  it("starts collapsed and toggles the whole panel on the Workpad icon", () => {
    planRef.value = { id: "p1", title: "Plan", body: MULTI };
    render(<WorkpadPanel issueId="issue-1" />);

    // Collapsed: the header and its progress are all that render.
    expect(screen.getByTestId("workpad-panel")).toBeTruthy();
    expect(screen.getByText("1/3")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Fase 1: Byg/ })).toBeNull();
    expect(screen.queryByText("Plan")).toBeNull();

    // The icon opens it.
    openPanel();
    expect(screen.getByRole("button", { name: /Fase 1: Byg/ })).toBeTruthy();

    // …and folds it away again.
    openPanel();
    expect(screen.queryByRole("button", { name: /Fase 1: Byg/ })).toBeNull();
  });

  it("renders each phase as a header with its status, all folded", () => {
    planRef.value = { id: "p1", title: "Plan", body: MULTI };
    render(<WorkpadPanel issueId="issue-1" />);
    openPanel();

    // No phase-filter chips anymore.
    expect(screen.queryByRole("button", { name: "All" })).toBeNull();

    // A header button per named phase.
    expect(screen.getByRole("button", { name: /Fase 1: Byg/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Fase 2: Test/ })).toBeTruthy();

    // Phase status is derived from progress: Fase 1 in progress, Fase 2 todo.
    const statuses = screen
      .getAllByTestId("status-icon")
      .map((el) => el.getAttribute("data-status"));
    expect(statuses).toEqual(["in_progress", "todo"]);

    // Every phase starts closed — no steps on screen yet.
    expect(screen.queryByText("Byg A")).toBeNull();
    expect(screen.queryByText("Test C")).toBeNull();
  });

  it("starts a completed phase closed too", () => {
    planRef.value = { id: "p1", title: "Plan", body: WITH_DONE_PHASE };
    render(<WorkpadPanel issueId="issue-1" />);
    openPanel();

    const statuses = screen
      .getAllByTestId("status-icon")
      .map((el) => el.getAttribute("data-status"));
    expect(statuses).toEqual(["done", "in_progress"]);

    expect(screen.queryByText("Byg A")).toBeNull();
    expect(screen.queryByText("Test D")).toBeNull();
  });

  it("expands and collapses a phase when its status is clicked", () => {
    planRef.value = { id: "p1", title: "Plan", body: MULTI };
    render(<WorkpadPanel issueId="issue-1" />);
    openPanel();

    // Clicking the phase's status icon — there is no disclosure arrow — reveals
    // that phase's steps and leaves the other phase untouched.
    clickPhaseStatus(0);
    expect(screen.getByText("Byg A")).toBeTruthy();
    expect(screen.getByText("Byg B")).toBeTruthy();
    expect(screen.queryByText("Test C")).toBeNull();

    // Clicking it again folds the phase back up.
    clickPhaseStatus(0);
    expect(screen.queryByText("Byg A")).toBeNull();
  });

  it("renders a phase's sub-steps lighter than a flat plan's steps", () => {
    planRef.value = { id: "p1", title: "Plan", body: MULTI };
    render(<WorkpadPanel issueId="issue-1" />);
    openPanel();
    clickPhaseStatus(0);

    // An open sub-step reads as detail under its phase title.
    expect(screen.getByText("Byg A").className).toContain(
      "text-muted-foreground",
    );
  });

  it("renders a flat list with no phase headers for a plan without named phases", () => {
    planRef.value = { id: "p1", title: "Plan", body: FLAT };
    render(<WorkpadPanel issueId="issue-1" />);
    openPanel();

    expect(screen.queryByTestId("status-icon")).toBeNull();
    expect(screen.queryByRole("button", { name: "All" })).toBeNull();
    expect(screen.getByText("Step one")).toBeTruthy();
    expect(screen.getByText("Step two")).toBeTruthy();

    // A top-level step is NOT subdued — only a phase's sub-steps are.
    expect(screen.getByText("Step one").className).not.toContain(
      "text-muted-foreground",
    );
  });
});
