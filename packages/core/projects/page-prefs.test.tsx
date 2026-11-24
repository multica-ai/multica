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
import { projectsPagePrefsFromDoc, useProjectsPagePrefsSync } from "./page-prefs";
import { EMPTY_PROJECT_FILTERS, useProjectViewStore } from "./stores/view-store";

const doc = (prefs: Record<string, unknown>, updated_at = "2026-11-01T00:00:00Z"): IssueViewPreference => ({
  scope_type: "projects_page",
  scope_id: "me",
  updated_at,
  prefs: { hidden: [], order: [], ...prefs },
});

describe("projectsPagePrefsFromDoc", () => {
  it("keeps the fields it understands and drops the rest", () => {
    expect(
      projectsPagePrefsFromDoc({
        layout: "board",
        groupBy: "swimlanes",
        sortField: "due_date",
        sortDirection: 1,
        hiddenColumns: ["issues", "future_column", 3],
        filters: { statuses: ["planned", 2], priorities: "high" },
      }),
    ).toEqual({
      layout: "board",
      sortField: "due_date",
      hiddenColumns: ["issues"],
      filters: { statuses: ["planned"], priorities: [], leads: [] },
    });
    expect(projectsPagePrefsFromDoc({})).toEqual({});
  });
});

describe("useProjectsPagePrefsSync", () => {
  let qc: QueryClient;
  let putIssueViewPreference: ReturnType<typeof vi.fn>;

  function setup(saved: IssueViewPreference) {
    let server = saved;
    putIssueViewPreference = vi.fn(async (body: { prefs: Record<string, unknown> }) => (server = doc(body.prefs)));
    setApiInstance({
      getIssueViewPreference: vi.fn(async () => server),
      putIssueViewPreference,
    } as unknown as ApiClient);
    return renderHook(() => useProjectsPagePrefsSync("ws-1"), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={qc}>{children}</QueryClientProvider>
      ),
    });
  }

  beforeEach(() => {
    qc = createQueryClient();
    useProjectViewStore.setState({
      layout: "table",
      groupBy: "none",
      sortField: "last_activity",
      sortDirection: "desc",
      hiddenColumns: ["issues"],
      filters: EMPTY_PROJECT_FILTERS,
    });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("opens the page the way the user saved it on another device", async () => {
    setup(doc({ layout: "board", groupBy: "lead", sortField: "due_date", sortDirection: "asc" }));
    await waitFor(() => expect(useProjectViewStore.getState().layout).toBe("board"));
    const state = useProjectViewStore.getState();
    expect([state.groupBy, state.sortField, state.sortDirection]).toEqual(["lead", "due_date", "asc"]);
    expect(state.hiddenColumns).toEqual(["issues"]);
  });

  it("keeps this device's settings until a copy is saved, then saves the whole display", async () => {
    const { result } = setup(doc({}, ""));
    await waitFor(() => expect(qc.getQueryState(["issue-view-prefs", "ws-1", "projects_page", null])?.status).toBe("success"));
    expect(useProjectViewStore.getState().layout).toBe("table");

    act(() => {
      useProjectViewStore.getState().setGroupBy("priority");
      result.current();
    });

    await waitFor(() => expect(putIssueViewPreference).toHaveBeenCalledTimes(1));
    expect(putIssueViewPreference.mock.calls[0]![0]).toMatchObject({
      scope_type: "projects_page",
      prefs: {
        layout: "table",
        groupBy: "priority",
        sortField: "last_activity",
        sortDirection: "desc",
        hiddenColumns: ["issues"],
        filters: EMPTY_PROJECT_FILTERS,
      },
    });
    expect(putIssueViewPreference.mock.calls[0]![0].prefs).not.toHaveProperty("search");
  });
});
