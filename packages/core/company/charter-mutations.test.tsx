// @vitest-environment jsdom
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  useRunSquadRoutine,
  useSaveSquadCharter,
  useSetSquadRoutinePaused,
  useUnlinkSquadRoutine,
} from "./charter-mutations";
import { charterKeys } from "./charter-queries";
import { emptySquadCharter, type SquadCharter, type UpdateSquadCharterRequest } from "./charter-schema";
import { autopilotKeys } from "../autopilots/queries";

const api = vi.hoisted(() => ({
  updateSquadCharter: vi.fn(),
  deleteSquadRoutine: vi.fn(),
  triggerAutopilot: vi.fn(),
  updateAutopilot: vi.fn(),
}));

vi.mock("../api", () => ({ api }));

const WS = "ws-1";
const SQUAD = "s1";

const request: UpdateSquadCharterRequest = {
  revision: 0,
  mission: "Find gaps",
  responsibilities: [{ key: "r1", text: "Track" }],
  outputs: [],
  boundaries: "",
  escalation: "",
  intake: "",
  response_hours: 48,
};

function savedCharter(revision: number): SquadCharter {
  return { ...emptySquadCharter(SQUAD), mission: "Find gaps", revision };
}

describe("charter mutations", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.clearAllMocks();
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  function wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  }

  it("saves the charter, stores the saved revision and refetches the routines and autopilots", async () => {
    api.updateSquadCharter.mockResolvedValue(savedCharter(1));
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useSaveSquadCharter(WS, SQUAD), { wrapper });

    await act(async () => {
      await result.current.mutateAsync(request);
    });

    expect(api.updateSquadCharter).toHaveBeenCalledWith(SQUAD, request);
    expect(queryClient.getQueryData(charterKeys.charter(WS, SQUAD))).toMatchObject({ revision: 1 });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: charterKeys.routines(WS, SQUAD) });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: autopilotKeys.list(WS) });
  });

  it("keeps the cached charter and surfaces the error when a save is refused as a conflict", async () => {
    queryClient.setQueryData(charterKeys.charter(WS, SQUAD), savedCharter(4));
    const conflict = Object.assign(new Error("the charter changed"), { status: 409, body: { revision: 5 } });
    api.updateSquadCharter.mockRejectedValue(conflict);
    const { result } = renderHook(() => useSaveSquadCharter(WS, SQUAD), { wrapper });

    await act(async () => {
      await expect(result.current.mutateAsync(request)).rejects.toBe(conflict);
    });

    expect(queryClient.getQueryData(charterKeys.charter(WS, SQUAD))).toMatchObject({ revision: 4 });
  });

  it("unlinks a routine and pauses its autopilot only when asked", async () => {
    api.deleteSquadRoutine.mockResolvedValue(undefined);
    const { result } = renderHook(() => useUnlinkSquadRoutine(WS, SQUAD), { wrapper });

    await act(async () => {
      await result.current.mutateAsync({ autopilotId: "ap1", pause: true });
    });

    expect(api.deleteSquadRoutine).toHaveBeenCalledWith(SQUAD, "ap1", { pause: true });
  });

  it("runs a routine through the autopilot trigger", async () => {
    api.triggerAutopilot.mockResolvedValue({ id: "run1" });
    const { result } = renderHook(() => useRunSquadRoutine(WS, SQUAD), { wrapper });

    await act(async () => {
      await result.current.mutateAsync("ap1");
    });

    expect(api.triggerAutopilot).toHaveBeenCalledWith("ap1");
  });

  it("pauses and resumes the routine's autopilot by its status", async () => {
    api.updateAutopilot.mockResolvedValue({ id: "ap1" });
    const { result } = renderHook(() => useSetSquadRoutinePaused(WS, SQUAD), { wrapper });

    await act(async () => {
      await result.current.mutateAsync({ autopilotId: "ap1", paused: true });
    });
    expect(api.updateAutopilot).toHaveBeenLastCalledWith("ap1", { status: "paused" });

    await act(async () => {
      await result.current.mutateAsync({ autopilotId: "ap1", paused: false });
    });
    expect(api.updateAutopilot).toHaveBeenLastCalledWith("ap1", { status: "active" });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
  });
});
