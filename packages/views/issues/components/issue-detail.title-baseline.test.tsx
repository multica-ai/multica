import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { createChatStore, registerChatStore } from "@multica/core/chat";
import { I18nProvider } from "@multica/core/i18n/react";
import { issueKeys } from "@multica/core/issues/queries";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { setCurrentWorkspace } from "@multica/core/platform";
import { WSProvider } from "@multica/core/realtime";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import type { Issue, StorageAdapter, UpdateIssueRequest } from "@multica/core/types";
import { NavigationProvider } from "../../navigation";
import { RESOURCES } from "../../locales";
import { IssueDetail } from "./issue-detail";

const origin = "http://127.0.0.1:1";
const wsId = "title-test-workspace";
const issueId = "title-test-issue";
const userId = "title-test-user";
const storage: StorageAdapter = {
  getItem: () => null,
  setItem: () => {},
  removeItem: () => {},
  keys: () => [],
};

let issue: Issue;
let qc: QueryClient;
let writes: { data: UpdateIssueRequest; status: number }[];
let holdSave: boolean;
let releaseSave: (() => void) | undefined;

function response(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

beforeAll(() => {
  // jsdom has no layout; the real ProseMirror instance measures its caret.
  const rects = () => ({ length: 0, item: () => null, [Symbol.iterator]: function* () {} }) as DOMRectList;
  Range.prototype.getBoundingClientRect = () => new DOMRect();
  Range.prototype.getClientRects = rects;
  HTMLElement.prototype.getClientRects = rects;
  Object.defineProperty(Text.prototype, "getClientRects", { configurable: true, value: rects });
  registerChatStore(createChatStore({ storage }));
});

beforeEach(() => {
  issue = {
    id: issueId, workspace_id: wsId, number: 1, identifier: "TITLE-1",
    title: "Original title", description: null, status: "backlog", priority: "none",
    assignee_type: null, assignee_id: null, creator_type: "member", creator_id: userId,
    parent_issue_id: null, project_id: null, position: 0, stage: null,
    start_date: null, due_date: null, metadata: {}, properties: {}, revision: 1,
    created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
  };
  writes = [];
  holdSave = false;
  releaseSave = undefined;
  qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  // Keep the real IssueDetail, editors, queries, mutation and ApiClient. Only
  // the HTTP transport supplies synthetic server data and the title CAS rule.
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = new URL(String(input));
    if (url.origin !== origin) throw new Error(`Unexpected origin: ${url.origin}`);
    const path = url.pathname;
    if (path === `/api/issues/${issueId}` && init?.method === "PUT") {
      const data = JSON.parse(String(init.body)) as UpdateIssueRequest;
      const conflict = issue.title !== data.title_base && issue.title !== data.title;
      writes.push({ data, status: conflict ? 409 : 200 });
      if (conflict) return response({ error: "Changed elsewhere", code: "revision_conflict" }, 409);
      issue = { ...issue, title: data.title!, revision: issue.revision! + 1 };
      const saved = { ...issue };
      if (holdSave) return new Promise<Response>((resolve) => {
        releaseSave = () => resolve(response(saved));
      });
      return response(saved);
    }
    if (init?.method && init.method !== "GET") throw new Error(`Unexpected method: ${init.method}`);
    if (path === `/api/issues/${issueId}`) return response(issue);
    const routes: Record<string, unknown> = {
      "/api/workspaces": [{ id: wsId, name: "Title test", slug: "title-test", created_at: issue.created_at }],
      [`/api/workspaces/${wsId}/members`]: [{ user_id: userId, name: "Title test", role: "owner" }],
      "/api/issues": { issues: [issue], total: 1 },
      "/api/agents": [], "/api/squads": [], "/api/issue-statuses": { statuses: [] },
      "/api/labels": [], "/api/properties": { properties: [], total: 0 },
      "/api/projects": { projects: [], total: 0 }, "/api/issue-workflows": [],
      "/api/assignee-frequency": [], "/api/quick-actions": { quick_actions: [] },
      "/api/pins": [], "/api/issues/unread": [],
      "/api/inbox/archived/page": { items: [], has_more: false, next_cursor: null },
      [`/api/issues/${issueId}/timeline`]: [], [`/api/issues/${issueId}/task-runs`]: [],
      [`/api/issues/${issueId}/children`]: { issues: [], total: 0 },
      [`/api/issues/${issueId}/attachments`]: [], [`/api/issues/${issueId}/labels`]: [],
      [`/api/issues/${issueId}/subscribers`]: [], [`/api/issues/${issueId}/wakeups`]: [],
      [`/api/issues/${issueId}/system-wakeups`]: [],
      [`/api/issues/${issueId}/duplicates`]: { duplicate_of: null, duplicates: [] },
    };
    if (!(path in routes)) throw new Error(`Unexpected request: ${path}`);
    return response(routes[path]);
  });
});

afterEach(async () => {
  releaseSave?.();
  cleanup();
  await qc.cancelQueries();
  qc.clear();
  setCurrentWorkspace(null, null);
  vi.restoreAllMocks();
});

async function openTitle() {
  const api = new ApiClient(origin);
  setApiInstance(api);
  const auth = createAuthStore({ api, storage });
  registerAuthStore(auth);
  auth.getState().setUser({
    id: userId, email: "title@example.invalid", name: "Title test",
    avatar_url: null, onboarded_at: null, onboarding_questionnaire: {}, language: null,
    profile_description: "", timezone: null, created_at: issue.created_at, updated_at: issue.updated_at,
  });
  setCurrentWorkspace("title-test", wsId);
  await qc.ensureQueryData(workspaceListOptions());
  const navigation = {
    push: () => {}, replace: () => {}, back: () => {},
    pathname: `/title-test/issues/${issueId}`, searchParams: new URLSearchParams(), hash: "",
    getShareableUrl: (path: string) => origin + path,
  };
  render(
    <I18nProvider locale="en" resources={RESOURCES}>
      <QueryClientProvider client={qc}>
        <WSProvider wsUrl="ws://127.0.0.1:1/ws" authStore={auth} storage={storage}>
          <WorkspaceSlugProvider slug="title-test">
            <NavigationProvider value={navigation}>
              <IssueDetail issueId={issueId} variant="peek" readsInbox={false} />
            </NavigationProvider>
          </WorkspaceSlugProvider>
        </WSProvider>
      </QueryClientProvider>
    </I18nProvider>,
  );
  fireEvent.keyDown(await screen.findByRole("button", { name: "Original title" }), { key: "Enter" });
  await waitFor(() => expect(document.querySelector(".title-editor")).not.toBeNull());
  const editor = document.querySelector<HTMLElement>(".title-editor")!;
  act(() => editor.focus());
  return editor;
}

async function typeTitle(editor: HTMLElement, text: string) {
  // Let the real ProseMirror DOM observer consume a contenteditable input.
  await act(async () => {
    editor.querySelector("p")!.textContent = text;
    fireEvent.input(editor, { inputType: "insertText", data: text });
  });
}

async function remoteTitle(title: string) {
  issue = { ...issue, title, revision: issue.revision! + 1 };
  await act(async () => { await qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) }); });
}

async function blurTitle(editor: HTMLElement, count: number) {
  act(() => editor.blur());
  await waitFor(() => expect(writes).toHaveLength(count));
  await waitFor(() => expect(qc.isMutating()).toBe(0));
}

describe("IssueDetail title baselines (real editor and data hooks)", () => {
  it("saves edits against an external title adopted while focused and clean", async () => {
    const editor = await openTitle();
    await remoteTitle("Remote title");
    await waitFor(() => expect(editor).toHaveTextContent("Remote title"));
    expect(editor).toHaveFocus();
    await typeTitle(editor, "Based on remote");
    await blurTitle(editor, 1);
    expect(writes[0]).toEqual({ data: { title: "Based on remote", title_base: "Remote title" }, status: 200 });
  });

  it("advances the baseline after its own successful save", async () => {
    const editor = await openTitle();
    await typeTitle(editor, "First edit");
    await blurTitle(editor, 1);
    act(() => editor.focus());
    await typeTitle(editor, "Second edit");
    await blurTitle(editor, 2);
    expect(writes.map((w) => w.data.title_base)).toEqual(["Original title", "First edit"]);
    expect(writes.every((w) => w.status === 200)).toBe(true);
  });

  it("keeps a dirty draft's old baseline and discards it without a write", async () => {
    const editor = await openTitle();
    await typeTitle(editor, "Dirty draft");
    await remoteTitle("Remote title");
    expect(editor).toHaveTextContent("Dirty draft");
    await blurTitle(editor, 1);
    expect(writes[0]).toEqual({ data: { title: "Dirty draft", title_base: "Original title" }, status: 409 });
    fireEvent.click(await screen.findByRole("button", { name: "Use the latest version" }));
    await waitFor(() => expect(document.querySelector(".title-editor")).toHaveTextContent("Remote title"));
    expect(writes).toHaveLength(1);
  });

  it("rebases an explicitly kept conflict draft onto the current server title", async () => {
    const editor = await openTitle();
    await typeTitle(editor, "Dirty draft");
    await remoteTitle("Remote title");
    await blurTitle(editor, 1);
    fireEvent.click(await screen.findByRole("button", { name: "Keep my version" }));
    await waitFor(() => expect(writes).toHaveLength(2));
    expect(writes[1]).toEqual({ data: { title: "Dirty draft", title_base: "Remote title" }, status: 200 });
  });

  it("keeps a newer adopted baseline when an older save response arrives", async () => {
    const editor = await openTitle();
    holdSave = true;
    await typeTitle(editor, "Own save");
    act(() => editor.blur());
    await waitFor(() => expect(releaseSave).toBeDefined());
    await remoteTitle("Newer remote");
    await waitFor(() => expect(editor).toHaveTextContent("Newer remote"));
    holdSave = false;
    await act(async () => releaseSave!());
    await waitFor(() => expect(qc.isMutating()).toBe(0));
    expect(editor).toHaveTextContent("Newer remote");
    act(() => editor.focus());
    await typeTitle(editor, "After newer remote");
    await blurTitle(editor, 2);
    expect(writes[1]).toEqual({ data: { title: "After newer remote", title_base: "Newer remote" }, status: 200 });
  });
});
