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
// The step marker forwards the props that decide whether it is legible: it must
// be `readOnly` and NOT `disabled`, because `disabled` is what pulls in the
// shared Checkbox's `disabled:opacity-50` and made the empty boxes almost
// invisible (FIR-3765 review).
vi.mock("@multica/ui/components/ui/checkbox", () => ({
  Checkbox: (props: {
    checked?: boolean;
    readOnly?: boolean;
    disabled?: boolean;
    className?: string;
  }) => (
    <input
      type="checkbox"
      readOnly
      checked={Boolean(props.checked)}
      data-testid="step-box"
      data-readonly={props.readOnly ? "true" : "false"}
      data-disabled={props.disabled ? "true" : "false"}
      data-class={props.className ?? ""}
    />
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

  // FIR-3765 (Jesper's review) — the header's icon is a status circle like the
  // phase circles beneath it, filled proportionally so the collapsed panel alone
  // shows how far the plan has come.
  it("shows the plan's overall progress in the header circle", () => {
    planRef.value = { id: "p1", title: "Plan", body: MULTI };
    render(<WorkpadPanel issueId="issue-1" />);

    // Collapsed: 1 of 3 steps done → the circle is a third full.
    const circle = screen.getByTestId("workpad-progress-circle");
    expect(Number(circle.getAttribute("data-fraction"))).toBeCloseTo(1 / 3, 5);

    // The circle stays the panel toggle.
    openPanel();
    expect(screen.getByRole("button", { name: /Fase 1: Byg/ })).toBeTruthy();
  });

  it("fills the header circle completely when every step is done", () => {
    planRef.value = {
      id: "p1",
      title: "Plan",
      body: "## Fase 1\n- [x] A\n## Fase 2\n- [x] B",
    };
    render(<WorkpadPanel issueId="issue-1" />);

    const circle = screen.getByTestId("workpad-progress-circle");
    expect(Number(circle.getAttribute("data-fraction"))).toBe(1);
  });

  // FIR-3765 (Jesper's review) — the step boxes were almost invisible because
  // `disabled` dimmed them to 50% on top of an already-faint border.
  it("renders step boxes read-only rather than disabled, so they are not dimmed", () => {
    planRef.value = { id: "p1", title: "Plan", body: MULTI };
    render(<WorkpadPanel issueId="issue-1" />);
    openPanel();
    clickPhaseStatus(0);

    const boxes = screen.getAllByTestId("step-box");
    expect(boxes.length).toBeGreaterThan(0);
    for (const box of boxes) {
      expect(box.getAttribute("data-disabled")).toBe("false");
      expect(box.getAttribute("data-readonly")).toBe("true");
      // …and the unchecked box gets a visible edge instead of `border-input`.
      expect(box.getAttribute("data-class")).toContain("border-muted-foreground");
    }
  });

  // FIR-3765 (Jesper's review) — a rule between the heading and the plan title.
  it("separates the heading from the plan title with a rule when expanded", () => {
    planRef.value = { id: "p1", title: "Plan", body: MULTI };
    const { container } = render(<WorkpadPanel issueId="issue-1" />);

    // Collapsed: heading only, no rule.
    expect(container.querySelector('[data-slot="separator"]')).toBeNull();

    openPanel();
    const rule = container.querySelector('[data-slot="separator"]');
    expect(rule).toBeTruthy();

    // The rule sits between the heading and the plan title line.
    const title = screen.getByText("Plan");
    expect(
      rule!.compareDocumentPosition(title) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
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
