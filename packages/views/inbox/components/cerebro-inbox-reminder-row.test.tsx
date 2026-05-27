import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import type { InboxItem } from "@multica/core/types";

import { CerebroInboxReminderRow } from "@multica/cerebro-inbox";

function makeItem(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "n1",
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "me",
    actor_type: "member",
    actor_id: "alice",
    type: "new_comment",
    severity: "info",
    route: "inbox",
    issue_id: "i1",
    project_id: null,
    title: "Ship the thing",
    body: "looks good",
    issue_status: "todo",
    read: false,
    archived: false,
    muted_until: null,
    created_at: new Date().toISOString(),
    details: null,
    ...overrides,
  };
}

describe("CerebroInboxReminderRow", () => {
  it("renders nothing for a non-reminder row", () => {
    const { container } = render(<CerebroInboxReminderRow item={makeItem()} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the badge + overdue prefix when a snoozed row is overdue", () => {
    render(
      <CerebroInboxReminderRow
        item={makeItem({ muted_until: "2000-01-01T11:00:00.000Z" })}
      />,
    );
    expect(screen.getByText("Reminder")).toBeInTheDocument();
    expect(
      screen.getByText((t) => t.startsWith("Overdue since")),
    ).toBeInTheDocument();
  });

  it("shows the planned prefix for a future native reminder", () => {
    const future = new Date(Date.now() + 60 * 60 * 1000).toISOString();
    render(
      <CerebroInboxReminderRow
        item={makeItem({ type: "reminder", details: { planned_at: future } })}
      />,
    );
    expect(screen.getByText("Reminder")).toBeInTheDocument();
    expect(
      screen.getByText((t) => t.startsWith("Planned for")),
    ).toBeInTheDocument();
  });
});
