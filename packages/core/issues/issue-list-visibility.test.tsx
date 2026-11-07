/** @vitest-environment jsdom */
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider, focusManager, useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { createQueryClient } from "../query-client";
import type { Issue, ListIssuesResponse } from "../types";
import { issueListOptions } from "./queries";
import { onIssueAuxiliaryRevision } from "./ws-updaters";

/**
 * Every open tab is its own client and receives every issue and comment event.
 * Before this gate each event that touched a loaded issue cost one list re-read
 * per open tab, hidden ones included.
 */

const WS = "ws-1";
const ISSUE_ID = "issue-1";
const REREAD = 1;
let revision = 1;

const issue = (): Issue =>
  ({
    id: ISSUE_ID,
    workspace_id: WS,
    identifier: "MUL-1",
    title: "Test",
    status: "todo",
    revision,
  }) as Issue;

const listIssues = vi.fn(
  async (): Promise<ListIssuesResponse> => ({ issues: [issue()], total: 1 }),
);

beforeEach(() => {
  revision = 1;
  listIssues.mockClear();
  setApiInstance({ listIssues } as unknown as ApiClient);
});

afterEach(() => {
  cleanup();
  focusManager.setFocused(undefined);
});

function setup() {
  const qc = createQueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const view = renderHook(() => useQuery(issueListOptions(WS)), { wrapper });
  return { qc, view };
}

const loadedRevision = (view: ReturnType<typeof setup>["view"]) =>
  view.result.current.data?.[0]?.revision;

async function setPageVisible(visible: boolean) {
  await act(async () => {
    focusManager.setFocused(visible);
  });
}

/** A comment on a loaded issue, as the comment:created handler applies it. */
async function commentCreated(qc: ReturnType<typeof createQueryClient>) {
  revision += 1;
  await act(async () => {
    onIssueAuxiliaryRevision(qc, WS, ISSUE_ID, revision);
  });
}

it("re-reads the list on every comment while the page is visible", async () => {
  const { qc, view } = setup();
  await waitFor(() => expect(loadedRevision(view)).toBe(1));

  await commentCreated(qc);
  await waitFor(() => expect(loadedRevision(view)).toBe(2));
  expect(listIssues).toHaveBeenCalledTimes(2 * REREAD);
});

it("a hidden page fetches nothing on comments and re-reads once when shown", async () => {
  const { qc, view } = setup();
  await waitFor(() => expect(loadedRevision(view)).toBe(1));
  await setPageVisible(false);

  await commentCreated(qc);
  await commentCreated(qc);
  await commentCreated(qc);
  expect(listIssues).toHaveBeenCalledTimes(REREAD);

  await setPageVisible(true);
  await waitFor(() => expect(loadedRevision(view)).toBe(4));
  expect(listIssues).toHaveBeenCalledTimes(2 * REREAD);
});

it("a page shown again after no event makes no request", async () => {
  const { view } = setup();
  await waitFor(() => expect(loadedRevision(view)).toBe(1));

  await setPageVisible(false);
  await setPageVisible(true);

  expect(listIssues).toHaveBeenCalledTimes(REREAD);
});

it("a page opened hidden fetches the list when it is first shown", async () => {
  await setPageVisible(false);
  const { view } = setup();
  expect(listIssues).not.toHaveBeenCalled();

  await setPageVisible(true);
  await waitFor(() => expect(loadedRevision(view)).toBe(1));
  expect(listIssues).toHaveBeenCalledTimes(REREAD);
});
