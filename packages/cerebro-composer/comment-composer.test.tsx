import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const submitFromBase = vi.fn();
vi.mock("./base-composer", () => ({
  BaseComposer: (props: { onSubmit: (content: string) => Promise<void>; sendMenu?: React.ReactNode }) => (
    <div>
      {props.sendMenu}
      <button type="button" onClick={() => props.onSubmit("Build it")}>Send test comment</button>
    </div>
  ),
}));
vi.mock("@multica/cerebro-comment-drafts", () => ({ useCommentDraft: () => ({}) }));
vi.mock("@multica/cerebro-access/views", () => ({
  usePrivateAgentSendConfirm: () => ({ confirmBeforeSend: undefined, confirmDialog: null }),
}));
vi.mock("@multica/views/issues/components", () => ({
  TriggerTargetBar: () => null,
  memberMentionMarkdown: () => "",
}));
vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => true }));

import { CommentComposer } from "./comment-composer";

afterEach(() => {
  cleanup();
  submitFromBase.mockReset();
});

describe("CommentComposer new-thread mode", () => {
  it("shows Mode before the first/new thread and submits Build by default", async () => {
    render(<CommentComposer issueId="issue-1" onSubmit={submitFromBase} />);

    expect(screen.getByRole("combobox", { name: "New thread mode" })).toHaveTextContent("Build");
    await userEvent.click(screen.getByRole("button", { name: "Send test comment" }));

    expect(submitFromBase).toHaveBeenCalledWith("Build it", undefined, "build");
  });

  it("submits the selected Plan mode and hides the selector for replies", async () => {
    const { rerender } = render(<CommentComposer issueId="issue-1" onSubmit={submitFromBase} />);
    await userEvent.click(screen.getByRole("combobox", { name: "New thread mode" }));
    await userEvent.click(await screen.findByRole("option", { name: "Plan" }));
    await userEvent.click(screen.getByRole("button", { name: "Send test comment" }));
    expect(submitFromBase).toHaveBeenLastCalledWith("Build it", undefined, "plan");

    rerender(<CommentComposer issueId="issue-1" variant="reply" onSubmit={submitFromBase} />);
    expect(screen.queryByRole("combobox", { name: "New thread mode" })).not.toBeInTheDocument();
  });
});
