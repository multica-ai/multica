import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";

// The provider reads the active workspace from this hook.
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { CerebroStatusModelProvider } from "@multica/cerebro-status-models/views";
import { cerebroStatusModelsKeys } from "@multica/cerebro-status-models/core";
import { useCerebroFeatureFlagsStore } from "@multica/cerebro-feature-flags";
import { CerebroIssueDetailStatusSubmenu } from "./cerebro-issue-detail-status-submenu";

const WS_ID = "ws-1";

const baseIssue: Issue = {
  id: "issue-1",
  workspace_id: WS_ID,
  number: 1,
  identifier: "TES-1",
  title: "Example",
  description: null,
  status: "in_progress",
  priority: "medium",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  start_date: null,
  due_date: null,
  project_id: "p-1",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} as Issue;

function seededClient() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  // Project p-1 is assigned model m-1, which renames the in_progress base
  // status to the custom status "Building" — the same fixture shape the
  // issue-detail status-picker suite uses.
  qc.setQueryData(cerebroStatusModelsKeys.assignments(WS_ID), {
    assignments: [
      {
        project_id: "p-1",
        workspace_id: WS_ID,
        status_model_id: "m-1",
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      },
    ],
  });
  qc.setQueryData(cerebroStatusModelsKeys.list(WS_ID), {
    status_models: [
      {
        id: "m-1",
        workspace_id: WS_ID,
        name: "Plan-first pipeline",
        statuses: [
          { key: "building", label: "Building", color: "#facc15", base_status: "in_progress", position: 0 },
        ],
        project_count: 1,
        created_by_id: "user-1",
        created_by_type: "member",
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      },
    ],
  });
  return qc;
}

function renderSubmenu(qc: QueryClient, onUpdate: (u: unknown) => void) {
  return render(
    <QueryClientProvider client={qc}>
      <CerebroStatusModelProvider projectId="p-1">
        <DropdownMenu>
          <DropdownMenuTrigger render={<button data-testid="trigger">Menu</button>} />
          <DropdownMenuContent>
            <CerebroIssueDetailStatusSubmenu issue={baseIssue} onUpdate={onUpdate as never} />
          </DropdownMenuContent>
        </DropdownMenu>
      </CerebroStatusModelProvider>
    </QueryClientProvider>,
  );
}

describe("CerebroIssueDetailStatusSubmenu", () => {
  beforeEach(() => {
    useCerebroFeatureFlagsStore.getState().setFlag("cerebro_workflows", true);
  });
  afterEach(() => {
    useCerebroFeatureFlagsStore.getState().setFlag("cerebro_workflows", undefined);
  });

  it("renders the project model's custom status instead of the base label", async () => {
    const onUpdate = vi.fn();
    renderSubmenu(seededClient(), onUpdate);

    fireEvent.click(screen.getByTestId("trigger"));
    fireEvent.click(await screen.findByText("Status"));

    // The covered base status (in_progress) is replaced by the model row.
    const items = await screen.findAllByRole("menuitem");
    const labels = items.map((el) => el.textContent);
    expect(labels.some((l) => l?.includes("Building"))).toBe(true);
    expect(labels.some((l) => l === "In Progress")).toBe(false);
  });

  it("pins the custom_status_key when a custom row is picked", async () => {
    const onUpdate = vi.fn();
    renderSubmenu(seededClient(), onUpdate);

    fireEvent.click(screen.getByTestId("trigger"));
    fireEvent.click(await screen.findByText("Status"));
    fireEvent.click(await screen.findByText("Building"));

    expect(onUpdate).toHaveBeenCalledWith({
      status: "in_progress",
      custom_status_key: "building",
    });
  });

  it("falls back to the base statuses when the project has no model", async () => {
    const onUpdate = vi.fn();
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    });
    qc.setQueryData(cerebroStatusModelsKeys.assignments(WS_ID), { assignments: [] });
    qc.setQueryData(cerebroStatusModelsKeys.list(WS_ID), { status_models: [] });
    renderSubmenu(qc, onUpdate);

    fireEvent.click(screen.getByTestId("trigger"));
    fireEvent.click(await screen.findByText("Status"));

    expect(await screen.findByText("In Progress")).toBeInTheDocument();
    expect(screen.queryByText("Building")).not.toBeInTheDocument();
  });
});
