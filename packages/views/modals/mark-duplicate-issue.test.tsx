import { useState } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, onTestFinished, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import { issueKeys } from "@multica/core/issues/queries";
import { createQueryClient } from "@multica/core/query-client";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { MarkDuplicateIssueModal } from "./mark-duplicate-issue";

const mocks = vi.hoisted(() => ({
  updateIssue: vi.fn(),
  getIssue: vi.fn(),
  listIssueDuplicates: vi.fn(),
  searchIssues: vi.fn(),
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("sonner", () => ({ toast: mocks.toast }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/api")>(),
  api: mocks,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog(undefined),
}));

const source: Issue = {
  id: "source", workspace_id: "ws-1", identifier: "MUL-1", title: "Source issue",
  status: "todo", revision: 1, parent_issue_id: null, labels: [], properties: {},
  number: 1, description: null, priority: "none", assignee_type: null, assignee_id: null,
  creator_type: "member", creator_id: "user-1", project_id: null, position: 0,
  stage: null, start_date: null, due_date: null, metadata: {},
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
};
const target = { ...source, id: "target", identifier: "MUL-2", title: "Original issue" };
const original = { ...source, id: "original", identifier: "MUL-3", title: "Stable original" };

function deferred() {
  let resolve!: (issue: Issue) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<Issue>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function mountPicker() {
  const client = createQueryClient();
  onTestFinished(() => client.clear());
  client.setQueryData(issueKeys.detail("ws-1", source.id), source);
  function Host() {
    const [open, setOpen] = useState(true);
    return (
      <QueryClientProvider client={client}>
        <button onClick={() => setOpen(true)}>Reopen</button>
        {open && <MarkDuplicateIssueModal onClose={() => setOpen(false)} data={{ issueId: source.id }} />}
      </QueryClientProvider>
    );
  }
  renderWithI18n(<Host />);
  return client;
}

async function select(issue = target) {
  fireEvent.change(await screen.findByPlaceholderText("Search issues..."), {
    target: { value: issue.title },
  });
  fireEvent.click(await screen.findByText(issue.title));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getIssue.mockResolvedValue(source);
  mocks.listIssueDuplicates.mockResolvedValue({ duplicate_of: null, duplicates: [] });
  mocks.searchIssues.mockResolvedValue({ issues: [target, original], total: 2 });
});
afterEach(cleanup);

describe("duplicate marking after the picker unmounts", () => {
  it("reports success once after settlement and reconciles the source", async () => {
    const request = deferred();
    mocks.updateIssue.mockReturnValue(request.promise);
    const client = mountPicker();
    await select();
    expect(mocks.toast.success).not.toHaveBeenCalled();
    expect(mocks.toast.error).not.toHaveBeenCalled();
    expect(client.getQueryData<Issue>(issueKeys.detail("ws-1", source.id))?.status).toBe("cancelled");
    const saved = { ...source, status: "cancelled", revision: 2, duplicate_of: target };
    await act(async () => request.resolve(saved));
    await waitFor(() => expect(mocks.toast.success).toHaveBeenCalledExactlyOnceWith("Marked as a duplicate of MUL-2"));
    expect(mocks.toast.error).not.toHaveBeenCalled();
    expect(client.getQueryData<Issue>(issueKeys.detail("ws-1", source.id))?.duplicate_of?.id).toBe(target.id);
  });

  it.each([
    ["duplicate target", new ApiError("server conflict", 409, "Conflict", { code: "duplicate_target_is_duplicate" }), "MUL-2 is itself a duplicate. Pick its original instead."],
    ["source has duplicates", new ApiError("server conflict", 409, "Conflict", { code: "issue_has_duplicates" }), "Other issues are marked as duplicates of this one. Remove those marks first."],
    ["unknown API code", new ApiError("Another server reason", 409, "Conflict", { code: "unknown_code" }), "Another server reason"],
    ["network failure", new TypeError("Failed to fetch"), "Failed to fetch"],
    ["non-Error failure", null, "Failed to mark as duplicate"],
  ])("reports %s once and rolls back", async (_name, error, message) => {
    const request = deferred();
    mocks.updateIssue.mockReturnValue(request.promise);
    const client = mountPicker();
    await select();
    await act(async () => request.reject(error));
    await waitFor(() => expect(mocks.toast.error).toHaveBeenCalledExactlyOnceWith(message));
    expect(mocks.toast.success).not.toHaveBeenCalled();
    expect(client.getQueryData<Issue>(issueKeys.detail("ws-1", source.id))?.status).toBe("todo");
  });

  it("keeps each selected identifier when a later picker operation finishes first", async () => {
    const first = deferred();
    const second = deferred();
    mocks.updateIssue.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    mountPicker();
    await select();
    fireEvent.click(screen.getByRole("button", { name: "Reopen" }));
    await select(original);
    await act(async () => second.resolve({ ...source, status: "cancelled", revision: 2, duplicate_of: original }));
    await waitFor(() => expect(mocks.toast.success).toHaveBeenCalledExactlyOnceWith("Marked as a duplicate of MUL-3"));
    await act(async () => first.reject(new ApiError("conflict", 409, "Conflict", { code: "duplicate_target_is_duplicate" })));
    await waitFor(() => expect(mocks.toast.error).toHaveBeenCalledExactlyOnceWith("MUL-2 is itself a duplicate. Pick its original instead."));
    expect(mocks.updateIssue).toHaveBeenNthCalledWith(1, source.id, { status: "cancelled", duplicate_of_issue_id: target.id });
    expect(mocks.updateIssue).toHaveBeenNthCalledWith(2, source.id, { status: "cancelled", duplicate_of_issue_id: original.id });
  });
});
