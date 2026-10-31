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

const preference = (
  order: string[],
  updated_at = "2026-10-30T00:00:00Z",
  extra: Record<string, unknown> = {},
): IssueViewPreference => ({
  scope_type: "sidebar",
  scope_id: null,
  updated_at,
  prefs: { hidden: [], order, ...extra },
});

const ids = (items: { id: string }[]) => items.map((item) => item.id);

let qc: QueryClient;
let stored: IssueViewPreference;
let putIssueViewPreference: ReturnType<typeof vi.fn>;

function setup(
  saved: IssueViewPreference,
  { codeHost = "none", viewsLoad = "pending" }: { codeHost?: "none" | "github" | "vcs"; viewsLoad?: "pending" | "empty" } = {},
) {
  // The server keeps whatever was last written, as the real one does.
  stored = saved;
  putIssueViewPreference = vi.fn(async (body: { prefs: IssueViewPreference["prefs"] }) => {
    stored = preference(body.prefs.order, "2026-10-31T00:00:00Z", body.prefs);
    return stored;
  });
  setApiInstance({
    getIssueViewPreference: vi.fn(async () => stored),
    listIssueViews: vi.fn(() => (viewsLoad === "pending" ? new Promise(() => {}) : Promise.resolve([]))),
    listPins: vi.fn(async () => []),
    listGitHubInstallations: vi.fn(async () => ({
      installations: codeHost === "github" ? [{ id: "inst-1" }] : [],
    })),
    listVCSConnections: vi.fn(async () => ({
      connections: codeHost === "vcs" ? [{ id: "conn-1" }] : [],
    })),
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

describe("useSidebarViews writes", () => {
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

describe("useSidebarViews Awaiting merge", () => {
  it("is in a new user's default list once a GitHub app is installed", async () => {
    const { result } = setup(preference([], ""), { codeHost: "github", viewsLoad: "empty" });
    await waitFor(() =>
      expect(ids(result.current.items)).toEqual([
        "builtin:my_issues",
        "builtin:my_agents",
        "builtin:all_issues",
        "builtin:awaiting_merge",
      ]),
    );
  });

  it("counts a GitLab or Forgejo connection as a code host", async () => {
    const { result } = setup(preference(["builtin:my_issues"]), { codeHost: "vcs" });
    await waitFor(() => expect(ids(result.current.items)).toEqual(["builtin:my_issues", "builtin:awaiting_merge"]));
  });

  it("stays out of the sidebar without a code host, even when saved there", async () => {
    const { result } = setup(preference(["builtin:awaiting_merge", "builtin:my_issues"]));
    await waitFor(() => expect(ids(result.current.items)).toEqual(["builtin:my_issues"]));
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(ids(result.current.items)).toEqual(["builtin:my_issues"]);
  });

  it("is put into a customized order once, after the user's last built-in", async () => {
    const { result } = setup(preference(["view:a", "builtin:all_issues", "view:b"]), { codeHost: "github" });
    await waitFor(() => expect(ids(result.current.items)).toEqual(["builtin:all_issues", "builtin:awaiting_merge"]));
    // Showing it writes nothing; the next write the user makes records it.
    expect(putIssueViewPreference).not.toHaveBeenCalled();
  });

  it("does not come back after the user removes it", async () => {
    const { result } = setup(preference(["builtin:my_issues"]), { codeHost: "github" });
    await waitFor(() => expect(ids(result.current.items)).toEqual(["builtin:my_issues", "builtin:awaiting_merge"]));

    act(() => result.current.unstar("builtin:awaiting_merge"));

    await waitFor(() => expect(putIssueViewPreference).toHaveBeenCalledTimes(1));
    expect(putIssueViewPreference.mock.calls[0]![0].prefs).toMatchObject({
      order: ["builtin:my_issues"],
      introduced: ["awaiting_merge"],
    });
    // The settled write is read back from the server.
    await waitFor(() => expect(stored.prefs.introduced).toEqual(["awaiting_merge"]));
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(result.current.isStarred("builtin:awaiting_merge")).toBe(false);
    expect(ids(result.current.items)).toEqual(["builtin:my_issues"]);
  });

  it("records the introduction on any write, so a later removal sticks", async () => {
    const { result } = setup(preference(["builtin:my_issues"]), { codeHost: "github" });
    await waitFor(() => expect(ids(result.current.items)).toEqual(["builtin:my_issues", "builtin:awaiting_merge"]));

    act(() => result.current.unstar("builtin:my_issues"));

    await waitFor(() => expect(putIssueViewPreference).toHaveBeenCalledTimes(1));
    expect(putIssueViewPreference.mock.calls[0]![0].prefs).toMatchObject({
      order: ["builtin:awaiting_merge"],
      introduced: ["awaiting_merge"],
    });
  });

  it("is not recorded as introduced while the workspace cannot show it", async () => {
    const { result } = setup(preference(["builtin:my_issues", "builtin:all_issues"]));
    await waitFor(() => expect(ids(result.current.items)).toEqual(["builtin:my_issues", "builtin:all_issues"]));

    act(() => result.current.unstar("builtin:all_issues"));

    await waitFor(() => expect(putIssueViewPreference).toHaveBeenCalledTimes(1));
    expect(putIssueViewPreference.mock.calls[0]![0].prefs).toMatchObject({
      order: ["builtin:my_issues"],
      introduced: [],
    });
  });
});
