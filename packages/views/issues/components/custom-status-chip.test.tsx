import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import type { IssueStatusEntry } from "@multica/core/types";
import { CustomStatusChip } from "./custom-status-chip";

let catalogEntries: IssueStatusEntry[] | undefined;

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog(catalogEntries),
}));

function entry(overrides: Partial<IssueStatusEntry>): IssueStatusEntry {
  return {
    id: overrides.key ?? "id",
    workspace_id: "workspace-1",
    key: "custom",
    name: "Custom",
    description: "",
    category: "in_review",
    color: "#ff0000",
    is_system: false,
    position: 1,
    archived_at: null,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

const IN_REVIEW_BUILT_IN = entry({
  id: "in_review",
  key: "in_review",
  name: "In Review",
  category: "in_review",
  is_system: true,
  position: 0,
});

describe("CustomStatusChip", () => {
  // The column header already says "In Review". Repeating it on every card
  // would add a chip to every issue in a workspace that never customized
  // anything — the change has to be invisible to those workspaces.
  it("renders nothing for a built-in status", () => {
    catalogEntries = [IN_REVIEW_BUILT_IN];
    const { container } = render(<CustomStatusChip status="in_review" />);
    expect(container).toBeEmptyDOMElement();
  });

  // Board columns and list sections are categories, so this chip is the only
  // thing distinguishing two statuses that share one column.
  it("names a custom status", () => {
    catalogEntries = [IN_REVIEW_BUILT_IN, entry({ key: "qa", name: "QA Review" })];
    render(<CustomStatusChip status="qa" />);
    expect(screen.getByText("QA Review")).toBeInTheDocument();
  });

  // An issue can carry a status this client has not fetched yet. Rendering a
  // chip from a guessed category would be worse than rendering none.
  it("renders nothing while the catalog is still loading", () => {
    catalogEntries = undefined;
    const { container } = render(<CustomStatusChip status="qa" />);
    expect(container).toBeEmptyDOMElement();
  });

  // Archiving retires a status from new assignment; issues already on it keep
  // it, and those issues still need it named.
  it("still names an archived custom status", () => {
    catalogEntries = [
      IN_REVIEW_BUILT_IN,
      entry({ key: "qa", name: "QA Review", archived_at: "2026-01-01T00:00:00Z" }),
    ];
    render(<CustomStatusChip status="qa" />);
    expect(screen.getByText("QA Review")).toBeInTheDocument();
  });
});
