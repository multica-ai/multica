// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { WorkspaceTable } from "./workspace-table";

describe("WorkspaceTable", () => {
  it("renders the 'No workspaces found' empty state when items is empty", () => {
    render(
      <WorkspaceTable
        items={[]}
        sort="activity"
        direction="desc"
        onSortChange={vi.fn()}
        onRowClick={vi.fn()}
      />,
    );
    expect(screen.getByText("No workspaces found")).toBeInTheDocument();
  });

  it("renders the filtered empty state and wires the clear-filters callback (plan §5.3)", async () => {
    const onClearFilters = vi.fn();
    render(
      <WorkspaceTable
        items={[]}
        sort="activity"
        direction="desc"
        onSortChange={vi.fn()}
        onRowClick={vi.fn()}
        hasActiveFilters
        onClearFilters={onClearFilters}
      />,
    );
    expect(screen.getByText("No workspaces match your filters")).toBeInTheDocument();
    await userEvent.click(screen.getByText("Clear filters"));
    expect(onClearFilters).toHaveBeenCalledOnce();
  });

  it("renders 'Not linked' for a workspace with no LiteLLM key and no invented value", () => {
    render(
      <WorkspaceTable
        items={[
          {
            id: "1",
            name: "Acme",
            slug: "acme",
            owner: "Jane",
            model: "claude-opus-5",
            llmKey: null,
            team: null,
            keySpend: null,
            status: "active",
            openIssues: 2,
            lastActivity: null,
          },
        ]}
        sort="activity"
        direction="desc"
        onSortChange={vi.fn()}
        onRowClick={vi.fn()}
      />,
    );
    expect(screen.getByText("Not linked")).toBeInTheDocument();
    expect(screen.getByText("Never")).toBeInTheDocument();
  });

  it("renders the Cost column from keySpend", () => {
    render(
      <WorkspaceTable
        items={[
          {
            id: "1",
            name: "Acme",
            slug: "acme",
            owner: "Jane",
            model: "claude-opus-5",
            llmKey: "agentfarm-acme",
            team: "Platform",
            keySpend: 12.5,
            status: "active",
            openIssues: 2,
            lastActivity: null,
          },
        ]}
        sort="activity"
        direction="desc"
        onSortChange={vi.fn()}
        onRowClick={vi.fn()}
      />,
    );
    expect(screen.getByText("$12.50")).toBeInTheDocument();
  });
});
