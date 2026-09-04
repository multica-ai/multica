import { beforeEach, describe, expect, it, vi } from "vitest";

const { invalidateQueries, subscriptionSetups } = vi.hoisted(() => ({
  invalidateQueries: vi.fn(),
  subscriptionSetups: [] as Array<
    (ws: MockWS, wsId: string) => Array<() => void>
  >,
}));

type EventHandler = (payload: unknown) => void;

interface MockWS {
  on: ReturnType<typeof vi.fn>;
  onReconnect: ReturnType<typeof vi.fn>;
}

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries }),
}));

vi.mock("@/lib/use-ws-subscriptions", () => ({
  useWSSubscriptions: (
    setup: (ws: MockWS, wsId: string) => Array<() => void>,
  ) => {
    subscriptionSetups.push(setup);
  },
}));

import { usePresenceRealtime } from "./use-presence-realtime";

describe("usePresenceRealtime", () => {
  beforeEach(() => {
    invalidateQueries.mockReset();
    subscriptionSetups.length = 0;
  });

  it("refreshes hidden agent liveness on daemon register and reconnect", () => {
    usePresenceRealtime();
    expect(subscriptionSetups).toHaveLength(1);

    const handlers = new Map<string, EventHandler>();
    let reconnect: (() => void) | undefined;
    const ws: MockWS = {
      on: vi.fn((event: string, handler: EventHandler) => {
        handlers.set(event, handler);
        return () => {};
      }),
      onReconnect: vi.fn((handler: () => void) => {
        reconnect = handler;
        return () => {};
      }),
    };

    subscriptionSetups[0](ws, "workspace-1");
    handlers.get("daemon:register")?.({});

    const invalidatedKeys = () =>
      invalidateQueries.mock.calls.map(([query]) => query.queryKey);
    expect(invalidatedKeys()).toEqual(
      expect.arrayContaining([
        ["runtimes", "workspace-1"],
        ["agents", "workspace-1"],
      ]),
    );

    invalidateQueries.mockClear();
    reconnect?.();

    expect(invalidatedKeys()).toEqual(
      expect.arrayContaining([
        ["runtimes", "workspace-1"],
        ["agents", "workspace-1"],
        ["agent-task-snapshot", "workspace-1"],
      ]),
    );
  });
});
