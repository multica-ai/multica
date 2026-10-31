/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { IssueViewPreferenceSchema, type IssueViewPreference } from "../api/schemas";
import { createQueryClient } from "../query-client";
import { sidebarProjects } from "./sidebar-list";
import {
  sidebarProjectPrefsFromDoc,
  useSidebarProjectPrefs,
  useUpdateSidebarProjectPrefs,
} from "./sidebar-project-prefs";
import {
  DEFAULT_SIDEBAR_ISSUE_SETTINGS,
  DEFAULT_SIDEBAR_PROJECT_SETTINGS,
  useSidebarTreeStore,
} from "./stores/sidebar-tree-store";

const doc = (prefs: Record<string, unknown>, updated_at = "2026-10-31T00:00:00Z"): IssueViewPreference => ({
  scope_type: "sidebar_projects",
  scope_id: "me",
  updated_at,
  prefs: { hidden: [], order: [], ...prefs },
});

type PutBody = { prefs: Record<string, unknown> };

describe("sidebar project prefs", () => {
  let qc: QueryClient;
  let getIssueViewPreference: ReturnType<typeof vi.fn>;
  let putIssueViewPreference: ReturnType<typeof vi.fn>;

  function setup(get: () => Promise<IssueViewPreference>, put = async (body: PutBody) => doc(body.prefs)) {
    getIssueViewPreference = vi.fn(get);
    putIssueViewPreference = vi.fn(put);
    setApiInstance({ getIssueViewPreference, putIssueViewPreference } as unknown as ApiClient);
    return renderHook(
      () => ({ prefs: useSidebarProjectPrefs("ws-1"), update: useUpdateSidebarProjectPrefs("ws-1") }),
      {
        wrapper: ({ children }: { children: ReactNode }) => (
          <QueryClientProvider client={qc}>{children}</QueryClientProvider>
        ),
      },
    );
  }

  beforeEach(() => {
    qc = createQueryClient();
    useSidebarTreeStore.setState({
      projectSettings: { ...DEFAULT_SIDEBAR_PROJECT_SETTINGS, showCount: false },
      issueSettings: DEFAULT_SIDEBAR_ISSUE_SETTINGS,
      projectOrder: ["p2"],
    });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("keeps this device's earlier settings until the user's copy is first saved", async () => {
    let server = doc({}, "");
    const { result } = setup(
      async () => server,
      async (body) => (server = doc(body.prefs)),
    );
    await waitFor(() => expect(result.current.prefs.loaded).toBe(true));
    expect(result.current.prefs.projectSettings.showCount).toBe(false);
    expect(result.current.prefs.projectOrder).toEqual(["p2"]);

    act(() => result.current.update({ issueSettings: { sortBy: "priority" } }));

    await waitFor(() => expect(putIssueViewPreference).toHaveBeenCalledTimes(1));
    expect(putIssueViewPreference.mock.calls[0]![0].prefs).toEqual({
      projects: { ...DEFAULT_SIDEBAR_PROJECT_SETTINGS, showCount: false },
      issues: { ...DEFAULT_SIDEBAR_ISSUE_SETTINGS, sortBy: "priority" },
      order: ["p2"],
    });
    await waitFor(() => expect(result.current.prefs.issueSettings.sortBy).toBe("priority"));
  });

  it("follows the saved copy over this device's settings, filling in settings it predates", async () => {
    const { result } = setup(async () => doc({ projects: { ordering: "name" }, order: ["p1"] }));
    await waitFor(() => expect(result.current.prefs.projectOrder).toEqual(["p1"]));
    expect(result.current.prefs.projectSettings).toEqual({ ...DEFAULT_SIDEBAR_PROJECT_SETTINGS, ordering: "name" });
    expect(result.current.prefs.issueSettings).toEqual(DEFAULT_SIDEBAR_ISSUE_SETTINGS);
  });

  it("does not write before the saved copy has loaded", async () => {
    const { result } = setup(() => new Promise(() => {}));

    act(() => result.current.update({ projectSettings: { showCount: true } }));

    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(putIssueViewPreference).not.toHaveBeenCalled();
  });

  it("sends a quick second change after the first lands, so the newer document is saved last", async () => {
    let server = doc({ order: [] });
    const pending: Array<() => void> = [];
    const { result } = setup(
      async () => server,
      (body) =>
        new Promise((resolve) => {
          pending.push(() => {
            server = doc(body.prefs);
            resolve(server);
          });
        }),
    );
    await waitFor(() => expect(result.current.prefs.loaded).toBe(true));

    act(() => result.current.update({ projectSettings: { showCount: false } }));
    await waitFor(() => expect(result.current.prefs.projectSettings.showCount).toBe(false));
    act(() => result.current.update({ issueSettings: { assignee: "me" } }));
    await waitFor(() => expect(result.current.prefs.issueSettings.assignee).toBe("me"));
    expect(putIssueViewPreference).toHaveBeenCalledTimes(1);

    await act(async () => pending[0]!());
    await waitFor(() => expect(putIssueViewPreference).toHaveBeenCalledTimes(2));
    expect(putIssueViewPreference.mock.calls[1]![0].prefs).toMatchObject({
      projects: { showCount: false },
      issues: { assignee: "me" },
    });
    expect(getIssueViewPreference).toHaveBeenCalledTimes(1);
    expect(result.current.prefs.issueSettings.assignee).toBe("me");

    await act(async () => pending[1]!());
    await waitFor(() => expect(getIssueViewPreference).toHaveBeenCalledTimes(2));
    expect(server.prefs).toMatchObject({ projects: { showCount: false }, issues: { assignee: "me" } });
    expect(result.current.prefs.issueSettings.assignee).toBe("me");
    expect(result.current.prefs.projectSettings.showCount).toBe(false);
  });

  it("falls back to defaults for settings this client does not know", async () => {
    const { result } = setup(async () =>
      IssueViewPreferenceSchema.parse(
        doc({
          projects: { ordering: "future-order", statuses: ["planned", "archived"], showCount: "yes" },
          issues: { sortBy: "magic", hiddenStatuses: null, assignee: 7 },
          order: ["p1"],
        }),
      ),
    );
    await waitFor(() => expect(result.current.prefs.projectOrder).toEqual(["p1"]));
    expect(result.current.prefs.projectSettings).toEqual({ ...DEFAULT_SIDEBAR_PROJECT_SETTINGS, statuses: ["planned"] });
    expect(result.current.prefs.issueSettings).toEqual({ ...DEFAULT_SIDEBAR_ISSUE_SETTINGS, hiddenStatuses: null });
    const { projectSettings, projectOrder } = result.current.prefs;
    expect(sidebarProjects([], projectSettings, projectOrder, "u1")).toEqual([]);
  });

  it.each([null, "settings", 42, [1, 2]])("falls back to defaults when the document holds %j", (value) => {
    expect(sidebarProjectPrefsFromDoc({ projects: value, issues: value, order: value })).toEqual({
      projectSettings: DEFAULT_SIDEBAR_PROJECT_SETTINGS,
      issueSettings: DEFAULT_SIDEBAR_ISSUE_SETTINGS,
      projectOrder: [],
    });
  });
});
