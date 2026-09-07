import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ComponentProps } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Issue, IssueSourceContext, SourceContextCommentSnapshot } from "@multica/core/types";
import enIssues from "../../locales/en/issues.json";
import zhHansIssues from "../../locales/zh-Hans/issues.json";

const navigationPush = vi.hoisted(() => vi.fn());

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: navigationPush }),
  AppLink: ({ href, ...props }: ComponentProps<"a">) => <a href={href} {...props} />,
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

vi.mock("../../editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
}));

import { SourceContextBadge } from "./source-context-viewer";

const baseContext: IssueSourceContext = {
  id: "context-1",
  version: 1,
  usage: "read_only_historical_background",
  captured_at: "2026-08-21T12:00:00Z",
  display_state: "unchanged",
  source_issue_state: "unchanged",
  comment_thread_state: "unchanged",
  anchor_comment_state: "available",
  can_open_current_source: true,
  current_source: { issue_id: "source-issue", anchor_comment_id: "selected", identifier: "NEW-7" },
  source_author_state: [{
    type: "member", id: "member-1", captured_name: "Alice", current_name: "Fox", state: "renamed",
  }],
  snapshot: {
    version: 1,
    captured_by_user_id: "creator-1",
    captured_at: "2026-08-21T12:00:00Z",
    source_issue: {
      id: "source-issue", identifier: "MUL-7", number: 7, title: "Source title",
      description: "Source body", created_at: "2026-08-20T00:00:00Z",
      updated_at: "2026-08-21T00:00:00Z", revision: 2, attachments: [],
    },
    anchor_comment_id: "selected",
    comment_thread: [{
      id: "selected", parent_id: null, type: "comment", content: "Captured comment",
      author: { type: "member", id: "member-1", name: "Alice" },
      created_at: "2026-08-21T01:00:00Z", updated_at: "2026-08-21T01:00:00Z",
      revision: 1, attachments: [],
    }],
  },
};

function renderBadge(
  context: IssueSourceContext,
  refreshed = context,
  relation?: { parentIssue?: Issue; parentProgress?: { done: number; total: number } },
) {
  const refetchIssue = vi.fn().mockResolvedValue({ source_context: refreshed } as Issue);
  render(
    <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
      <SourceContextBadge
        context={context}
        refetchIssue={refetchIssue}
        parentIssue={relation?.parentIssue}
        parentProgress={relation?.parentProgress}
      />
    </I18nProvider>,
  );
  return refetchIssue;
}

beforeEach(() => {
  navigationPush.mockReset();
});

describe("SourceContextBadge", () => {
  it("groups the parent relation, progress, and snapshot action into one quiet summary", () => {
    const parentIssue = {
      id: "source-issue",
      identifier: "MUL-7",
      title: "Current source title",
      status: "in_progress",
    } as Issue;
    renderBadge(baseContext, baseContext, {
      parentIssue,
      parentProgress: { done: 1, total: 3 },
    });

    const summary = screen.getByText("Sub-issue of").closest('[data-slot="source-context-summary"]');
    expect(summary).toHaveClass("border-border/60", "bg-muted/30");
    expect(screen.getByRole("link", { name: /MUL-7 Current source title/ })).toHaveAttribute(
      "href",
      "/acme/issues/source-issue",
    );
    expect(screen.getByText("1/3")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Context snapshot" })).toBeInTheDocument();
    expect(screen.queryByText(/From MUL-7/)).not.toBeInTheDocument();
  });

  it("keeps the snapshot and live-source actions distinct", async () => {
    const refetch = renderBadge(baseContext);
    fireEvent.click(screen.getByRole("button", { name: "Context snapshot" }));

    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(refetch).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Go to source comment" }));
    await waitFor(() => expect(refetch).toHaveBeenCalledOnce());
    expect(navigationPush).toHaveBeenCalledWith("/acme/issues/source-issue#comment-selected");
  });

  it("keeps the snapshot open when live-source revalidation fails", async () => {
    const refetchIssue = vi.fn().mockRejectedValue(new Error("offline"));
    render(
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <SourceContextBadge context={baseContext} refetchIssue={refetchIssue} />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Context snapshot" }));
    fireEvent.click(screen.getByRole("button", { name: "Go to source comment" }));

    expect(await screen.findByRole("dialog")).toBeTruthy();
    expect(screen.getByText("Captured comment")).toBeTruthy();
    expect(navigationPush).not.toHaveBeenCalled();
  });

  it("supports expanding the viewer and keeps source navigation in the header", () => {
    renderBadge(baseContext);
    fireEvent.click(screen.getByRole("button", { name: "Context snapshot" }));

    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveClass("h-[min(82dvh,56rem)]", "sm:max-w-4xl");
    expect(dialog).not.toHaveClass("h-[calc(100dvh-2rem)]", "sm:max-w-[calc(100%-2rem)]");
    expect(screen.getByText("Source comment")).toHaveClass(
      "rounded",
      "bg-info/10",
      "text-info",
    );
    expect(screen.getByText("Alice at capture · now Fox")).toHaveClass(
      "truncate",
    );
    expect(screen.getByText("Captured comment").closest("div.mt-1\\.5")).toBeInTheDocument();
    expect(screen.getByText("Source body").closest("section")).toHaveClass("space-y-4");
    expect(screen.getByText("Captured comment").closest("ol")?.parentElement).toHaveClass("space-y-8");
    expect(screen.getByText("Captured comment").closest('[data-slot="source-context-content"]')).toBeInTheDocument();
    const openCurrent = screen.getByRole("button", { name: "Go to source comment" });
    expect(openCurrent.closest('[data-slot="dialog-header"]')).toBeInTheDocument();
    expect(openCurrent.querySelector(".lucide-locate-fixed")).toBeInTheDocument();
    const expand = screen.getByRole("button", { name: "Expand captured context" });
    expect(expand.closest('[data-slot="dialog-header"]')).toBeInTheDocument();
    fireEvent.click(expand);
    expect(dialog).toHaveClass("h-[calc(100dvh-2rem)]", "sm:max-w-[calc(100%-2rem)]");
    expect(screen.getByRole("button", { name: "Restore captured context size" })).toHaveAttribute("aria-pressed", "true");
  });

  it("does not render separate attachment lists in the captured-context viewer", () => {
    const withAttachment: IssueSourceContext = {
      ...baseContext,
      snapshot: {
        ...baseContext.snapshot,
        source_issue: {
          ...baseContext.snapshot.source_issue,
          attachments: [{
            id: "issue-snapshot-attachment",
            owner_type: "issue",
            owner_id: "source-issue",
            filename: "issue-captured.txt",
            content_type: "text/plain",
            size_bytes: 12,
            created_at: "2026-08-21T01:00:00Z",
          }],
        },
        comment_thread: [{
          ...baseContext.snapshot.comment_thread[0]!,
          attachments: [{
            id: "snapshot-attachment",
            owner_type: "comment",
            owner_id: "selected",
            filename: "captured.txt",
            content_type: "text/plain",
            size_bytes: 12,
            created_at: "2026-08-21T01:00:00Z",
          }],
        }],
      },
    };
    renderBadge(withAttachment);
    fireEvent.click(screen.getByRole("button", { name: "Context snapshot" }));
    expect(screen.getByText("Source body")).toBeInTheDocument();
    expect(screen.getByText("Captured comment")).toBeInTheDocument();
    expect(screen.queryByText("issue-captured.txt")).not.toBeInTheDocument();
    expect(screen.queryByText("captured.txt")).not.toBeInTheDocument();
  });

  it("opens the frozen viewer with a structured change summary", async () => {
    const changed: IssueSourceContext = {
      ...baseContext,
      display_state: "changed",
      comment_thread_state: "changed",
      change_reasons: ["comment_thread", "issue_description_attachments"],
      change_details: {
        changed_comment_ids: ["selected"],
        description_attachment_changes: [
          { kind: "added", attachment_id: "new-1", filename: "requirements.docx" },
          { kind: "added", attachment_id: "new-2", filename: "data.xlsx" },
          {
            kind: "replaced",
            attachment_id: "replacement",
            previous_filename: "draft.pptx",
            filename: "final.pptx",
          },
          { kind: "removed", attachment_id: "old-attachment", filename: "old.txt" },
        ],
      },
    };
    renderBadge(changed);
    expect(screen.getByRole("status")).toHaveTextContent("Source updated");
    fireEvent.click(screen.getByRole("button", { name: "Context snapshot" }));

    expect(await screen.findByRole("dialog")).toBeTruthy();
    expect(screen.getByText("MUL-7 · Source title · at capture · now NEW-7")).toBeTruthy();
    expect(screen.getByText("Alice at capture · now Fox")).toBeTruthy();
    const alert = screen.getByText("Source changed after capture.").closest("[data-slot='source-context-change-summary']");
    expect(alert).toHaveTextContent("Source changed after capture.");
    expect(alert).toHaveTextContent("Changed areas: Comment thread");
    expect(alert).toHaveTextContent("Changes to issue attachments: 2 added · 1 replaced · 1 removed");
    const stateLine = screen.getByText("Source changed after capture.").closest("p");
    expect(stateLine).toHaveTextContent("Source changed after capture. Changed areas: Comment thread");
    const attachmentRow = screen.getByText(/Changes to issue attachments:/).closest(
      "[data-slot='source-context-issue-attachment-summary']",
    );
    const detailsButton = screen.getByRole("button", { name: "Show issue attachment details" });
    expect(attachmentRow).toContainElement(detailsButton);
    expect(attachmentRow?.tagName).toBe("P");
    expect(detailsButton.parentElement).toBe(attachmentRow);
    expect(detailsButton).toHaveTextContent("");
    expect(detailsButton.querySelectorAll("svg")).toHaveLength(1);
    fireEvent.click(detailsButton);
    const details = screen.getByText("Added:").closest("ul");
    if (!details) throw new Error("Expected issue attachment details to render as a list");
    expect(details).toHaveTextContent("Added: requirements.docx, data.xlsx");
    expect(details).toHaveTextContent("Replaced: draft.pptx → final.pptx");
    expect(details).toHaveTextContent("Removed: old.txt");
    expect(within(details).getAllByRole("listitem")).toHaveLength(3);
    expect(screen.getByRole("button", { name: "Hide issue attachment details" })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("Captured comment").closest("[data-source-context-change-kind='changed']")).toHaveClass(
      "bg-amber-500/5",
    );
    expect(screen.getByRole("button", { name: "Go to source comment" })).toBeTruthy();
  });

  it("distinguishes added, changed, and deleted comment nodes", () => {
    const makeComment = (
      id: string,
      content: string,
      createdAt: string,
    ): SourceContextCommentSnapshot => ({
      ...baseContext.snapshot.comment_thread[0]!,
      id,
      content,
      created_at: createdAt,
      updated_at: createdAt,
    });
    const changedComment = makeComment("changed", "Changed comment", "2026-08-21T01:00:00Z");
    const removedComment = makeComment("removed", "Removed comment", "2026-08-21T03:00:00Z");
    const addedComment = makeComment("added", "Added comment", "2026-08-21T02:00:00Z");
    const context: IssueSourceContext = {
      ...baseContext,
      display_state: "changed",
      comment_thread_state: "changed",
      change_reasons: ["comment_thread"],
      change_details: {
        changed_comment_ids: [changedComment.id],
        added_comments: [addedComment],
        removed_comment_ids: [removedComment.id],
        description_attachment_changes: [],
      },
      snapshot: {
        ...baseContext.snapshot,
        anchor_comment_id: removedComment.id,
        comment_thread: [changedComment, removedComment],
      },
    };
    renderBadge(context);
    fireEvent.click(screen.getByRole("button", { name: "Context snapshot" }));

    expect(screen.getByText("Added comment").closest("[data-source-context-change-kind='added']")).toHaveClass(
      "bg-success/5",
    );
    expect(screen.getByText("Changed comment").closest("[data-source-context-change-kind='changed']")).toHaveClass(
      "bg-amber-500/5",
    );
    expect(screen.getByText("Removed comment").closest("[data-source-context-change-kind='deleted']")).toHaveClass(
      "bg-destructive/5",
    );
    expect(screen.getByText("Added after capture")).toHaveClass("sr-only");
    expect(screen.getByText("Changed after capture")).toHaveClass("sr-only");
    expect(screen.getByText("Deleted after capture")).toHaveClass("sr-only");
  });

  it("structures Chinese field and issue-attachment changes", async () => {
    const changed: IssueSourceContext = {
      ...baseContext,
      display_state: "changed",
      source_issue_state: "changed",
      comment_thread_state: "changed",
      change_reasons: ["issue_title", "issue_description", "comment_thread", "issue_description_attachments"],
      change_details: {
        changed_comment_ids: ["selected"],
        description_attachment_changes: [{
          kind: "removed", attachment_id: "github", filename: "github.png",
        }],
      },
    };
    const refetchIssue = vi.fn().mockResolvedValue({ source_context: changed } as Issue);
    render(
      <I18nProvider locale="zh-Hans" resources={{ "zh-Hans": { issues: zhHansIssues } }}>
        <SourceContextBadge context={changed} refetchIssue={refetchIssue} />
      </I18nProvider>,
    );

    expect(screen.getByRole("status")).toHaveTextContent("来源有更新");
    fireEvent.click(screen.getByRole("button", { name: "上下文快照" }));

    const alert = await screen.findByText("来源在捕获后发生变化。");
    const status = alert.closest("[data-slot='source-context-change-summary']");
    expect(alert.closest("p")).toHaveTextContent(
      "来源在捕获后发生变化。变化项：任务标题 · 任务描述 · 讨论",
    );
    expect(status).toHaveTextContent("任务附件变化：移除 1");
  });

  it("keeps a single changed object in the same structured layout", async () => {
    const changed: IssueSourceContext = {
      ...baseContext,
      display_state: "changed",
      source_issue_state: "changed",
      change_reasons: ["issue_title"],
    };
    renderBadge(changed);

    fireEvent.click(screen.getByRole("button", { name: "Context snapshot" }));

    const alert = (await screen.findByText("Source changed after capture.")).closest("[data-slot='source-context-change-summary']");
    expect(alert).toHaveTextContent("Changed areas: Issue title");
  });

  it("formats the complete public change-object set in a stable order", async () => {
    const changed: IssueSourceContext = {
      ...baseContext,
      display_state: "changed",
      source_issue_state: "changed",
      comment_thread_state: "changed",
      change_reasons: [
        "issue_title",
        "issue_description",
        "comment_thread",
      ],
    };
    renderBadge(changed);

    fireEvent.click(screen.getByRole("button", { name: "Context snapshot" }));

    const alert = (await screen.findByText("Source changed after capture.")).closest("[data-slot='source-context-change-summary']");
    expect(alert).toHaveTextContent(
      "Changed areas: Issue title · Issue description · Comment thread",
    );
  });

  it("labels unavailable comparison state independently from source navigation", async () => {
    const unavailable: IssueSourceContext = {
      ...baseContext,
      display_state: "unavailable",
      comment_thread_state: "unavailable",
    };
    renderBadge(unavailable);

    expect(screen.getByRole("status")).toHaveTextContent("Source unavailable");
    fireEvent.click(screen.getByRole("button", { name: "Context snapshot" }));

    expect(await screen.findByText("The current source could not be checked.")).toBeTruthy();
    expect(screen.getByText("Captured context is still available.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Go to source comment" })).toBeTruthy();
  });

  it("keeps the snapshot readable without a live action after source issue deletion", async () => {
    const deleted: IssueSourceContext = {
      ...baseContext,
      display_state: "deleted",
      source_issue_state: "deleted",
      comment_thread_state: "unavailable",
      anchor_comment_state: "deleted",
      can_open_current_source: false,
      current_source: undefined,
      change_details: {
        changed_comment_ids: [],
        removed_comment_ids: ["selected"],
        description_attachment_changes: [],
      },
    };
    renderBadge(deleted);
    expect(screen.getByRole("status")).toHaveTextContent("Source issue deleted");
    fireEvent.click(screen.getByRole("button", { name: "Context snapshot" }));

    expect(await screen.findByText("Source issue deleted after capture.")).toBeTruthy();
    expect(await screen.findByText("Captured comment")).toBeTruthy();
    expect(screen.getByText("Captured comment").closest("[data-source-context-change-kind='deleted']")).toHaveClass(
      "bg-destructive/5",
    );
    expect(screen.queryByRole("button", { name: "Go to source comment" })).toBeNull();
  });

  it("treats anchor deletion as a thread change and disables navigation", async () => {
    const changedEarlierComment: SourceContextCommentSnapshot = {
      ...baseContext.snapshot.comment_thread[0]!,
      id: "earlier",
      content: "Earlier changed comment",
      created_at: "2026-08-21T00:30:00Z",
      updated_at: "2026-08-21T00:30:00Z",
    };
    const deleted: IssueSourceContext = {
      ...baseContext,
      display_state: "changed",
      comment_thread_state: "changed",
      anchor_comment_state: "deleted",
      can_open_current_source: false,
      current_source: undefined,
      change_reasons: ["comment_thread"],
      change_details: {
        changed_comment_ids: [changedEarlierComment.id],
        removed_comment_ids: ["selected"],
        description_attachment_changes: [],
      },
      snapshot: {
        ...baseContext.snapshot,
        comment_thread: [changedEarlierComment, ...baseContext.snapshot.comment_thread],
      },
    };
    renderBadge(deleted);

    expect(screen.getByRole("status")).toHaveTextContent("Source updated");
    fireEvent.click(screen.getByRole("button", { name: "Context snapshot" }));

    const alert = (await screen.findByText("Source changed after capture.")).closest("[data-slot='source-context-change-summary']");
    expect(alert).toHaveTextContent("Changed areas: Comment thread");
    expect(screen.getByText("Captured comment").closest("[data-source-context-change-kind='deleted']")).toHaveClass(
      "bg-destructive/5",
    );
    expect(screen.getByText("Earlier changed comment").closest("[data-source-context-change-kind='changed']")).toHaveClass(
      "bg-amber-500/5",
    );
    expect(screen.queryByRole("button", { name: "Go to source comment" })).toBeNull();
  });

  it("updates the badge when focus refetch supplies fresher source state", async () => {
    const refetchIssue = vi.fn().mockResolvedValue({ source_context: baseContext } as Issue);
    const { rerender } = render(
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <SourceContextBadge context={baseContext} refetchIssue={refetchIssue} />
      </I18nProvider>,
    );
    const changed: IssueSourceContext = {
      ...baseContext,
      display_state: "changed",
      comment_thread_state: "changed",
    };

    rerender(
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <SourceContextBadge context={changed} refetchIssue={refetchIssue} />
      </I18nProvider>,
    );

    expect(await screen.findByRole("status")).toHaveTextContent("Source updated");
  });

  it.each([
    ["archived", "member", "Alice at capture · now Fox · archived"],
    ["deleted_agent", "agent", "Alice · deleted agent"],
    ["no_longer_in_workspace", "member", "Alice · no longer in workspace"],
  ] as const)("renders the captured-author %s state", async (state, type, expected) => {
    const context: IssueSourceContext = {
      ...baseContext,
      display_state: "changed",
      comment_thread_state: "changed",
      source_author_state: [{
        type,
        id: "member-1",
        captured_name: "Alice",
        current_name: state === "deleted_agent" || state === "no_longer_in_workspace" ? undefined : "Fox",
        state,
      }],
      snapshot: {
        ...baseContext.snapshot,
        comment_thread: [{
          ...baseContext.snapshot.comment_thread[0]!,
          author: { type, id: "member-1", name: "Alice" },
        }],
      },
    };
    renderBadge(context);
    fireEvent.click(screen.getByRole("button", { name: "Context snapshot" }));

    expect(await screen.findByText(expected)).toBeTruthy();
  });

  const displayCases = [
    {
      name: "unchanged",
      patch: {},
      trigger: "Context snapshot",
      openCurrent: true,
    },
    {
      name: "changed",
      patch: { display_state: "changed", source_issue_state: "changed" },
      trigger: "Context snapshot",
      openCurrent: true,
    },
    {
      name: "anchor deleted",
      patch: {
        display_state: "changed",
        comment_thread_state: "changed",
        anchor_comment_state: "deleted",
        can_open_current_source: false,
        current_source: undefined,
      },
      trigger: "Context snapshot",
      openCurrent: false,
    },
    {
      name: "issue deleted",
      patch: {
        display_state: "deleted",
        source_issue_state: "deleted",
        comment_thread_state: "unavailable",
        anchor_comment_state: "deleted",
        can_open_current_source: false,
        current_source: undefined,
      },
      trigger: "Context snapshot",
      openCurrent: false,
    },
    {
      name: "unavailable",
      patch: {
        display_state: "unavailable",
        source_issue_state: "unavailable",
        comment_thread_state: "unavailable",
        anchor_comment_state: "unavailable",
        can_open_current_source: false,
        current_source: undefined,
      },
      trigger: "Context snapshot",
      openCurrent: false,
    },
  ] as const;
  const authorCases = [
    { name: "unchanged", type: "member", state: "unchanged", current_name: "Alice", expected: "Alice" },
    { name: "renamed", type: "member", state: "renamed", current_name: "Fox", expected: "Alice at capture · now Fox" },
    { name: "archived", type: "agent", state: "archived", current_name: "Fox", expected: "Alice at capture · now Fox · archived" },
    { name: "deleted agent", type: "agent", state: "deleted_agent", current_name: undefined, expected: "Alice · deleted agent" },
    { name: "left workspace", type: "member", state: "no_longer_in_workspace", current_name: undefined, expected: "Alice · no longer in workspace" },
    { name: "unavailable", type: "member", state: "unavailable", current_name: undefined, expected: "Alice at capture" },
  ] as const;
  const viewerMatrix = displayCases.flatMap((display) =>
    authorCases.map((author) => ({ display, author })),
  );

  it.each(viewerMatrix)(
    "covers $display.name source × $author.name author",
    async ({ display, author }) => {
      const context: IssueSourceContext = {
        ...baseContext,
        ...display.patch,
        source_author_state: [{
          type: author.type,
          id: "member-1",
          captured_name: "Alice",
          current_name: author.current_name,
          state: author.state,
        }],
        snapshot: {
          ...baseContext.snapshot,
          comment_thread: [{
            ...baseContext.snapshot.comment_thread[0]!,
            author: { type: author.type, id: "member-1", name: "Alice" },
          }],
        },
      };
      renderBadge(context);
      fireEvent.click(screen.getByRole("button", { name: display.trigger }));
      const dialog = await screen.findByRole("dialog");

      expect(screen.getByText(author.expected)).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Go to source comment" }) !== null)
        .toBe(display.openCurrent);
      expect(dialog).toHaveClass("h-[min(82dvh,56rem)]", "sm:max-w-4xl");
    },
  );
});
