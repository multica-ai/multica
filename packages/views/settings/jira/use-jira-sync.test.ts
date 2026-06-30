import { describe, expect, it, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";

const mockAuthState = vi.hoisted(() => ({
  user: { id: "m1", email: "me@acme.com" } as { id: string; email: string } | null,
}));

const mockSync = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (sel?: (s: typeof mockAuthState) => unknown) => (sel ? sel(mockAuthState) : mockAuthState),
    { getState: () => mockAuthState },
  );
  return { useAuthStore };
});

vi.mock("@multica/core/api", () => ({ api: {} }));

vi.mock("@multica/core/jira", () => ({ syncJiraIssues: mockSync }));

import { useJiraSync } from "./use-jira-sync";

describe("useJiraSync", () => {
  beforeEach(() => {
    mockSync.mockReset();
    mockAuthState.user = { id: "m1", email: "me@acme.com" };
    (globalThis as unknown as { window: { jiraAPI: unknown } }).window.jiraAPI = {
      request: vi.fn().mockResolvedValue({}),
      getConfig: vi.fn().mockResolvedValue({
        siteUrl: "https://acme.atlassian.net",
        email: "me@acme.com",
        hasToken: true,
        apiToken: "",
        jql: "assignee = currentUser()",
        statusMapping: {},
        pollIntervalMinutes: 0,
      }),
      setConfig: vi.fn(),
      onPollTick: vi.fn(),
    };
  });

  it("runs a sync and returns the result", async () => {
    mockSync.mockResolvedValue({ created: 2, updated: 0, skipped: 0, commentsAdded: 0, errors: [] });
    const { result } = renderHook(() => useJiraSync());
    let res: Awaited<ReturnType<typeof result.current.syncNow>>;
    await act(async () => {
      res = await result.current.syncNow();
    });
    expect(res!.created).toBe(2);
    expect(result.current.lastResult?.created).toBe(2);
    // The sync engine is called with the current member id from the auth store.
    expect(mockSync.mock.calls[0]![0].currentMemberId).toBe("m1");
  });

  it("sets an error and skips syncing when no jiraAPI is present", async () => {
    delete (globalThis as unknown as { window: { jiraAPI?: unknown } }).window.jiraAPI;
    const { result } = renderHook(() => useJiraSync());
    await act(async () => {
      await result.current.syncNow();
    });
    expect(result.current.error).toMatch(/desktop app/i);
    expect(mockSync).not.toHaveBeenCalled();
  });
});
