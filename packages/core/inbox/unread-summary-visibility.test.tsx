/** @vitest-environment jsdom */
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider, focusManager } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { createQueryClient } from "../query-client";
import type { InboxWorkspaceUnread } from "../types";
import { useInboxUnreadCount } from "./queries";
import { onInboxSummaryInvalidate } from "./ws-updaters";

/**
 * Every open tab is its own client and receives every inbox event. Before this
 * gate each event cost one summary request per open tab, hidden ones included,
 * and each request recomputes the newest item per issue over the user's whole
 * active inbox.
 */

const WS = "ws-1";
let unread = 1;
const getInboxUnreadSummary = vi.fn(
  async (): Promise<InboxWorkspaceUnread[]> => [{ workspace_id: WS, count: unread }],
);

beforeEach(() => {
  unread = 1;
  getInboxUnreadSummary.mockClear();
  setApiInstance({ getInboxUnreadSummary } as unknown as ApiClient);
});

afterEach(() => {
  cleanup();
  focusManager.setFocused(undefined);
});

function setup() {
  const qc = createQueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const view = renderHook(() => useInboxUnreadCount(WS), { wrapper });
  return { qc, view };
}

async function setPageVisible(visible: boolean) {
  await act(async () => {
    focusManager.setFocused(visible);
  });
}

async function inboxEvent(qc: ReturnType<typeof createQueryClient>) {
  unread += 1;
  await act(async () => {
    await onInboxSummaryInvalidate(qc);
  });
}

it("refetches on every inbox event while the page is visible", async () => {
  const { qc, view } = setup();
  await waitFor(() => expect(view.result.current).toBe(1));

  await inboxEvent(qc);
  await waitFor(() => expect(view.result.current).toBe(2));
  expect(getInboxUnreadSummary).toHaveBeenCalledTimes(2);
});

it("a hidden page defers every inbox event to one request when it is shown", async () => {
  const { qc, view } = setup();
  await waitFor(() => expect(view.result.current).toBe(1));
  await setPageVisible(false);

  await inboxEvent(qc);
  await inboxEvent(qc);
  await inboxEvent(qc);
  expect(getInboxUnreadSummary).toHaveBeenCalledTimes(1);

  await setPageVisible(true);
  await waitFor(() => expect(view.result.current).toBe(4));
  expect(getInboxUnreadSummary).toHaveBeenCalledTimes(2);
});

it("a page shown again after no inbox event makes no request", async () => {
  const { view } = setup();
  await waitFor(() => expect(view.result.current).toBe(1));

  await setPageVisible(false);
  await setPageVisible(true);

  expect(getInboxUnreadSummary).toHaveBeenCalledTimes(1);
});

it("a page opened hidden fetches the summary when it is first shown", async () => {
  await setPageVisible(false);
  const { view } = setup();
  expect(getInboxUnreadSummary).not.toHaveBeenCalled();

  await setPageVisible(true);
  await waitFor(() => expect(view.result.current).toBe(1));
  expect(getInboxUnreadSummary).toHaveBeenCalledTimes(1);
});

it("whileHidden keeps the summary live on a hidden page, as the dock badge needs", async () => {
  // Desktop mounts both: the sidebar's gated reader and the dock badge.
  const qc = createQueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const view = renderHook(
    () => ({
      sidebar: useInboxUnreadCount(WS),
      dock: useInboxUnreadCount(WS, { whileHidden: true }),
    }),
    { wrapper },
  );
  await waitFor(() => expect(view.result.current.dock).toBe(1));
  await setPageVisible(false);

  await inboxEvent(qc);
  await waitFor(() => expect(view.result.current.dock).toBe(2));
  expect(getInboxUnreadSummary).toHaveBeenCalledTimes(2);
});
