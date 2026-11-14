import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { InboxItem } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { InboxReasonLabel } from "./inbox-reason-label";

// The change label reads the status catalog and the workspace's people. This
// file only cares whether it is shown, and for which notification.
vi.mock("./inbox-detail-label", () => ({
  InboxDetailLabel: ({ item }: { item: InboxItem }) => <span data-testid="change">{item.id}</span>,
  useTypeLabels: () => ({ app_request: "App request" }),
}));

function item(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "workspace-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue title",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-06-15T08:00:00Z",
    details: null,
    ...overrides,
  };
}

const reasonEl = (container: HTMLElement) => container.querySelector("[data-reason]") as HTMLElement;

describe("InboxReasonLabel", () => {
  it.each([
    [{ type: "agent_completed", issue_status: "in_review" }, "review", "Ready for review"],
    [{ type: "agent_completed", issue_status: "blocked" }, "question", "Waiting for your answer"],
    [{ type: "agent_blocked" }, "question", "Waiting for your answer"],
    [{ type: "mentioned" }, "mention", "Mentioned you"],
    [{ type: "task_failed" }, "failed", "Run failed"],
    [{ type: "task_cancelled" }, "cancelled", "Run cancelled"],
    [{ type: "issue_assigned" }, "assigned", "Assigned to you"],
    [{ type: "new_comment" }, "comment", "New comment"],
    [{ type: "agent_completed", issue_status: "in_progress" }, "finished", "Run finished"],
  ] as const)("names why %o waits", (overrides, kind, label) => {
    const reason = item(overrides as Partial<InboxItem>);
    const { container } = renderWithI18n(<InboxReasonLabel reason={reason} latest={reason} />);

    expect(reasonEl(container)).toHaveAttribute("data-reason", kind);
    expect(reasonEl(container)).toHaveTextContent(label);
  });

  it("colours only the two that ask for something to be answered or fixed", () => {
    const tone = (overrides: Partial<InboxItem>) => {
      const reason = item(overrides);
      const { container, unmount } = renderWithI18n(<InboxReasonLabel reason={reason} latest={reason} />);
      const className = reasonEl(container).className;
      unmount();
      return className;
    };

    expect(tone({ type: "agent_blocked" })).toContain("text-warning");
    expect(tone({ type: "task_failed" })).toContain("text-destructive");
    expect(tone({ type: "mentioned" })).toContain("text-foreground");
    expect(tone({ type: "agent_completed", issue_status: "in_review" })).toContain("text-foreground");
  });

  it("falls back to the notification's own label for a type it has no reason for", () => {
    const reason = item({ type: "app_request" as InboxItem["type"] });
    const { container } = renderWithI18n(<InboxReasonLabel reason={reason} latest={reason} />);

    expect(reasonEl(container)).toHaveTextContent("App request");
  });

  it("follows the reason with the latest thing said on the issue", () => {
    const reason = item({ type: "agent_blocked" });
    renderWithI18n(
      <InboxReasonLabel
        reason={reason}
        latest={reason}
        preview={item({ id: "inbox-said", body: "  Which region should it deploy to?  " })}
      />,
    );

    expect(screen.getByText("Which region should it deploy to?")).toBeInTheDocument();
    expect(screen.queryByTestId("change")).toBeNull();
  });

  it("shows a mention in what was said as its name, not its link", () => {
    const reason = item({ type: "mentioned" });
    renderWithI18n(
      <InboxReasonLabel
        reason={reason}
        latest={reason}
        preview={item({
          id: "inbox-said",
          type: "new_comment",
          body: "[@Ada](mention://member/user-1) can you decide on [MUL-12](mention://issue/issue-12)?",
        })}
      />,
    );

    expect(screen.getByText("@Ada can you decide on MUL-12?")).toBeInTheDocument();
  });

  it("does not repeat the issue title as what was said", () => {
    const reason = item({ type: "issue_assigned", body: "Issue title" });
    const { container } = renderWithI18n(
      <InboxReasonLabel reason={reason} latest={reason} preview={reason} />,
    );

    expect(container).toHaveTextContent(/^Assigned to you$/);
  });

  it("shows what changed last when nothing on the issue carries text", () => {
    const reason = item({ type: "mentioned" });
    renderWithI18n(
      <InboxReasonLabel reason={reason} latest={item({ id: "inbox-moved", type: "status_changed" })} />,
    );

    expect(screen.getByTestId("change")).toHaveTextContent("inbox-moved");
  });

  it.each([
    [{ cancelled_by_type: "member", cancelled_by_id: "member-1", cancelled_by_name: "Me" }, "You stopped this run"],
    [{ cancelled_by_type: "member", cancelled_by_id: "member-2", cancelled_by_name: "Ada" }, "Ada stopped this run"],
    [{ cancelled_by_type: "agent", cancelled_by_id: "agent-2", cancelled_by_name: "Lambda" }, "Lambda stopped this run"],
    [{ cancelled_by_type: "system" }, "Stopped by the system"],
  ])("says who stopped a cancelled run: %o", (details, text) => {
    const reason = item({ type: "task_cancelled", details: { task_id: "task-1", ...details } });
    renderWithI18n(<InboxReasonLabel reason={reason} latest={reason} />);

    expect(screen.getByText(text)).toBeInTheDocument();
  });

  it("shows a cancelled run alone when nothing says who stopped it", () => {
    const reason = item({ type: "task_cancelled", details: { task_id: "task-1" } });
    const { container } = renderWithI18n(<InboxReasonLabel reason={reason} latest={reason} />);

    expect(container).toHaveTextContent(/^Run cancelled$/);
  });

  it("keeps who stopped the run until something is said after it", () => {
    const reason = item({
      type: "task_cancelled",
      created_at: "2026-06-15T10:00:00Z",
      details: { cancelled_by_type: "member", cancelled_by_id: "member-1" },
    });
    const earlier = item({ id: "inbox-earlier", body: "Started on the migration", created_at: "2026-06-15T09:00:00Z" });
    const later = item({ id: "inbox-later", body: "Can you pick this back up?", created_at: "2026-06-15T11:00:00Z" });

    const { unmount } = renderWithI18n(<InboxReasonLabel reason={reason} latest={reason} preview={earlier} />);
    expect(screen.getByText("You stopped this run")).toBeInTheDocument();
    expect(screen.queryByText("Started on the migration")).toBeNull();
    unmount();

    renderWithI18n(<InboxReasonLabel reason={reason} latest={later} preview={later} />);
    expect(screen.getByText("Can you pick this back up?")).toBeInTheDocument();
    expect(screen.queryByText("You stopped this run")).toBeNull();
  });

  it("shows the reason alone when it is also the latest notification", () => {
    const reason = item({ type: "status_changed", issue_status: "in_review" });
    renderWithI18n(<InboxReasonLabel reason={reason} latest={reason} />);

    expect(screen.queryByTestId("change")).toBeNull();
  });
});
