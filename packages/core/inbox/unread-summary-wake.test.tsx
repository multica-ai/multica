/** @vitest-environment jsdom */
import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClientProvider, focusManager } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { createQueryClient } from "../query-client";
import type { InboxWorkspaceUnread } from "../types";
import { inboxKeys, nextInboxSummaryWake, useInboxUnreadCount } from "./queries";

/**
 * Nothing is sent when a snooze runs out (MUL-9215). The summary says when the
 * next one does, so the badge comes back on its own, wherever the user is.
 */

const WS = "ws-1";
const START = Date.parse("2026-11-14T12:00:00Z");
const WAKE = START + 60_000;
const getInboxUnreadSummary = vi.fn<() => Promise<InboxWorkspaceUnread[]>>();

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(START);
  getInboxUnreadSummary.mockReset();
  getInboxUnreadSummary.mockResolvedValue([{ workspace_id: WS, count: 1, needs_you: 1 }]);
  setApiInstance({ getInboxUnreadSummary } as unknown as ApiClient);
});

afterEach(() => {
  cleanup();
  focusManager.setFocused(undefined);
  vi.useRealTimers();
});

function setup(options?: { whileHidden?: boolean }) {
  const qc = createQueryClient();
  qc.setQueryData<InboxWorkspaceUnread[]>(inboxKeys.unreadSummary(), [
    { workspace_id: WS, count: 0, needs_you: 0, next_wake_at: new Date(WAKE).toISOString() },
  ]);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return renderHook(() => useInboxUnreadCount(WS, options), { wrapper });
}

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

it("re-reads the summary when a snoozed issue comes back, with no Inbox open", async () => {
  const view = setup();
  expect(view.result.current).toBe(0);

  await advance(WAKE - START - 1);
  expect(getInboxUnreadSummary).not.toHaveBeenCalled();

  await advance(2_000);
  expect(getInboxUnreadSummary).toHaveBeenCalledTimes(1);
  expect(view.result.current).toBe(1);

  // Nothing else is snoozed, so nothing else is asked.
  await advance(24 * 3_600_000);
  expect(getInboxUnreadSummary).toHaveBeenCalledTimes(1);
});

it("a page hidden when the snooze runs out re-reads the summary once shown", async () => {
  const view = setup();
  await act(async () => {
    focusManager.setFocused(false);
  });

  await advance(WAKE - START + 2_000);
  expect(getInboxUnreadSummary).not.toHaveBeenCalled();

  await act(async () => {
    focusManager.setFocused(true);
  });
  await advance(0);
  expect(getInboxUnreadSummary).toHaveBeenCalledTimes(1);
  expect(view.result.current).toBe(1);
});

it("the dock badge re-reads the summary at the wake while the page is hidden", async () => {
  const view = setup({ whileHidden: true });
  await act(async () => {
    focusManager.setFocused(false);
  });

  await advance(WAKE - START + 2_000);
  expect(getInboxUnreadSummary).toHaveBeenCalledTimes(1);
  expect(view.result.current).toBe(1);
});

it("takes the earliest wake across workspaces and ignores what it cannot read", () => {
  expect(nextInboxSummaryWake(undefined)).toBeNull();
  expect(nextInboxSummaryWake([{ workspace_id: "a", count: 0 }])).toBeNull();
  expect(
    nextInboxSummaryWake([
      { workspace_id: "a", count: 0, next_wake_at: "2026-11-15T00:00:00Z" },
      { workspace_id: "b", count: 0, next_wake_at: "not a time" },
      { workspace_id: "c", count: 0, next_wake_at: "2026-11-14T13:00:00Z" },
    ]),
  ).toBe(Date.parse("2026-11-14T13:00:00Z"));
});
