/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import { ApiClient } from "../api/client";
import type { IssueViewPreference } from "../api/schemas";
import { setCurrentWorkspace } from "../platform/workspace-storage";
import { createQueryClient } from "../query-client";
import { issueViewPrefKeys, useUpdateIssueViewPreference } from "./preferences";

const SCOPE = { scope_type: "sidebar_projects" } as const;

afterEach(() => {
  cleanup();
  setCurrentWorkspace(null, null);
  vi.unstubAllGlobals();
});

/** The real client against a server that answers each PUT only when told to. */
function serveHeldPuts() {
  const requests: Array<{ workspace: string | null; finish: () => void }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((_url: string, init: RequestInit) => {
      const body = JSON.parse(init.body as string);
      return new Promise<Response>((resolve) => {
        requests.push({
          workspace: new Headers(init.headers).get("X-Workspace-Slug"),
          finish: () =>
            resolve(
              new Response(JSON.stringify({ ...body, scope_id: "user-1", updated_at: "2026-10-31T00:00:00Z" }), {
                status: 200,
                headers: { "Content-Type": "application/json" },
              }),
            ),
        });
      });
    }),
  );
  setApiInstance(new ApiClient("https://api.test"));
  return requests;
}

describe("useUpdateIssueViewPreference", () => {
  it.each([
    ["the sidebar unmounts", true],
    ["the sidebar stays mounted", false],
  ])("keeps a queued write in the workspace it was made in when %s", async (_, unmount) => {
    const qc = createQueryClient();
    const requests = serveHeldPuts();
    setCurrentWorkspace("alpha", "ws-a");
    const hook = renderHook(({ wsId }) => useUpdateIssueViewPreference(wsId, SCOPE), {
      initialProps: { wsId: "ws-a" },
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={qc}>{children}</QueryClientProvider>
      ),
    });

    act(() => hook.result.current({ projects: { showCount: false } }));
    await waitFor(() => expect(requests).toHaveLength(1));
    act(() => hook.result.current({ projects: { showCount: false, onlyMine: true } }));
    await waitFor(() =>
      expect(qc.getQueryData<IssueViewPreference>(issueViewPrefKeys.scope("ws-a", SCOPE))?.prefs).toMatchObject({
        projects: { onlyMine: true },
      }),
    );

    setCurrentWorkspace("beta", "ws-b");
    if (unmount) hook.unmount();
    else hook.rerender({ wsId: "ws-b" });
    await act(async () => requests[0]!.finish());
    await waitFor(() => expect(requests).toHaveLength(2));
    await act(async () => requests[1]!.finish());
    await waitFor(() => expect(qc.isMutating()).toBe(0));

    expect(requests.map((request) => request.workspace)).toEqual(["alpha", "alpha"]);
    expect(qc.getQueryData(issueViewPrefKeys.scope("ws-b", SCOPE))).toBeUndefined();
    qc.clear();
  });
});
