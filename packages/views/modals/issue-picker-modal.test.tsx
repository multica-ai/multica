import { act } from "react";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { IssuePickerModal } from "./issue-picker-modal";
import { renderWithI18n } from "../test/i18n";

const { mockSearchIssues } = vi.hoisted(() => ({
  mockSearchIssues: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: { searchIssues: mockSearchIssues },
}));

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

describe("IssuePickerModal search lifecycle", () => {
  beforeEach(() => {
    mockSearchIssues.mockReset().mockResolvedValue({ issues: [] });
    Element.prototype.scrollIntoView = vi.fn();
  });

  it("cancels an in-flight search and discards its results when the modal closes", async () => {
    const user = userEvent.setup();
    const pendingSearch = deferred<{
      issues: Array<{
        id: string;
        identifier: string;
        title: string;
        status: "todo";
      }>;
    }>();
    mockSearchIssues.mockReturnValueOnce(pendingSearch.promise);
    const props = {
      onOpenChange: vi.fn(),
      title: "Select issue",
      description: "Choose an issue",
      excludeIds: [],
      onSelect: vi.fn(),
    };
    const { rerender } = renderWithI18n(<IssuePickerModal {...props} open />);

    const input = screen.getByPlaceholderText("Search issues...");
    await user.type(input, "stale");
    await waitFor(() => expect(mockSearchIssues).toHaveBeenCalledTimes(1), {
      timeout: 2000,
    });
    const signal = mockSearchIssues.mock.calls[0]?.[0]?.signal as AbortSignal;

    rerender(<IssuePickerModal {...props} open={false} />);
    await act(async () => {
      pendingSearch.resolve({
        issues: [
          {
            id: "stale-issue",
            identifier: "MUL-404",
            title: "Stale picker result",
            status: "todo",
          },
        ],
      });
      await pendingSearch.promise;
    });

    rerender(<IssuePickerModal {...props} open />);
    await screen.findByPlaceholderText("Search issues...");

    expect(signal.aborted).toBe(true);
    expect(screen.queryByText("Stale picker result")).not.toBeInTheDocument();
  });

  it("clears previous results immediately and keeps them cleared when the next search fails", async () => {
    const user = userEvent.setup();
    mockSearchIssues
      .mockResolvedValueOnce({
        issues: [
          {
            id: "first-issue",
            identifier: "MUL-1",
            title: "First picker result",
            status: "todo",
          },
        ],
      })
      .mockRejectedValueOnce(new Error("search failed"));
    renderWithI18n(
      <IssuePickerModal
        open
        onOpenChange={vi.fn()}
        title="Select issue"
        description="Choose an issue"
        excludeIds={[]}
        onSelect={vi.fn()}
      />,
    );

    const input = screen.getByPlaceholderText("Search issues...");
    await user.type(input, "first");
    await screen.findByText("First picker result", undefined, { timeout: 2000 });

    await user.type(input, "x");
    expect(screen.queryByText("First picker result")).not.toBeInTheDocument();
    await waitFor(() => expect(mockSearchIssues).toHaveBeenCalledTimes(2), {
      timeout: 2000,
    });

    await waitFor(() => {
      expect(screen.queryByText("First picker result")).not.toBeInTheDocument();
    });
  });
});
