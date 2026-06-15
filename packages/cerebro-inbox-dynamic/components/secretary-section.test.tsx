// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { InboxActionContext } from "@multica/cerebro-inbox";
import type { InboxItem } from "@multica/core/types";
import type { DynInboxEntry, SectionFilterContext } from "../section-filter";
import {
  SecretarySection,
  chooseSecretaryEntries,
  secretaryEntryKey,
} from "./secretary-section";

afterEach(() => cleanup());

vi.mock("./dynamic-inbox-row", () => ({
  DynamicInboxRow: ({
    entry,
    onSelect,
  }: {
    entry: DynInboxEntry;
    onSelect: (entry: DynInboxEntry) => void;
  }) => (
    <button type="button" onClick={() => onSelect(entry)}>
      {entry.kind === "notif" ? entry.item.title : entry.id}
    </button>
  ),
}));

const filterContext: SectionFilterContext = {
  action: {
    userId: "me",
    issueRunStates: new Map(),
    subIssueRunStates: new Map(),
    chatRunStates: new Map(),
    mentionedChannels: new Set(),
    wakeupIssueIds: new Set(),
  } satisfies InboxActionContext,
  matchesPins: () => false,
};

function item(id: string, title: string, read: boolean): InboxItem {
  return {
    id,
    issue_id: `issue-${id}`,
    type: "new_comment",
    severity: "action_required",
    title,
    body: "",
    read,
    archived: false,
    created_at: "2026-06-15T00:00:00Z",
    updated_at: "2026-06-15T00:00:00Z",
  } as unknown as InboxItem;
}

function entry(id: string, title: string, read: boolean, time: number): DynInboxEntry {
  return { kind: "notif", id, time, item: item(id, title, read) };
}

function renderSecretary(entries: DynInboxEntry[], completedKeys = new Set<string>()) {
  const props: React.ComponentProps<typeof SecretarySection> = {
    entries,
    filterContext,
    selectedKey: null,
    completedKeys,
    onSelect: vi.fn(),
    onArchive: vi.fn(),
    onResetCompleted: vi.fn(),
  };
  const result = render(<SecretarySection {...props} />);
  return { ...result, props };
}

describe("chooseSecretaryEntries", () => {
  it("prioritizes unread first, then oldest waiting first", () => {
    const newestUnread = entry("new-unread", "New unread", false, 30);
    const oldestUnread = entry("old-unread", "Old unread", false, 10);
    const oldestRead = entry("old-read", "Old read", true, 1);

    expect(chooseSecretaryEntries([newestUnread, oldestRead, oldestUnread], 2)).toEqual([
      oldestUnread,
      newestUnread,
    ]);
  });
});

describe("SecretarySection", () => {
  it("requires exactly the chosen manual count before starting", async () => {
    const user = userEvent.setup();
    renderSecretary([
      entry("a", "Alpha", false, 1),
      entry("b", "Beta", false, 2),
      entry("c", "Gamma", false, 3),
    ]);

    await user.click(screen.getByRole("button", { name: "3" }));
    await user.click(screen.getByRole("button", { name: /i choose/i }));

    expect(screen.getByRole("button", { name: /^start$/i })).toHaveProperty("disabled", true);

    await user.click(screen.getByRole("button", { name: "Alpha" }));
    await user.click(screen.getByRole("button", { name: "Beta" }));

    expect(screen.getByRole("button", { name: /^start$/i })).toHaveProperty("disabled", true);

    await user.click(screen.getByRole("button", { name: "Gamma" }));

    expect(screen.getByRole("button", { name: /^start$/i })).toHaveProperty("disabled", false);
  });

  it("shows the Good work state when the active round is completed", async () => {
    const user = userEvent.setup();
    const onlyEntry = entry("a", "Alpha", false, 1);
    const { rerender, props } = renderSecretary([onlyEntry]);

    await user.click(screen.getByRole("button", { name: /let agent choose/i }));

    rerender(
      <SecretarySection
        {...props}
        completedKeys={new Set([secretaryEntryKey(onlyEntry)])}
      />,
    );

    expect(await screen.findByText("Good work")).toBeTruthy();
  });
});
