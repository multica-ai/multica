/**
 * @vitest-environment jsdom
 */
import { act, renderHook, waitFor } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { issueKeys } from "@multica/core/issues/queries";
import { useIssueSurfaceActions } from "./use-issue-surface-actions";

const mutate = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutate, isPending: false }),
  useBatchUpdateIssues: () => ({ isPending: false }),
  useBatchDeleteIssues: () => ({ isPending: false }),
}));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
vi.mock("../../i18n", () => ({ useT: () => ({ t: () => "translated" }) }));

const ROWS_KEY = [...issueKeys.tableAll("ws-1"), "rows"];
const MOVE = { position: 1, before_id: null, after_id: null };

describe("useIssueSurfaceActions moveIssue", () => {
  let qc: QueryClient;
  let landings: Array<() => void>;

  beforeEach(() => {
    mutate.mockReset();
    landings = [];
    qc = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    qc.setQueryData(ROWS_KEY, "before");
  });

  function setup() {
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    );
    return renderHook(
      () => {
        useQuery({
          queryKey: ROWS_KEY,
          queryFn: () =>
            new Promise<string>((resolve) => {
              landings.push(() => resolve("after"));
            }),
        });
        return useIssueSurfaceActions({ createDefaults: {} as never });
      },
      { wrapper },
    );
  }

  /** What the update mutation does on settle before its caller's callback. */
  const refetchRows = () => {
    void qc.invalidateQueries({ queryKey: issueKeys.tableAll("ws-1") });
  };
  const settleMove = (call: number, err: Error | null = null) =>
    mutate.mock.calls[call]![1].onSettled(undefined, err);
  const flush = () => act(() => new Promise((r) => setTimeout(r, 20)));

  it("reports a move settled only once the refetched rows land", async () => {
    const { result } = setup();
    const onSettled = vi.fn();
    act(() => result.current.moveIssue("issue-1", MOVE, onSettled));
    act(() => {
      refetchRows();
      settleMove(0);
    });
    await flush();
    expect(onSettled).not.toHaveBeenCalled();
    expect(landings).toHaveLength(1);

    act(() => landings[0]!());
    await waitFor(() => expect(onSettled).toHaveBeenCalledTimes(1));
    expect(qc.getQueryData(ROWS_KEY)).toBe("after");
  });

  it("keeps waiting when a realtime echo restarts the refetch", async () => {
    const { result } = setup();
    const onSettled = vi.fn();
    act(() => result.current.moveIssue("issue-1", MOVE, onSettled));
    act(() => {
      refetchRows();
      settleMove(0);
    });
    act(refetchRows);
    await flush();
    act(refetchRows);
    await flush();
    expect(onSettled).not.toHaveBeenCalled();

    act(() => landings.at(-1)!());
    await waitFor(() => expect(onSettled).toHaveBeenCalledTimes(1));
  });

  it("reports a failed move settled at once, so the card goes back", async () => {
    const { result } = setup();
    const onSettled = vi.fn();
    act(() => result.current.moveIssue("issue-1", MOVE, onSettled));
    act(() => {
      refetchRows();
      settleMove(0, new Error("nope"));
    });
    expect(onSettled).toHaveBeenCalledTimes(1);
  });

  it("leaves settling to the newer move when one starts during the refetch", async () => {
    const { result } = setup();
    const first = vi.fn();
    const second = vi.fn();
    act(() => result.current.moveIssue("issue-1", MOVE, first));
    act(() => {
      refetchRows();
      settleMove(0);
    });
    act(() => result.current.moveIssue("issue-2", MOVE, second));
    // The newer move's optimistic patch cancels the first one's refetch.
    await act(() => qc.cancelQueries({ queryKey: issueKeys.tableAll("ws-1") }));
    await flush();
    expect(first).not.toHaveBeenCalled();

    act(() => {
      refetchRows();
      settleMove(1);
    });
    act(() => landings.at(-1)!());
    await waitFor(() => expect(second).toHaveBeenCalledTimes(1));
    expect(first).not.toHaveBeenCalled();
  });
});
