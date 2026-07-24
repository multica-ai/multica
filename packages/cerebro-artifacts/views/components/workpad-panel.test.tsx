// FIR-3765 — the Workpad panel groups a plan's steps by markdown-heading phases
// and offers an "All / <phase>" filter. A plan with fewer than two named
// phases renders flat, with no filter, exactly as before.
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
vi.mock("@multica/ui/components/ui/checkbox", () => ({
  Checkbox: (props: { checked?: boolean }) => (
    <input type="checkbox" readOnly checked={Boolean(props.checked)} />
  ),
}));

import { WorkpadPanel } from "./workpad-panel";

const MULTI = [
  "## Fase 1: Byg",
  "- [ ] Byg A",
  "- [x] Byg B",
  "## Fase 2: Test",
  "- [ ] Test C",
].join("\n");

describe("WorkpadPanel phase filter", () => {
  beforeEach(() => {
    planRef.value = null;
  });

  it("shows a phase filter and every step when a plan has two named phases", () => {
    planRef.value = { id: "p1", title: "Plan", body: MULTI };
    render(<WorkpadPanel issueId="issue-1" />);

    expect(screen.getByRole("button", { name: "All" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Fase 1: Byg" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Fase 2: Test" })).toBeTruthy();

    expect(screen.getByText("Byg A")).toBeTruthy();
    expect(screen.getByText("Test C")).toBeTruthy();
  });

  it("filters to a single phase when its chip is clicked", () => {
    planRef.value = { id: "p1", title: "Plan", body: MULTI };
    render(<WorkpadPanel issueId="issue-1" />);

    fireEvent.click(screen.getByRole("button", { name: "Fase 2: Test" }));

    expect(screen.queryByText("Byg A")).toBeNull();
    expect(screen.queryByText("Byg B")).toBeNull();
    expect(screen.getByText("Test C")).toBeTruthy();
  });

  it("renders a flat list with no filter for a plan without named phases", () => {
    planRef.value = { id: "p1", title: "Plan", body: "- [ ] Step one\n- [x] Step two" };
    render(<WorkpadPanel issueId="issue-1" />);

    expect(screen.queryByRole("group", { name: "Filter plan by phase" })).toBeNull();
    expect(screen.queryByRole("button", { name: "All" })).toBeNull();
    expect(screen.getByText("Step one")).toBeTruthy();
    expect(screen.getByText("Step two")).toBeTruthy();
  });
});
