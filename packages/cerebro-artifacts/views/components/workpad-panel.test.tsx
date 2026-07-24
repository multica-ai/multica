// FIR-3765 — the Workpad panel groups a plan's steps by markdown-heading phases
// and renders each phase as a collapsible section headed by a status icon.
// Completed phases start collapsed; the user can fold any phase in or out. A
// plan with fewer than two named phases renders flat, exactly as before.
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
    phaseComplete: wp.phaseComplete,
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

// Fase 1 fully done (starts collapsed), Fase 2 in progress (starts open).
const WITH_DONE_PHASE = [
  "## Fase 1: Byg",
  "- [x] Byg A",
  "- [x] Byg B",
  "## Fase 2: Test",
  "- [x] Test C",
  "- [ ] Test D",
].join("\n");

describe("WorkpadPanel phases", () => {
  beforeEach(() => {
    planRef.value = null;
  });

  it("renders each phase as a header with its status and shows every step", () => {
    planRef.value = { id: "p1", title: "Plan", body: MULTI };
    render(<WorkpadPanel issueId="issue-1" />);

    // No phase-filter chips anymore.
    expect(screen.queryByRole("button", { name: "All" })).toBeNull();

    // A header button per named phase.
    expect(screen.getByRole("button", { name: /Fase 1: Byg/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Fase 2: Test/ })).toBeTruthy();

    // Phase status is derived from progress: Fase 1 in progress, Fase 2 todo.
    const statuses = screen
      .getAllByTestId("status-icon")
      .map((el) => el.getAttribute("data-status"));
    expect(statuses).toContain("in_progress");
    expect(statuses).toContain("todo");

    // Both phases start expanded (neither is complete) → all steps visible.
    expect(screen.getByText("Byg A")).toBeTruthy();
    expect(screen.getByText("Byg B")).toBeTruthy();
    expect(screen.getByText("Test C")).toBeTruthy();
  });

  it("collapses and expands a phase when its header is clicked", () => {
    planRef.value = { id: "p1", title: "Plan", body: MULTI };
    render(<WorkpadPanel issueId="issue-1" />);

    // Collapse Fase 1 → its steps hide, other phase untouched.
    fireEvent.click(screen.getByRole("button", { name: /Fase 1: Byg/ }));
    expect(screen.queryByText("Byg A")).toBeNull();
    expect(screen.getByText("Test C")).toBeTruthy();

    // Expand it again → steps return.
    fireEvent.click(screen.getByRole("button", { name: /Fase 1: Byg/ }));
    expect(screen.getByText("Byg A")).toBeTruthy();
  });

  it("collapses a completed phase by default and marks it done", () => {
    planRef.value = { id: "p1", title: "Plan", body: WITH_DONE_PHASE };
    render(<WorkpadPanel issueId="issue-1" />);

    // Fase 1 is complete → collapsed by default (its steps are hidden).
    expect(screen.queryByText("Byg A")).toBeNull();
    // Fase 2 is in progress → open by default.
    expect(screen.getByText("Test C")).toBeTruthy();

    const statuses = screen
      .getAllByTestId("status-icon")
      .map((el) => el.getAttribute("data-status"));
    expect(statuses).toContain("done");
    expect(statuses).toContain("in_progress");

    // Expanding the completed phase reveals its steps.
    fireEvent.click(screen.getByRole("button", { name: /Fase 1: Byg/ }));
    expect(screen.getByText("Byg A")).toBeTruthy();
  });

  it("renders a flat list with no phase headers for a plan without named phases", () => {
    planRef.value = { id: "p1", title: "Plan", body: "- [ ] Step one\n- [x] Step two" };
    render(<WorkpadPanel issueId="issue-1" />);

    expect(screen.queryByTestId("status-icon")).toBeNull();
    expect(screen.queryByRole("button", { name: "All" })).toBeNull();
    expect(screen.getByText("Step one")).toBeTruthy();
    expect(screen.getByText("Step two")).toBeTruthy();
  });
});
