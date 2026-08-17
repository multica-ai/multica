/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { workspaceKeys } from "./queries";
import { useActorName } from "./hooks";

// useActorName reads the current workspace from the core WorkspaceId provider;
// the directory-name resolution under test does not depend on the real id.
vi.mock("../hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useActorName", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  // MUL-4985 regression: while the member/agent/squad directory queries are
  // still loading, `data` is undefined. A `= []` default allocated a fresh
  // array every render, so `getActorName` (memoized on those arrays) changed
  // identity on every render. Consumers that list `getActorName` in their own
  // memo deps (BoardView's `groups`, SwimLaneView's `laneGroups`) then churned
  // a new value each render and spun the column-resync effect without end —
  // an infinite re-render that react-virtuoso escalated into "Maximum update
  // depth exceeded" on the Issues route. The fix shares one stable empty
  // reference for the loading snapshot, so `getActorName` must be stable
  // across re-renders while the directories are unresolved.
  it("returns a referentially stable getActorName across renders during cold load", () => {
    // Directory endpoints never resolve → the queries stay pending, so the
    // hook renders repeatedly with undefined directory data (the cold-load
    // state that used to loop).
    const pending = () => new Promise<never>(() => {});
    setApiInstance({
      listMembers: pending,
      listAgents: pending,
      listSquads: pending,
    } as unknown as ApiClient);

    const { result, rerender } = renderHook(() => useActorName(), {
      wrapper: createWrapper(qc),
    });

    const first = result.current.getActorName;
    rerender();
    const second = result.current.getActorName;
    rerender();
    const third = result.current.getActorName;

    expect(second).toBe(first);
    expect(third).toBe(first);
    // A stable resolver over an empty directory still resolves gracefully.
    expect(first("member", "user-1")).toBe("Unknown");
  });

  it("resolves names once the directories are loaded", () => {
    // Seed the caches directly so the hook reads resolved directories on its
    // first render — this guards that stabilizing the loading default did not
    // break name resolution when data IS present.
    const members = [{ user_id: "user-1", name: "Ada", avatar_url: null }];
    const agents = [{ id: "agent-1", name: "Walt", avatar_url: null }];
    const squads = [{ id: "squad-1", name: "Core", avatar_url: null }];
    setApiInstance({
      listMembers: () => Promise.resolve(members),
      listAgents: () => Promise.resolve(agents),
      listSquads: () => Promise.resolve(squads),
    } as unknown as ApiClient);
    qc.setQueryData(workspaceKeys.members("ws-1"), members);
    qc.setQueryData(workspaceKeys.agents("ws-1"), agents);
    qc.setQueryData(workspaceKeys.squads("ws-1"), squads);

    const { result } = renderHook(() => useActorName(), {
      wrapper: createWrapper(qc),
    });

    expect(result.current.getActorName("member", "user-1")).toBe("Ada");
    expect(result.current.getActorName("agent", "agent-1")).toBe("Walt");
    expect(result.current.getActorName("squad", "squad-1")).toBe("Core");
  });
});
