import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { IssueReference } from "@multica/cerebro-references/core";

// Drive the list from mocked query/mutation hooks; keep the rest of core
// (renderer registry, github_pr renderer, parsing) real so rendering goes
// through the same path production does.
const mockUseIssueReferences = vi.fn();
const mockDeleteMutate = vi.fn();

vi.mock("@multica/cerebro-references/core", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@multica/cerebro-references/core")>();
  return {
    ...actual,
    useIssueReferences: (issueId: string) => mockUseIssueReferences(issueId),
    useDeleteReference: () => ({ mutate: mockDeleteMutate, isPending: false }),
    useAddReference: () => ({ mutate: vi.fn(), isPending: false }),
  };
});

import { IssueReferenceList } from "./reference-list";

function makeRef(overrides: Partial<IssueReference> = {}): IssueReference {
  return {
    id: "ref-1",
    issue_id: "issue-1",
    object: "github_pr",
    type: null,
    ref_id: "firtal-group/firtal-cerebro#42",
    label: null,
    url: null,
    metadata: {},
    created_by_type: "member",
    created_by_id: "user-1",
    created_at: "2026-05-19T00:00:00Z",
    updated_at: "2026-05-19T00:00:00Z",
    ...overrides,
  };
}

describe("IssueReferenceList", () => {
  beforeEach(() => {
    mockUseIssueReferences.mockReset();
    mockDeleteMutate.mockReset();
  });

  it("renders the empty state with 0 references", () => {
    mockUseIssueReferences.mockReturnValue({ data: [], isLoading: false });
    render(<IssueReferenceList issueId="issue-1" />);

    expect(screen.getByText(/Link a GitHub PR/i)).toBeInTheDocument();
    expect(screen.queryByTestId("reference-row")).not.toBeInTheDocument();
  });

  it("renders a single github_pr reference with title and state badge", () => {
    mockUseIssueReferences.mockReturnValue({
      data: [makeRef({ metadata: { state: "open" } })],
      isLoading: false,
    });
    render(<IssueReferenceList issueId="issue-1" />);

    const rows = screen.getAllByTestId("reference-row");
    expect(rows).toHaveLength(1);
    expect(
      screen.getByText("firtal-group/firtal-cerebro#42"),
    ).toBeInTheDocument();
    // github_pr renderer maps metadata.state "open" → "Open" badge.
    expect(screen.getByText("Open")).toBeInTheDocument();
  });

  it("renders N references grouped across known and unknown object types", () => {
    mockUseIssueReferences.mockReturnValue({
      data: [
        makeRef({ id: "a", ref_id: "firtal-group/firtal-cerebro#42" }),
        makeRef({ id: "b", ref_id: "firtal-group/firtal-cerebro#43" }),
        // Unknown object kind: no registered renderer → fallback renderer
        // produces the title from the label, proving new edge-types render
        // without frontend changes.
        makeRef({
          id: "c",
          object: "linear_issue",
          ref_id: "LIN-7",
          label: "Linear LIN-7",
        }),
      ],
      isLoading: false,
    });
    render(<IssueReferenceList issueId="issue-1" />);

    expect(screen.getAllByTestId("reference-row")).toHaveLength(3);
    expect(
      screen.getByText("firtal-group/firtal-cerebro#42"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("firtal-group/firtal-cerebro#43"),
    ).toBeInTheDocument();
    // Fallback renderer title for the unknown object.
    expect(screen.getByText("Linear LIN-7")).toBeInTheDocument();
    // Two distinct object groups rendered.
    const unknownRow = screen.getByText("Linear LIN-7").closest("[data-testid='reference-row']");
    expect(unknownRow).toHaveAttribute("data-object", "linear_issue");
  });
});
