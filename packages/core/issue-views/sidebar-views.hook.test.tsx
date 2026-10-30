/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { IssueViewPreference } from "../api/schemas";
import { createQueryClient } from "../query-client";
import { useSidebarViews } from "./sidebar-views";

const preference = (order: string[], updated_at = "2026-10-30T00:00:00Z"): IssueViewPreference => ({
  scope_type: "sidebar",
  scope_id: null,
  updated_at,
  prefs: { hidden: [], order },
});

describe("useSidebarViews writes", () => {
  let qc: QueryClient;
  let putIssueViewPreference: ReturnType<typeof vi.fn>;

  function setup(saved: IssueViewPreference) {
    putIssueViewPreference = vi.fn(async (body: { prefs: { order: string[] } }) => preference(body.prefs.order));
    setApiInstance({
      getIssueViewPreference: vi.fn(async () => saved),
      // Saved views never arrive: the directory is still loading.
      listIssueViews: vi.fn(() => new Promise(() => {})),
      listPins: vi.fn(async () => []),
      putIssueViewPreference,
    } as unknown as ApiClient);
    return renderHook(() => useSidebarViews("ws-1", "me"), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={qc}>{children}</QueryClientProvider>
      ),
    });
  }

  beforeEach(() => {
    qc = createQueryClient();
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("keeps starred views that have not loaded when unstarring a built-in", async () => {
    const { result } = setup(preference(["builtin:my_issues", "view:a", "builtin:all_issues", "view:b"]));
    await waitFor(() => expect(result.current.isStarred("view:a")).toBe(true));
    expect(result.current.viewsPending).toBe(true);

    act(() => result.current.unstar("builtin:all_issues"));

    await waitFor(() => expect(putIssueViewPreference).toHaveBeenCalledTimes(1));
    expect(putIssueViewPreference.mock.calls[0]![0].prefs.order).toEqual([
      "builtin:my_issues",
      "view:a",
      "view:b",
    ]);
  });

  it("does not write a default order before the views behind it have loaded", async () => {
    const { result } = setup(preference([], ""));
    await waitFor(() => expect(result.current.isStarred("builtin:all_issues")).toBe(true));

    act(() => result.current.unstar("builtin:all_issues"));
    act(() => result.current.star("view:a"));

    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(putIssueViewPreference).not.toHaveBeenCalled();
  });
});
