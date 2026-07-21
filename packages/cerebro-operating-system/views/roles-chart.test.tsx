import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { OrgChartSeat } from "../core/types";
import { RolesChart } from "./roles-chart";

const seat = (id: string, name: string, partial: Partial<OrgChartSeat> = {}): OrgChartSeat => ({ id, name, workspace_id: "workspace-1", responsibilities: [], vacant: true, position: 0, ...partial });

describe("RolesChart", () => {
  it("renders stable parent-child semantics and distinct owner states", () => {
    render(<RolesChart onEdit={vi.fn()} seats={[
      seat("lead", "Leadership", { owner_type: "member", owner_id: "m1", owner_name: "Ava", vacant: false }),
      seat("ops", "Operations", { parent_id: "lead", owner_type: "agent", owner_id: "a1", owner_name: "Atlas", vacant: false, position: 1 }),
      seat("open", "Commercial", { parent_id: "lead", position: 2 }),
    ]} />);
    expect(screen.getByRole("treeitem", { name: /Leadership, Owner Ava/ })).toHaveAttribute("aria-level", "1");
    expect(screen.getByRole("treeitem", { name: /Operations, Agent Atlas/ })).toHaveAttribute("aria-level", "2");
    expect(screen.getByRole("treeitem", { name: /Commercial, Vacant/ })).toBeInTheDocument();
  });

  it("surfaces orphaned and cyclic roles instead of hiding them", () => {
    render(<RolesChart onEdit={vi.fn()} seats={[seat("orphan", "Orphan", { parent_id: "missing" }), seat("a", "A", { parent_id: "b" }), seat("b", "B", { parent_id: "a" })]} />);
    expect(screen.getByText("Unassigned roles")).toBeInTheDocument();
    expect(screen.getByText("Orphan")).toBeInTheDocument();
    expect(screen.getAllByText("Reporting cycle needs correction.").length).toBeGreaterThan(0);
  });

  it("shows the owner's picture when the actor resolves an avatar url", () => {
    render(<RolesChart onEdit={vi.fn()}
      resolveActor={() => ({ name: "Ava", initials: "AV", avatarUrl: "https://pics/ava.png" })}
      seats={[seat("lead", "Leadership", { owner_type: "member", owner_id: "m1", owner_name: "Ava", vacant: false })]} />);
    expect(screen.getByRole("img", { name: "Ava" })).toHaveAttribute("src", "https://pics/ava.png");
  });

  it("offers above, beside, and below inserts and reports the chosen direction", () => {
    const onInsert = vi.fn();
    render(<RolesChart onEdit={vi.fn()} onInsert={onInsert} seats={[seat("lead", "Leadership")]} />);
    fireEvent.click(screen.getByRole("button", { name: "Add a role near Leadership" }));
    expect(screen.getByRole("button", { name: "Add role above Leadership" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add role beside Leadership" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Add role below Leadership" }));
    expect(onInsert).toHaveBeenCalledWith("lead", "below");
  });
});
