import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { OrgChartSeat } from "../core/types";
import { RolesChart } from "./roles-chart";

const seat = (id: string, name: string, partial: Partial<OrgChartSeat> = {}): OrgChartSeat => ({ id, name, workspace_id: "workspace-1", responsibilities: [], owners: [], vacant: true, position: 0, ...partial });

const member = (id: string, name: string) => ({ type: "member" as const, id, name });
const agent = (id: string, name: string) => ({ type: "agent" as const, id, name });

describe("RolesChart", () => {
  it("renders stable parent-child semantics and distinct owner states", () => {
    render(<RolesChart onEdit={vi.fn()} seats={[
      seat("lead", "Leadership", { owners: [member("m1", "Ava")], vacant: false }),
      seat("ops", "Operations", { parent_id: "lead", owners: [agent("a1", "Atlas")], vacant: false, position: 1 }),
      seat("open", "Commercial", { parent_id: "lead", position: 2 }),
    ]} />);
    expect(screen.getByRole("treeitem", { name: /Leadership, held by Ava/ })).toHaveAttribute("aria-level", "1");
    expect(screen.getByRole("treeitem", { name: /Operations, held by Atlas/ })).toHaveAttribute("aria-level", "2");
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
      seats={[seat("lead", "Leadership", { owners: [member("m1", "Ava")], vacant: false })]} />);
    expect(screen.getByRole("img", { name: "Ava" })).toHaveAttribute("src", "https://pics/ava.png");
  });

  it("offers above, beside, and below as small plus buttons without a popover first", () => {
    const onInsert = vi.fn();
    render(<RolesChart onEdit={vi.fn()} onInsert={onInsert} seats={[seat("lead", "Leadership")]} />);
    // No "Add role" toggle to open first — the three plusses sit on the lines.
    expect(screen.queryByRole("button", { name: /Add a role near/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add role above Leadership" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add role beside Leadership" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Add role below Leadership" }));
    expect(onInsert).toHaveBeenCalledWith("lead", "below");
  });

  // FIR-3589 items 10 and 12: one column width for every card so a level reads
  // as a row, but the height follows the content instead of a tall fixed box —
  // and no accountability is hidden behind a "+N more".
  it("gives every card one width, a content height, and no truncated accountabilities", () => {
    const { container } = render(<RolesChart onEdit={vi.fn()} seats={[
      seat("lead", "Leadership", { owners: [member("m1", "Ava")], vacant: false, responsibilities: ["One", "Two", "Three", "Four", "Five", "Six"] }),
      seat("ops", "Operations", { parent_id: "lead", position: 1 }),
    ]} />);
    const cards = [...container.querySelectorAll("article")];
    expect(cards).toHaveLength(2);
    for (const card of cards) {
      expect(card.className).toContain("w-56");
      expect(card.className).not.toContain("h-60");
    }
    for (const responsibility of ["One", "Two", "Three", "Four", "Five", "Six"]) {
      expect(screen.getByText(responsibility)).toBeInTheDocument();
    }
    expect(screen.queryByText(/\+\d+ more/)).not.toBeInTheDocument();
  });

  // FIR-3589 item 9: several people or agents can hold one role.
  it("lists every holder of a shared role", () => {
    render(<RolesChart onEdit={vi.fn()} seats={[
      seat("web", "Web Development", { owners: [member("m1", "Junaid"), member("m2", "Alipio"), agent("a1", "Atlas")], vacant: false }),
    ]} />);
    for (const name of ["Junaid", "Alipio", "Atlas"]) {
      expect(screen.getByText(name)).toBeInTheDocument();
    }
    expect(screen.getByRole("treeitem", { name: /held by Junaid, Alipio, Atlas/ })).toBeInTheDocument();
  });

  it("labels the name and accountability blocks on each card", () => {
    render(<RolesChart onEdit={vi.fn()} seats={[
      seat("lead", "Leadership", { owners: [member("m1", "Ava")], vacant: false, responsibilities: ["Vision"] }),
    ]} />);
    expect(screen.getByText("Name(s)")).toBeInTheDocument();
    expect(screen.getByText("Accountabilities")).toBeInTheDocument();
    expect(screen.getByText("Vision")).toBeInTheDocument();
  });

  it("collapses a parent so its reports disappear until expanded again", () => {
    render(<RolesChart onEdit={vi.fn()} seats={[
      seat("lead", "Leadership"),
      seat("ops", "Operations", { parent_id: "lead", position: 1 }),
    ]} />);
    expect(screen.getByRole("heading", { name: "Operations" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Collapse Leadership" }));
    expect(screen.queryByRole("heading", { name: "Operations" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Expand Leadership" }));
    expect(screen.getByRole("heading", { name: "Operations" })).toBeInTheDocument();
  });

  it("edits from anywhere on the card, not only a small pencil", () => {
    const onEdit = vi.fn();
    render(<RolesChart onEdit={onEdit} seats={[seat("lead", "Leadership")]} />);
    fireEvent.click(screen.getByRole("button", { name: "Edit Leadership" }));
    expect(onEdit).toHaveBeenCalledWith("lead");
  });

  // FIR-3589 item 8: a hover-only reveal is unreachable on a touch screen, so
  // the + stays visible and only brightens on hover.
  it("keeps the + buttons visible without a hover", () => {
    render(<RolesChart onEdit={vi.fn()} onInsert={vi.fn()} seats={[seat("lead", "Leadership")]} />);
    const add = screen.getByRole("button", { name: "Add role above Leadership" });
    expect(add.className).not.toContain("opacity-0");
    expect(add.className).toContain("opacity-60");
    expect(add.className).toContain("group-hover:opacity-100");
  });

  it("zooms the canvas in and out from the zoom rail", () => {
    const { container } = render(<RolesChart onEdit={vi.fn()} seats={[seat("lead", "Leadership")]} />);
    const canvas = container.querySelector<HTMLElement>("[aria-label='Role chart canvas'] > div");
    expect(canvas?.style.transform).toBe("scale(1)");
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    expect(canvas?.style.transform).toBe("scale(1.15)");
    fireEvent.click(screen.getByRole("button", { name: "Reset zoom" }));
    expect(canvas?.style.transform).toBe("scale(1)");
    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    expect(canvas?.style.transform).toBe("scale(0.85)");
  });

  it("gives the canvas a scrollable, pannable viewport", () => {
    render(<RolesChart onEdit={vi.fn()} seats={[seat("lead", "Leadership")]} />);
    const viewport = screen.getByLabelText("Role chart canvas");
    expect(viewport.className).toContain("overflow-auto");
    expect(viewport.className).toContain("cursor-grab");
  });
});
