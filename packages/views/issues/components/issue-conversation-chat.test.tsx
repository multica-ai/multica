import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { renderWithI18n } from "../../test/i18n";
import { IssueConversationChat } from "./issue-conversation-chat";
import type { ConversationTimelineItem } from "./issue-conversation-timeline";

vi.mock("react-virtuoso", () => ({
  Virtuoso: ({ data, itemContent }: { data: ConversationTimelineItem[]; itemContent: (index: number, item: ConversationTimelineItem) => ReactNode }) =>
    <div>{data.map((item, index) => <div key={`${item.runId ?? ""}:${item.seq}:${item.divider ?? false}`}>{itemContent(index, item)}</div>)}</div>,
}));
vi.mock("../../rich-content", () => ({ RichContent: ({ content }: { content: string }) => <div>{content}</div> }));
vi.mock("../components/comment-card", () => ({ AttachmentList: () => null }));

afterEach(cleanup);

describe("issue conversation chat", () => {
  it("shows each run's produced summary beneath its full-chat divider", () => {
    renderWithI18n(<IssueConversationChat isLive={false} items={[
      { seq: 0, type: "text", divider: true, runId: "run-1", content: "Run 1" },
      { seq: 1, type: "text", runId: "run-1", content: "First reply" },
      { seq: 0, type: "text", divider: true, runId: "run-2", content: "Run 2" },
      { seq: 1, type: "text", runId: "run-2", content: "Second reply" },
    ]} runSummaries={{
      "run-1": { outcome: { paths: ["first.ts"], addedLines: 6, removedLines: 2, commandCount: 3 } },
      "run-2": { outcome: { paths: ["second.ts", "third.ts"], addedLines: 1, removedLines: 0, commandCount: 4 } },
    }} />);
    expect(screen.getByTitle("first.ts")).toHaveTextContent("+6");
    expect(screen.getByTitle("first.ts")).toHaveTextContent("−2");
    const secondFiles = screen.getByText("2 files").closest("[title]")!;
    expect(secondFiles).toHaveAttribute("title", "second.ts\nthird.ts");
    expect(secondFiles).toHaveTextContent("+1");
    expect(screen.getByText("Run 1").compareDocumentPosition(screen.getByTitle("first.ts")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.getByText("Run 2").compareDocumentPosition(secondFiles) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("shows ordered human/assistant text with thinking and tools collapsed even while live", () => {
    const { container } = renderWithI18n(<IssueConversationChat isLive items={[
      { seq: -1, humanId: "human", type: "text", content: "**Human interaction**\n\nmy question", humanContent: "my question" },
      { seq: 1, type: "thinking", content: "private detail" },
      { seq: 2, type: "tool_use", tool: "read", input: { path: "example.ts" } },
      { seq: 3, type: "tool_result", tool: "read", output: "file contents" },
      { seq: 4, type: "text", content: "my answer" },
    ]} />);
    expect(screen.getByText("my question").compareDocumentPosition(screen.getByText("my answer")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.queryByText(/Human interaction/)).not.toBeInTheDocument();
    // Collapsed rows retain a one-line preview, not the expanded body.
    expect(container.querySelector("pre")).toBeNull();
    const collapsed = screen.getAllByRole("button").filter((button) => button.getAttribute("aria-expanded") === "false");
    expect(collapsed).toHaveLength(3);
    fireEvent.click(collapsed[0]!);
    expect(container.querySelector("pre")).toHaveTextContent("private detail");
  });
});
