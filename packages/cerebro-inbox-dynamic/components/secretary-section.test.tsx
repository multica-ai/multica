// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { InboxActionContext } from "@multica/cerebro-inbox";
import type { InboxItem } from "@multica/core/types";
import type { Channel, ChatSession } from "@multica/core/types";
import type { SecretaryCriteria } from "@multica/cerebro-feature-flags";
import type { DynInboxEntry, SectionFilterContext } from "../section-filter";
import {
  SecretarySection,
  chooseSecretaryEntries,
  isSecretaryCandidate,
  secretaryEntryKey,
} from "./secretary-section";

afterEach(() => {
  cleanup();
  mockCriteria = { ...ALL_ON };
});

// The component reads the user's criteria via a preference hook; for the render
// tests we keep it at the default (everything included) so existing behaviour is
// unchanged. The criteria logic itself is unit-tested via isSecretaryCandidate.
const ALL_ON: SecretaryCriteria = {
  unreadOnly: false,
  includeIssues: true,
  includeChannels: true,
  includeChats: true,
  startFolded: true,
};
// Mutable so individual tests can flip startFolded (the criteria hook is the
// single source of truth for whether the block starts collapsed).
let mockCriteria: SecretaryCriteria = { ...ALL_ON };
vi.mock("@multica/cerebro-feature-flags", () => ({
  useSecretaryCriteria: () => mockCriteria,
}));

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

function mutedNotif(id: string): DynInboxEntry {
  const future = new Date(Date.now() + 60 * 60 * 1000).toISOString();
  return {
    kind: "notif",
    id,
    time: 1,
    item: { ...item(id, id, false), muted_until: future } as unknown as InboxItem,
  };
}

function channelEntry(id: string, opts: { unread?: boolean } = {}): DynInboxEntry {
  return {
    kind: "channel",
    id,
    time: 1,
    channel: { id, unread_count: opts.unread ? 1 : 0 } as unknown as Channel,
  };
}

function chatEntry(id: string): DynInboxEntry {
  return { kind: "chat", id, time: 1, session: { id } as unknown as ChatSession };
}

describe("isSecretaryCandidate", () => {
  it("never picks a muted row, whatever the criteria", () => {
    expect(isSecretaryCandidate(mutedNotif("m"), ALL_ON)).toBe(false);
  });

  it("includes each kind only when its criterion is on", () => {
    expect(isSecretaryCandidate(entry("n", "n", false, 1), { ...ALL_ON, includeIssues: false })).toBe(
      false,
    );
    expect(isSecretaryCandidate(channelEntry("c"), { ...ALL_ON, includeChannels: false })).toBe(false);
    expect(isSecretaryCandidate(chatEntry("a"), { ...ALL_ON, includeChats: false })).toBe(false);
    expect(isSecretaryCandidate(entry("n", "n", false, 1), ALL_ON)).toBe(true);
    expect(isSecretaryCandidate(channelEntry("c"), ALL_ON)).toBe(true);
    expect(isSecretaryCandidate(chatEntry("a"), ALL_ON)).toBe(true);
  });

  it("drops read rows when unreadOnly is set", () => {
    const unreadOnly = { ...ALL_ON, unreadOnly: true };
    expect(isSecretaryCandidate(entry("read", "read", true, 1), unreadOnly)).toBe(false);
    expect(isSecretaryCandidate(entry("unread", "unread", false, 1), unreadOnly)).toBe(true);
    expect(isSecretaryCandidate(channelEntry("c", { unread: false }), unreadOnly)).toBe(false);
    expect(isSecretaryCandidate(channelEntry("c2", { unread: true }), unreadOnly)).toBe(true);
  });
});

function renderSecretary(entries: DynInboxEntry[], completedKeys = new Set<string>()) {
  const props: React.ComponentProps<typeof SecretarySection> = {
    entries,
    filterContext,
    selectedKey: null,
    completedKeys,
    onSelect: vi.fn(),
    onArchive: vi.fn(),
    onResetCompleted: vi.fn(),
    onRemove: vi.fn(),
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
  it("starts collapsed and only shows the controls once expanded", async () => {
    const user = userEvent.setup();
    renderSecretary([entry("a", "Alpha", false, 1)]);

    // Collapsed: the start controls are hidden, only the header toggle shows.
    expect(screen.queryByRole("button", { name: /let agent choose/i })).toBeNull();

    await user.click(screen.getByRole("button", { name: /^secretary/i }));

    expect(screen.getByRole("button", { name: /let agent choose/i })).toBeTruthy();
  });

  it("starts expanded when the user turns off 'Start collapsed'", () => {
    mockCriteria = { ...ALL_ON, startFolded: false };
    renderSecretary([entry("a", "Alpha", false, 1)]);

    // Controls are visible immediately — no need to unfold first.
    expect(screen.getByRole("button", { name: /let agent choose/i })).toBeTruthy();
  });

  it("removes the whole block via the remove button", async () => {
    const user = userEvent.setup();
    const { props } = renderSecretary([entry("a", "Alpha", false, 1)]);

    await user.click(screen.getByRole("button", { name: /remove secretary block/i }));

    expect(props.onRemove).toHaveBeenCalledTimes(1);
  });

  it("requires exactly the chosen manual count before starting", async () => {
    const user = userEvent.setup();
    renderSecretary([
      entry("a", "Alpha", false, 1),
      entry("b", "Beta", false, 2),
      entry("c", "Gamma", false, 3),
    ]);

    await user.click(screen.getByRole("button", { name: /^secretary/i }));
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

    await user.click(screen.getByRole("button", { name: /^secretary/i }));
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
