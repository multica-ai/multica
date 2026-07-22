import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { OrgChartSeat } from "../core/types";
import { OrgChartPage } from "./org-chart-page";

const state = vi.hoisted(() => ({
  seats: [] as OrgChartSeat[],
  members: [{ id: "member-1", name: "Jesper" }] as { id: string; name: string; avatar_url?: string | null }[],
  agents: [{ id: "agent-1", name: "Sara" }] as { id: string; name: string; avatar_url?: string | null }[],
  loading: false,
  error: false,
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: (options: { queryKey: readonly string[] }) => {
      if (options.queryKey.includes("members")) return { data: state.members };
      if (options.queryKey.includes("agents")) return { data: state.agents };
      if (options.queryKey.includes("org-chart")) return { data: { seats: state.seats }, isLoading: state.loading, isError: state.error };
      return { data: undefined };
    },
  };
});

vi.mock("../core/queries", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../core/queries")>();
  return {
    ...actual,
    orgChartOptions: () => ({ queryKey: ["cerebro", "operating-system", "workspace-1", "org-chart"] }),
    useCreateOrgChartSeat: () => ({ mutate: state.create, isPending: false, isError: false }),
    useUpdateOrgChartSeat: () => ({ mutate: state.update, isPending: false, isError: false }),
    useDeleteOrgChartSeat: () => ({ mutate: state.remove, isPending: false, isError: false }),
  };
});

const seat = (partial: Partial<OrgChartSeat>): OrgChartSeat => ({
  id: "seat-1", workspace_id: "workspace-1", name: "Operations", responsibilities: [],
  vacant: true, position: 0, ...partial,
});

describe("OrgChartPage", () => {
  beforeEach(() => {
    state.seats = [seat({})];
    state.loading = false;
    state.error = false;
    state.create.mockReset();
    state.update.mockReset();
    state.remove.mockReset();
  });

  it("shows an owner by name, not an ID", () => {
    state.seats = [seat({ owner_type: "member", owner_id: "member-1", owner_name: "Jesper", vacant: false })];

    render(<OrgChartPage />);

    expect(screen.getByText("Jesper")).toBeInTheDocument();
  });

  it("marks an unowned seat as vacant", () => {
    render(<OrgChartPage />);
    expect(screen.getByText("Vacant")).toBeInTheDocument();
  });

  it("never asks for a UUID or an owner type when editing", () => {
    render(<OrgChartPage />);
    fireEvent.click(screen.getByRole("button", { name: "Edit Operations" }));

    // The owner is chosen from a searchable name list.
    expect(screen.getByRole("button", { name: "Owner" })).toBeInTheDocument();
    // The old agent-oriented fields must be gone.
    expect(screen.queryByLabelText("Owner ID")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Owner type" })).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText(/UUID/i)).not.toBeInTheDocument();
  });

  it("edits responsibilities as individual rows, not a text blob", () => {
    state.seats = [seat({ responsibilities: ["Own the weekly plan", "Report on cost"] })];

    render(<OrgChartPage />);
    fireEvent.click(screen.getByRole("button", { name: "Edit Operations" }));

    expect(screen.getByLabelText("Responsibility 1")).toHaveValue("Own the weekly plan");
    expect(screen.getByLabelText("Responsibility 2")).toHaveValue("Report on cost");
    expect(screen.queryByRole("textbox", { name: "Responsibilities" })).not.toBeInTheDocument();
  });

  it("adds a responsibility row", () => {
    render(<OrgChartPage />);
    fireEvent.click(screen.getByRole("button", { name: "Edit Operations" }));
    fireEvent.click(screen.getByRole("button", { name: "+ Add responsibility" }));

    expect(screen.getByLabelText("Responsibility 1")).toBeInTheDocument();
  });

  it("puts each level on its own row under its parent", () => {
    state.seats = [
      seat({ id: "root", name: "CEO" }),
      seat({ id: "child", name: "Operations", parent_id: "root", position: 0 }),
    ];

    render(<OrgChartPage />);

    expect(screen.getByRole("treeitem", { name: /^CEO,/ })).toHaveAttribute("aria-level", "1");
    expect(screen.getByRole("treeitem", { name: /^Operations,/ })).toHaveAttribute("aria-level", "2");
  });

  it("creates a seat from the add form", () => {
    render(<OrgChartPage />);
    fireEvent.click(screen.getByRole("button", { name: "+ Add seat" }));
    fireEvent.change(screen.getByLabelText("Seat name"), { target: { value: "Finance" } });
    fireEvent.click(screen.getByRole("button", { name: "Save seat" }));

    expect(state.create).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Finance", responsibilities: [] }),
      expect.any(Object),
    );
  });

  it("shows the owner's avatar picture when the member has one", () => {
    state.members = [{ id: "member-1", name: "Jesper", avatar_url: "https://pics/jesper.png" }];
    state.seats = [seat({ owner_type: "member", owner_id: "member-1", owner_name: "Jesper", vacant: false })];

    render(<OrgChartPage />);

    expect(screen.getByRole("img", { name: "Jesper" })).toHaveAttribute("src", "https://pics/jesper.png");
    state.members = [{ id: "member-1", name: "Jesper" }];
  });

  it("inserts a report below a seat with the parent preset", () => {
    render(<OrgChartPage />);
    fireEvent.click(screen.getByRole("button", { name: "Add role below Operations" }));
    fireEvent.change(screen.getByLabelText("Seat name"), { target: { value: "Finance" } });
    fireEvent.click(screen.getByRole("button", { name: "Save seat" }));

    expect(state.create).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Finance", parent_id: "seat-1" }),
      expect.any(Object),
    );
  });

  it("re-parents the target under a new seat inserted above it", () => {
    state.create.mockImplementation((_input: { name: string }, opts?: { onSuccess?: (created: { id: string }) => void }) => opts?.onSuccess?.({ id: "new-seat" }));

    render(<OrgChartPage />);
    fireEvent.click(screen.getByRole("button", { name: "Add role above Operations" }));
    fireEvent.change(screen.getByLabelText("Seat name"), { target: { value: "Leadership" } });
    fireEvent.click(screen.getByRole("button", { name: "Save seat" }));

    expect(state.update).toHaveBeenCalledWith(
      expect.objectContaining({ id: "seat-1", input: expect.objectContaining({ parent_id: "new-seat" }) }),
    );
  });

  it("opens the editor as a modal on desktop and a bottom sheet on mobile", () => {
    render(<OrgChartPage />);
    expect(document.querySelector("[data-slot='dialog-content']")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Edit Operations" }));

    const dialog = document.querySelector("[data-slot='dialog-content']");
    expect(dialog).not.toBeNull();
    expect(screen.getByLabelText("Seat name")).toBeInTheDocument();
    // Anchored to the bottom edge on a phone, centred from `sm` up.
    expect(dialog?.className).toContain("bottom-0");
    expect(dialog?.className).toContain("sm:top-1/2");
  });

  it("lists people and the roles they hold on a second tab", () => {
    state.seats = [
      seat({ id: "seat-1", name: "Operations", owner_type: "member", owner_id: "member-1", owner_name: "Jesper", vacant: false }),
      seat({ id: "seat-2", name: "Marketing" }),
    ];

    render(<OrgChartPage />);
    fireEvent.click(screen.getByRole("tab", { name: "People" }));

    expect(screen.getByRole("columnheader", { name: "Roles" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "Operations" })).toBeInTheDocument();
    // An agent with no seat is still listed, rather than silently dropped.
    expect(screen.getByText("Sara")).toBeInTheDocument();
    expect(screen.getAllByText("No role yet").length).toBeGreaterThan(0);
    // A seat nobody owns shows up as vacant instead of disappearing.
    expect(screen.getByRole("region", { name: "Vacant roles" })).toHaveTextContent("Marketing");
  });
});
