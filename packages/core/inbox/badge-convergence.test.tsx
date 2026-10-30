/** @vitest-environment jsdom */
import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClientProvider, useQuery } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { createQueryClient } from "../query-client";
import type { InboxItem, InboxWorkspaceUnread } from "../types";
import { useMarkInboxRead } from "./mutations";
import {
  deduplicateInboxItems,
  inboxKeys,
  inboxListOptions,
  useInboxUnreadCount,
} from "./queries";
import {
  onInboxInvalidate,
  onInboxNew,
  onInboxSummaryInvalidate,
} from "./ws-updaters";

/**
 * The badge and the list it counts are two independent server reads, rendered
 * side by side. This pins the property that makes that safe (MUL-6967): once
 * everything settles, the badge equals the unread rows the list shows, and
 * both equal the server — whatever order responses and events arrive in.
 *
 * The first release shipped with the list violating it: "2 unread" over a list
 * with nothing unread. A change that landed while the list's FIRST load was in
 * flight was answered by that pre-change request and never re-read. The first
 * load is exercised repeatedly here on purpose, because in the app it is not a
 * one-off — nothing outside the Inbox page observes the list, so on web the
 * cache is collected while the user is elsewhere and every return is a first
 * load again.
 */

vi.mock("../hooks", () => ({ useWorkspaceId: () => "ws-1" }));
afterEach(cleanup);

const WS = "ws-1";
// Deterministic, so the range is a fixed set of interleavings. Seeds 4, 16 and
// 21 fail against the pre-fix code (a list refresh swallowed by the list's
// first load) — the range keeps them so this guard has proven teeth, at a
// fraction of the cost of a wider sweep. The mechanism itself is pinned
// directly in ws-updaters.test.ts; this covers the interleavings around it.
const SEEDS = 25;

// Deterministic PRNG, so a failing interleaving replays exactly by seed.
function mulberry32(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/**
 * A server with the grouping rule of CountUnreadInboxByWorkspace and
 * ListInboxItems. Each response is computed when the request ARRIVES but
 * handed back only when the scheduler releases it — the adversarial case,
 * where a response describing an older state lands after newer writes.
 */
class FakeServer {
  rows: InboxItem[] = [];
  private clock = 0;
  private seq = 0;

  notify(issueId: string): InboxItem {
    // Strictly increasing: each autocommit INSERT gets its own now().
    this.clock += 1;
    const row: InboxItem = {
      id: `n${++this.seq}`,
      workspace_id: WS,
      recipient_type: "member",
      recipient_id: "me",
      actor_type: "agent",
      actor_id: "agent",
      type: "new_comment",
      severity: "info",
      issue_id: issueId,
      title: issueId,
      body: null,
      issue_status: null,
      read: false,
      archived: false,
      created_at: new Date(Date.UTC(2026, 8, 1, 0, 0, this.clock)).toISOString(),
      details: null,
    };
    this.rows.push(row);
    return row;
  }

  markRead(id: string) {
    const row = this.rows.find((r) => r.id === id);
    if (row) row.read = true;
  }

  list(): InboxItem[] {
    return this.rows
      .filter((r) => !r.archived)
      .map((r) => ({ ...r }))
      .sort((a, b) => b.created_at.localeCompare(a.created_at));
  }

  unread(): number {
    return deduplicateInboxItems(this.list()).filter((r) => !r.read).length;
  }

  summary(): InboxWorkspaceUnread[] {
    const count = this.unread();
    return count > 0 ? [{ workspace_id: WS, count }] : [];
  }
}

async function tick() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

function unreadIn(list: InboxItem[] | undefined) {
  return deduplicateInboxItems(list ?? []).filter((r) => r.read !== true);
}

it.each(Array.from({ length: SEEDS }, (_, i) => i + 1))(
  "badge converges on the listed unread rows (seed %i)",
  async (seed) => {
    const rand = mulberry32(seed);
    const server = new FakeServer();
    for (const issue of ["i1", "i2", "i3"]) server.notify(issue);
    server.markRead("n2");

    const pending: Array<() => void> = [];
    const events: Array<(qc: QueryClient) => void> = [];
    const qc = createQueryClient();

    const deferred = <T,>(value: () => T): Promise<T> => {
      const snapshot = value();
      return new Promise<T>((resolve) => {
        pending.push(() => resolve(snapshot));
      });
    };

    setApiInstance({
      listInbox: vi.fn(() => deferred(() => server.list())),
      getInboxUnreadSummary: vi.fn(() => deferred(() => server.summary())),
      markInboxRead: vi.fn((id: string) => {
        // Commit, then publish: the event exists before the response does.
        server.markRead(id);
        events.push((c) => {
          void onInboxInvalidate(c, WS);
          void onInboxSummaryInvalidate(c);
        });
        return deferred(() => server.rows.find((r) => r.id === id)!);
      }),
    } as unknown as ApiClient);

    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    );
    // The sidebar: always mounted, reads only the summary.
    const shell = renderHook(
      () => ({
        badge: useInboxUnreadCount(WS),
        markRead: useMarkInboxRead(),
      }),
      { wrapper },
    );
    // The Inbox page: the list's only observer, so it comes and goes.
    const mountPage = () =>
      renderHook(() => useQuery(inboxListOptions(WS)).data, { wrapper });
    let page: ReturnType<typeof mountPage> | null = mountPage();

    try {
      for (let step = 0; step < 40; step++) {
        const roll = rand();
        if (roll < 0.18) {
          const row = server.notify(`i${1 + Math.floor(rand() * 5)}`);
          events.push((c) => {
            void onInboxNew(c, WS, row);
            void onInboxSummaryInvalidate(c);
          });
        } else if (roll < 0.34 && page) {
          // The user opens an unread row the list is showing.
          const unread = unreadIn(page.result.current);
          if (unread.length > 0) {
            const target = unread[Math.floor(rand() * unread.length)]!;
            act(() => shell.result.current.markRead.mutate(target.id));
          }
        } else if (roll < 0.42) {
          // Leave the Inbox long enough for the list cache to be collected,
          // then come back — the next read is a first load again.
          if (page) {
            page.unmount();
            page = null;
            qc.removeQueries({ queryKey: inboxKeys.list(WS), exact: true });
          } else {
            page = mountPage();
          }
        } else if (roll < 0.75 && pending.length > 0) {
          // Release a random in-flight response — possibly a stale one.
          const [release] = pending.splice(Math.floor(rand() * pending.length), 1);
          await act(async () => release!());
        } else if (events.length > 0) {
          // Deliver the next event, in publish order.
          const event = events.shift()!;
          await act(async () => event(qc));
        }
        await tick();
      }

      // Drain: every event and response is eventually delivered, and the user
      // ends up back on the Inbox page.
      if (!page) page = mountPage();
      for (let guard = 0; guard < 500 && (pending.length || events.length); guard++) {
        const event = events.shift();
        if (event) await act(async () => event(qc));
        while (pending.length) {
          const [release] = pending.splice(Math.floor(rand() * pending.length), 1);
          await act(async () => release!());
        }
        for (let i = 0; i < 4; i++) await tick();
      }
      for (let i = 0; i < 6; i++) await tick();

      const truth = server.unread();
      expect({
        badge: shell.result.current.badge,
        listed: unreadIn(page.result.current).length,
      }).toEqual({ badge: truth, listed: truth });
    } finally {
      for (const release of pending) release();
      page?.unmount();
      shell.unmount();
      qc.clear();
    }
  },
);
