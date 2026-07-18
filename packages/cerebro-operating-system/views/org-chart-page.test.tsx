import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { OrgChartSeat } from "../core/types";
import { OrgChartPage } from "./org-chart-page";

const state = vi.hoisted(() => ({
  seats: [] as OrgChartSeat[],
  members: [{ id: "member-1", name: "Jesper" }],
  agents: [{ id: "agent-1", name: "Sara" }],
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

  it("renders reports nested under their parent seat", () => {
    state.seats = [
      seat({ id: "root", name: "CEO" }),
      seat({ id: "child", name: "Operations", parent_id: "root", position: 0 }),
    ];

    render(<OrgChartPage />);

    expect(screen.getByRole("heading", { name: "CEO" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Operations" })).toBeInTheDocument();
    expect(screen.getByText("1 direct report")).toBeInTheDocument();
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
});
