// Regression test for FIR-1550: the editor must re-seed its form every time it
// opens. The original bug kept StatusModelEditor permanently mounted, so
// useState(model...) only ran once and "Edit" / "New model" opened with stale
// state from the previous session. Mounting the editor only while open fixes it.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { CerebroStatusModel } from "../core";

const ALPHA: CerebroStatusModel = {
  id: "model-alpha",
  workspace_id: "ws-1",
  name: "Alpha",
  description: "First model",
  statuses: [
    { key: "todo", label: "Plan", color: "#6366f1", base_status: "todo", position: 0 },
    { key: "in_progress", label: "I gang", color: "#f59e0b", base_status: "in_progress", position: 1 },
    { key: "done", label: "Færdig", color: "#10b981", base_status: "done", position: 2 },
  ],
  project_count: 0,
  workspace_default: false,
  created_by_id: "u-1",
  created_by_type: "member",
  created_at: "2026-05-27T00:00:00Z",
  updated_at: "2026-05-27T00:00:00Z",
};

const BETA: CerebroStatusModel = { ...ALPHA, id: "model-beta", name: "Beta", description: "Second model" };

vi.mock("@tanstack/react-query", () => ({
  // assignments key extends the status-models key, so test it first.
  useQuery: (opts: { queryKey: unknown[] }) => {
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("assignments")) return { data: { assignments: [] } };
    if (key.includes("status-models")) return { data: { status_models: [ALPHA, BETA] } };
    if (key.includes("projects")) return { data: [] };
    return { data: undefined };
  },
  useMutation: () => ({ mutate: vi.fn(), mutateAsync: vi.fn().mockResolvedValue({}), isPending: false }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: (wsId: string) => ({ queryKey: ["projects", wsId] }),
}));

import { StatusModelsTab } from "./status-models-tab";

function nameInput(): HTMLInputElement {
  return screen.getByLabelText("Name") as HTMLInputElement;
}

function editButtonFor(modelName: string): HTMLElement {
  const row = screen.getByText(modelName).closest("div.rounded-lg") as HTMLElement;
  return within(row).getByTitle("Edit model");
}

describe("StatusModelsTab editor state", () => {
  beforeEach(() => vi.clearAllMocks());

  it("opens an empty form for 'New model' after editing a model", async () => {
    render(<StatusModelsTab />);

    fireEvent.click(editButtonFor("Alpha"));
    expect((await screen.findByLabelText("Name")) as HTMLInputElement).toHaveValue("Alpha");

    fireEvent.click(screen.getByRole("button", { name: /Cancel/i }));
    await waitFor(() => expect(screen.queryByLabelText("Name")).toBeNull());

    fireEvent.click(screen.getByRole("button", { name: /New model/i }));
    expect(nameInput()).toHaveValue("");
  });

  it("re-seeds the form when switching from one model to another", async () => {
    render(<StatusModelsTab />);

    fireEvent.click(editButtonFor("Alpha"));
    expect(nameInput()).toHaveValue("Alpha");

    fireEvent.click(screen.getByRole("button", { name: /Cancel/i }));
    await waitFor(() => expect(screen.queryByLabelText("Name")).toBeNull());

    fireEvent.click(editButtonFor("Beta"));
    expect(nameInput()).toHaveValue("Beta");
  });
});
