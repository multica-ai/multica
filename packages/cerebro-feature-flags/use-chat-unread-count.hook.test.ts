// @vitest-environment jsdom
//
// FIR-4350 — the badge must count over the SAME channel set the Chat page
// renders. The Chat page's SlackBlock reads the roster query
// (include_archived: true), because inbox-archiving hides a channel from the
// inbox feed only, not from Chat. Counting the plain inbox list here let an
// unread archived channel sit visible in Chat while the badge stayed at 0.
//
// The defect is which query the hook picks, so it can only be caught by
// rendering the hook — the pure counter in use-chat-unread-count.test.ts is
// correct either way.
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor, cleanup } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Channel } from "@multica/core/types";

const { listChannels } = vi.hoisted(() => ({ listChannels: vi.fn() }));

vi.mock("@multica/core/api", () => ({
  api: {
    listChannels,
    listChatSessions: vi.fn(async () => []),
    listInbox: vi.fn(async () => []),
  },
}));
vi.mock("./api", () => ({ useFeatureFlag: () => true }));
vi.mock("./use-chat-placement", () => ({
  useChatPlacement: () => ({
    placement: {
      channel: { chat: true, inbox: false },
      dm: { chat: true, inbox: false },
      agent_chat: { chat: true, inbox: false },
    },
    setPlacement: vi.fn(),
  }),
}));

const { useChatUnreadCount } = await import("./use-chat-unread-count");

const ARCHIVED_UNREAD: Channel = {
  id: "c-archived",
  workspace_id: "w",
  number: 1,
  identifier: "C-1",
  kind: "channel",
  title: "archived-but-unread",
  description: null,
  status: "todo",
  project_id: null,
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "u",
  participants: [],
  unread_count: 2,
  last_message: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} as unknown as Channel;

const ARCHIVED_DM_UNREAD: Channel = {
  ...ARCHIVED_UNREAD,
  id: "dm-archived",
  identifier: "C-2",
  kind: "dm",
  title: "archived-dm",
} as unknown as Channel;

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return createElement(QueryClientProvider, { client }, children);
}

afterEach(() => {
  cleanup();
  listChannels.mockReset();
});

describe("useChatUnreadCount", () => {
  it("counts an unread channel that is archived out of the inbox feed", async () => {
    // The inbox list drops archived channels; only the roster keeps them.
    listChannels.mockImplementation(async (params?: { include_archived?: boolean }) =>
      params?.include_archived === true ? [ARCHIVED_UNREAD] : [],
    );

    const { result } = renderHook(() => useChatUnreadCount("w"), { wrapper });

    await waitFor(() => expect(result.current).toBe(1));
    expect(listChannels).toHaveBeenCalledWith({ include_archived: true });
  });

  it("does NOT count an unread DM that is archived out of the inbox feed", async () => {
    // FIR-4350 — the Chat rail's People section matches DMs from the
    // non-archived list, so an inbox-archived DM (e.g. snoozed as a reminder) is
    // hidden from Chat. It sits only in the include-archived roster; counting it
    // there made the badge claim an unread the user found nowhere in Chat.
    listChannels.mockImplementation(async (params?: { include_archived?: boolean }) =>
      params?.include_archived === true ? [ARCHIVED_DM_UNREAD] : [],
    );

    const { result } = renderHook(() => useChatUnreadCount("w"), { wrapper });

    // Give both queries time to settle before asserting the count stays 0.
    await waitFor(() => expect(listChannels).toHaveBeenCalledWith({ include_archived: true }));
    await waitFor(() => expect(listChannels).toHaveBeenCalledWith());
    expect(result.current).toBe(0);
  });

  it("counts a non-archived unread DM", async () => {
    listChannels.mockImplementation(async () => [ARCHIVED_DM_UNREAD]);

    const { result } = renderHook(() => useChatUnreadCount("w"), { wrapper });

    await waitFor(() => expect(result.current).toBe(1));
  });
});
