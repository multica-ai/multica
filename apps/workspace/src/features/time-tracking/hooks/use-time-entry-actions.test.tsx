import React from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, act } from "@testing-library/react";
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { api, TimeEntryOverlapApiError } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import type { CreateTimeEntryRequest, SwitchTimeEntryRequest, TimeEntry, UpdateTimeEntryRequest } from "@/shared/types";
import { useTimeEntryActions } from "./use-time-entry-actions";

vi.mock("@/features/workspace", () => ({
  useWorkspaceStore: (selector: (state: { workspace: { id: string } | null }) => unknown) =>
    selector({ workspace: { id: "ws-1" } }),
}));

function makeEntry(overrides: Partial<TimeEntry> = {}): TimeEntry {
  return {
    id: "time-entry-1",
    workspace_id: "ws-1",
    user_id: "user-1",
    issue_id: null,
    description: null,
    start_time: "2026-05-17T09:00:00Z",
    stop_time: null,
    duration_seconds: 0,
    type: "manual",
    labels: [],
    created_at: "2026-05-17T09:00:00Z",
    updated_at: "2026-05-17T09:00:00Z",
    ...overrides,
  };
}

function makeSwitchRequest(overrides: Partial<SwitchTimeEntryRequest> = {}): SwitchTimeEntryRequest {
  return {
    description: "New work",
    issue_id: "issue-1",
    label_ids: ["label-1"],
    start_time: "2026-05-17T10:00:00Z",
    ...overrides,
  };
}

function makeCreateRequest(overrides: Partial<CreateTimeEntryRequest> = {}): CreateTimeEntryRequest {
  return {
    description: "Historical work",
    issue_id: null,
    label_ids: ["label-1"],
    start_time: "2026-05-17T09:00:00Z",
    stop_time: "2026-05-17T10:00:00Z",
    ...overrides,
  };
}

function makeUpdateRequest(overrides: Partial<UpdateTimeEntryRequest> = {}): UpdateTimeEntryRequest {
  return {
    description: "Updated work",
    issue_id: "issue-2",
    label_ids: ["label-2"],
    ...overrides,
  };
}

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

function renderActions(args?: Parameters<typeof useTimeEntryActions>[0]) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const wrapper = createWrapper(queryClient);
  const rendered = renderHook(() => useTimeEntryActions(args), { wrapper });
  return { ...rendered, queryClient };
}

describe("useTimeEntryActions", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("stores a pending switch when a timer is already running", async () => {
    const { result } = renderActions({ currentEntry: makeEntry({ stop_time: null }) });
    const request = makeSwitchRequest();
    const startSpy = vi.spyOn(api, "startTimeEntry");

    await act(async () => {
      await result.current.requestStart(request);
    });

    expect(result.current.pendingSwitch).toEqual(request);
    expect(startSpy).not.toHaveBeenCalled();
  });

  it("starts immediately when no timer is running", async () => {
    const { result } = renderActions({ currentEntry: null });
    const started = makeEntry({ stop_time: null });
    const startSpy = vi.spyOn(api, "startTimeEntry").mockResolvedValue(started);
    const request = makeSwitchRequest();

    await act(async () => {
      await expect(result.current.requestStart(request)).resolves.toEqual(started);
    });

    expect(result.current.pendingSwitch).toBeNull();
    expect(startSpy).toHaveBeenCalledWith(request);
  });

  it("confirms a pending switch and clears the staged request", async () => {
    const { result } = renderActions({ currentEntry: makeEntry({ stop_time: null }) });
    const request = makeSwitchRequest();
    const switched = makeEntry({ id: "time-entry-2", stop_time: null, start_time: request.start_time });
    const switchSpy = vi.spyOn(api, "switchTimeEntry").mockResolvedValue(switched);

    await act(async () => {
      await result.current.requestStart(request);
    });

    await act(async () => {
      await expect(result.current.confirmSwitch()).resolves.toEqual(switched);
    });

    expect(switchSpy).toHaveBeenCalledWith(request);
    expect(result.current.pendingSwitch).toBeNull();
  });

  it("returns null when confirming without a pending switch", async () => {
    const { result } = renderActions({ currentEntry: null });
    const switchSpy = vi.spyOn(api, "switchTimeEntry");

    await expect(result.current.confirmSwitch()).resolves.toBeNull();

    expect(switchSpy).not.toHaveBeenCalled();
  });

  it("overwrites an already pending switch request with the latest start request", async () => {
    const { result } = renderActions({ currentEntry: makeEntry({ stop_time: null }) });
    const firstRequest = makeSwitchRequest({ description: "First work" });
    const secondRequest = makeSwitchRequest({ description: "Second work" });

    await act(async () => {
      await result.current.requestStart(firstRequest);
    });

    await act(async () => {
      await result.current.requestStart(secondRequest);
    });

    expect(result.current.pendingSwitch).toEqual(secondRequest);
  });

  it("exposes setPendingSwitch so callers can clear a staged request", async () => {
    const { result } = renderActions({ currentEntry: makeEntry({ stop_time: null }) });
    const request = makeSwitchRequest();

    await act(async () => {
      await result.current.requestStart(request);
    });

    expect(result.current.setPendingSwitch).toBeTypeOf("function");

    act(() => {
      result.current.setPendingSwitch(null);
    });

    expect(result.current.pendingSwitch).toBeNull();
  });

  it("preserves typed overlap errors when creating a historical entry", async () => {
    const { result } = renderActions();
    const error = new TimeEntryOverlapApiError({
      error: "time entry overlaps an existing entry",
      code: "time_entry_overlap",
      conflicts: [
        {
          id: "time-entry-2",
          description: "Overlap source",
          start_time: "2026-05-17T09:30:00Z",
          stop_time: "2026-05-17T10:30:00Z",
          issue_id: null,
        },
      ],
    });
    vi.spyOn(api, "startTimeEntry").mockRejectedValue(error);

    await expect(
      result.current.createHistoricalEntry(makeCreateRequest()),
    ).rejects.toBe(error);
  });

  it("preserves typed overlap errors when updating a historical entry", async () => {
    const { result } = renderActions();
    const error = new TimeEntryOverlapApiError({
      error: "time entry overlaps an existing entry",
      code: "time_entry_overlap",
      conflicts: [
        {
          id: "time-entry-2",
          description: "Overlap source",
          start_time: "2026-05-17T09:30:00Z",
          stop_time: "2026-05-17T10:30:00Z",
          issue_id: null,
        },
      ],
    });
    vi.spyOn(api, "updateTimeEntry").mockRejectedValue(error);

    await expect(
      result.current.updateTimeEntry("time-entry-1", makeUpdateRequest()),
    ).rejects.toBe(error);
  });
});

describe("useTimeEntryActions delete with undo", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("optimistically removes the entry from cached lists while the undo window is open", () => {
    const entry = makeEntry({ id: "entry-1", issue_id: "issue-1", stop_time: "2026-05-17T10:00:00Z" });
    const { result, queryClient } = renderActions();

    queryClient.setQueryData(queryKeys.timeTracking.entriesParams("ws-1", { limit: 50 }), [entry]);
    queryClient.setQueryData(queryKeys.timeTracking.issueEntries("issue-1"), [entry]);

    act(() => {
      result.current.requestDelete(entry, "issue-1");
    });

    expect(
      queryClient.getQueryData<TimeEntry[]>(queryKeys.timeTracking.entriesParams("ws-1", { limit: 50 })),
    ).toEqual([]);
    expect(
      queryClient.getQueryData<TimeEntry[]>(queryKeys.timeTracking.issueEntries("issue-1")),
    ).toEqual([]);
  });

  it("waits for the undo window before sending the delete request", async () => {
    const { result } = renderActions();
    const entry = makeEntry({ id: "entry-1", stop_time: "2026-05-17T10:00:00Z" });
    const deleteSpy = vi.spyOn(api, "deleteTimeEntry").mockResolvedValue(undefined);

    act(() => {
      result.current.requestDelete(entry);
    });

    expect(deleteSpy).not.toHaveBeenCalled();

    await act(async () => {
      vi.advanceTimersByTime(5000);
      await Promise.resolve();
    });

    expect(deleteSpy).toHaveBeenCalledWith("entry-1");
  });

  it("undoes a staged delete before the timeout fires and restores cached entries", () => {
    const entry = makeEntry({ id: "entry-1", issue_id: "issue-1", stop_time: "2026-05-17T10:00:00Z" });
    const { result, queryClient } = renderActions();
    const deleteSpy = vi.spyOn(api, "deleteTimeEntry");

    queryClient.setQueryData(queryKeys.timeTracking.entriesParams("ws-1", { limit: 50 }), [entry]);
    queryClient.setQueryData(queryKeys.timeTracking.issueEntries("issue-1"), [entry]);

    act(() => {
      result.current.requestDelete(entry, "issue-1");
    });

    act(() => {
      result.current.undoDelete(entry);
    });

    expect(deleteSpy).not.toHaveBeenCalled();
    expect(
      queryClient.getQueryData<TimeEntry[]>(queryKeys.timeTracking.entriesParams("ws-1", { limit: 50 })),
    ).toEqual([entry]);
    expect(
      queryClient.getQueryData<TimeEntry[]>(queryKeys.timeTracking.issueEntries("issue-1")),
    ).toEqual([entry]);
  });

  it("still commits the delete after the hook owner unmounts", async () => {
    const entry = makeEntry({ id: "entry-1", stop_time: "2026-05-17T10:00:00Z" });
    const deleteSpy = vi.spyOn(api, "deleteTimeEntry").mockResolvedValue(undefined);
    const { result, unmount } = renderActions();

    act(() => {
      result.current.requestDelete(entry);
    });

    unmount();

    await act(async () => {
      vi.advanceTimersByTime(5000);
      await Promise.resolve();
    });

    expect(deleteSpy).toHaveBeenCalledWith("entry-1");
  });

  it("passes issueId to requestDelete so cache invalidation can target the issue entry list", () => {
    const { result } = renderActions();
    const entry = makeEntry({ id: "entry-1", issue_id: "issue-1", stop_time: "2026-05-17T10:00:00Z" });

    act(() => {
      result.current.requestDelete(entry, "issue-1");
    });

    expect(result.current.pendingDelete).toEqual({ entry, issueId: "issue-1" });
  });
});

describe("ApiClient time entry overlap handling", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("throws a typed overlap error for structured 409 responses", async () => {
    const response = new Response(
      JSON.stringify({
        error: "time entry overlaps an existing entry",
        code: "time_entry_overlap",
        conflicts: [
          {
            id: "time-entry-2",
            description: "Overlap source",
            start_time: "2026-05-17T09:30:00Z",
            stop_time: "2026-05-17T10:30:00Z",
            issue_id: null,
          },
        ],
      }),
      {
        status: 409,
        headers: {
          "Content-Type": "application/json",
        },
      },
    );
    vi.spyOn(globalThis, "fetch").mockResolvedValue(response as never);

    await expect(api.startTimeEntry(makeCreateRequest())).rejects.toBeInstanceOf(TimeEntryOverlapApiError);
  });
});
