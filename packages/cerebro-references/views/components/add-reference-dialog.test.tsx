import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { referenceKeys } from "@multica/cerebro-references/core";

// Run the REAL useAddReference so we exercise both the UPSERT API call and
// the cache invalidation on settle — only the API client and the workspace
// id are stubbed.
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const createReference = vi.fn();
vi.mock("@multica/core/api", () => ({
  api: {
    createCerebroIssueReference: (...args: unknown[]) => createReference(...args),
  },
}));

import { AddReferenceDialog } from "./add-reference-dialog";

function renderDialog(qc: QueryClient) {
  return render(
    <QueryClientProvider client={qc}>
      <AddReferenceDialog issueId="issue-1" open onOpenChange={() => {}} />
    </QueryClientProvider>,
  );
}

describe("AddReferenceDialog", () => {
  beforeEach(() => {
    createReference.mockReset();
    createReference.mockResolvedValue({
      id: "ref-new",
      issue_id: "issue-1",
      object: "github_pr",
      ref_id: "firtal-group/firtal-cerebro#42",
    });
  });

  it("UPSERTs a parsed github_pr reference and invalidates the by-issue cache", async () => {
    const qc = new QueryClient();
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");
    const user = userEvent.setup();
    renderDialog(qc);

    await user.type(
      screen.getByLabelText("Reference"),
      "firtal-group/firtal-cerebro#42",
    );
    await user.click(screen.getByRole("button", { name: /add reference/i }));

    // UPSERT — posts the normalized identifier + resolved URL for the issue.
    await waitFor(() => {
      expect(createReference).toHaveBeenCalledWith("issue-1", {
        object: "github_pr",
        ref_id: "firtal-group/firtal-cerebro#42",
        url: "https://github.com/firtal-group/firtal-cerebro/pull/42",
        label: "firtal-group/firtal-cerebro#42",
      });
    });

    // Invalidate — the by-issue list refetches after the mutation settles.
    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: referenceKeys.byIssue("ws-1", "issue-1"),
      });
    });
  });

  it("rejects an unparseable github_pr identifier without calling the API", async () => {
    const qc = new QueryClient();
    const user = userEvent.setup();
    renderDialog(qc);

    await user.type(screen.getByLabelText("Reference"), "not-a-pr");
    await user.click(screen.getByRole("button", { name: /add reference/i }));

    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(createReference).not.toHaveBeenCalled();
  });
});
