import { createServer, type ServerResponse } from "node:http";
import { useEffect, useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { afterAll, afterEach, beforeAll, expect, it } from "vitest";
import { CoreProvider } from "@multica/core/platform";
import { useSearchStore } from "@multica/core/command-bar";
import { useAuthStore } from "@multica/core/auth";
import { DOCS_V1_FLAG } from "@multica/core/feature-flags";
import { workspaceListOptions } from "@multica/core/workspace";
import { RESOURCES } from "@multica/views/locales";
import { useTabStore } from "@/stores/tab-store";
import { getAppRouter } from "@/platform/tab-coordinator";
import { createDesktopLocaleAdapter } from "@/platform/i18n-adapter";
import { DesktopShell } from "./desktop-layout";

const stamp = "2026-11-14T00:00:00Z";
const workspaces = ["a", "b"].map((key) => ({
  id: `workspace-${key}`,
  slug: `synthetic-${key}`,
  name: `Workspace ${key.toUpperCase()}`,
  created_at: stamp,
  updated_at: stamp,
}));
const issue = (key: string) => ({
  id: key === "a" ? "11111111-1111-4111-8111-111111111111" : "22222222-2222-4222-8222-222222222222",
  workspace_id: `workspace-${key}`,
  number: 1,
  identifier: `${key.toUpperCase()}-1`,
  title: `Alpha ${key.toUpperCase()} result`,
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "synthetic-user",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  start_date: null,
  due_date: null,
  metadata: {},
  properties: {},
  created_at: stamp,
  updated_at: stamp,
  match_source: "title",
});
const requests: { path: string; workspace: string }[] = [];
const inboxListeners = new Set<(payload: { slug: string; issueKey: string }) => void>();
const held: { response: ServerResponse; payload: unknown }[] = [];
let holdA = false;
let baseUrl = "";
let queryClient: QueryClient;
const originalDesktopAPI = window.desktopAPI;
const originalScrollIntoView = Element.prototype.scrollIntoView;
// Every row and route here waits on the real fixture server, which shares its
// event loop with the whole shell. In CI, turbo runs other packages' suites
// beside this one, and the first search has queued behind the shell's startup
// requests for over five seconds.
const NETWORK_WAIT = { timeout: 15_000 };
const TEST_TIMEOUT = 60_000;

function payloadFor(path: string, workspace: string): unknown {
  if (path === "/api/config") {
    return { feature_flags: { [DOCS_V1_FLAG]: true }, allow_signup: false };
  }
  if (path === "/api/me") return {
    id: "synthetic-user", name: "Synthetic user", email: "synthetic@example.test",
    onboarded_at: stamp, onboarding_questionnaire: { source: "other" },
    star_prompt_starred_at: stamp,
  };
  if (path === "/api/workspaces") return workspaces;
  if (path === "/api/issues/search") return { issues: [issue(workspace === "synthetic-b" ? "b" : "a")] };
  if (path === "/api/projects/search" || path === "/api/projects") return { projects: [], total: 0 };
  if (path === "/api/docs/search") return { docs: [] };
  if (path === "/api/docs") return { folders: [], docs: [] };
  if (path === "/api/issues") return { issues: [], total: 0 };
  if (path === "/api/issue-statuses") return { statuses: [] };
  if (path === "/api/autopilots") return { autopilots: [], total: 0 };
  if (path === "/api/agent-run-counts") return {};
  if (path === "/api/issue-view-preferences") return { preferences: [] };
  if (path.endsWith("/members")) return [{
    user_id: "synthetic-user", name: "Synthetic user", email: "synthetic@example.test", role: "owner",
  }];
  // The unsupported local-index endpoint exercises the production HTTP fallback.
  if (path.startsWith("/api/search-index/")) return undefined;
  if (/^\/api\/issues\/[^/]+$/.test(path)) return issue(workspace === "synthetic-b" ? "b" : "a");
  if (path.startsWith("/api/")) return [];
  return undefined;
}

const server = createServer((request, response) => {
  const path = new URL(request.url ?? "/", "http://fixture").pathname;
  const workspace = String(request.headers["x-workspace-slug"] ?? "");
  requests.push({ path, workspace });
  const payload = payloadFor(path, workspace);
  response.setHeader("Content-Type", "application/json");
  if (holdA && workspace === "synthetic-a" && path.endsWith("/search")) {
    held.push({ response, payload });
    return;
  }
  response.statusCode = payload === undefined ? 404 : 200;
  response.end(JSON.stringify(payload ?? { message: "Unsupported synthetic endpoint" }));
});

function ReadyShell() {
  const user = useAuthStore((state) => state.user);
  const { data } = useQuery(workspaceListOptions());
  const [ready, setReady] = useState(false);
  queryClient = useQueryClient();
  useEffect(() => {
    if (!user || !data?.length || ready) return;
    useTabStore.getState().switchWorkspace("synthetic-a", "/synthetic-a/projects");
    setReady(true);
  }, [user, data, ready]);
  return ready ? <DesktopShell /> : null;
}

function input() {
  return screen.getByRole("combobox");
}

async function openSearch() {
  fireEvent.keyDown(document, { key: "k", code: "KeyK", metaKey: true });
  await screen.findByRole("combobox");
}

async function notify(workspace: string) {
  await act(async () => {
    for (const listener of inboxListeners) {
      listener({ slug: workspace, issueKey: workspace === "synthetic-b" ? "B-1" : "A-1" });
    }
  });
  await waitFor(() => expect(getAppRouter().state.location.pathname).toMatch(new RegExp(`^/${workspace}/(inbox|issues/)`)), NETWORK_WAIT);
}

async function mountShell() {
  render(
    <CoreProvider apiBaseUrl={baseUrl} wsUrl={baseUrl.replace("http", "ws") + "/ws"}
      locale="en" resources={RESOURCES} identity={{ platform: "desktop", os: "macos" }}
      localeAdapter={createDesktopLocaleAdapter("en")}>
      <ReadyShell />
    </CoreProvider>,
  );
  await waitFor(() => expect(document.querySelector('[data-slot="app-rail"]')).toBeInTheDocument(), NETWORK_WAIT);
}

beforeAll(async () => {
  // cmdk measures selected rows through this browser layout API, absent in jsdom.
  Element.prototype.scrollIntoView = () => {};
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Missing fixture HTTP port");
  baseUrl = `http://127.0.0.1:${address.port}`;
  localStorage.setItem("multica_token", "synthetic-loopback-token");
  const native = {
    platform: "darwin",
    windowContext: { kind: "main" },
    runtimeConfig: { ok: true, config: { apiUrl: baseUrl, appUrl: baseUrl } },
    getOtherWindowWorkspaces: () => [],
    focusWorkspaceWindow: () => false,
    onInboxOpen: (listener: (payload: { slug: string; issueKey: string }) => void) => {
      inboxListeners.add(listener);
      return () => inboxListeners.delete(listener);
    },
  };
  window.desktopAPI = new Proxy(native, {
    get(target, key) {
      if (key in target) return Reflect.get(target, key);
      if (String(key).startsWith("on")) return () => () => {};
      return () => Promise.resolve(null);
    },
  }) as unknown as Window["desktopAPI"];
});

afterEach(() => {
  cleanup();
  useSearchStore.getState().setOpen(false);
  queryClient?.clear();
  holdA = false;
  for (const { response, payload } of held.splice(0)) response.end(JSON.stringify(payload));
});

afterAll(async () => {
  window.desktopAPI = originalDesktopAPI;
  Element.prototype.scrollIntoView = originalScrollIntoView;
  server.closeAllConnections();
  await new Promise<void>((resolve) => server.close(() => resolve()));
});

it("resets completed search rows through a native notification workspace switch", async () => {
  await mountShell();
  await openSearch();
  fireEvent.change(input(), { target: { value: "alpha" } });
  await screen.findByRole("option", { name: /Alpha A result/ }, NETWORK_WAIT);
  await notify("synthetic-b");
  expect(input()).toHaveValue("");
  expect(screen.queryByRole("option", { name: /Alpha A result/ })).not.toBeInTheDocument();
  fireEvent.change(input(), { target: { value: "alpha" } });
  await screen.findByRole("option", { name: /Alpha B result/ }, NETWORK_WAIT);
  expect(requests.some((request) => request.path === "/api/issues/search" && request.workspace === "synthetic-b")).toBe(true);
  fireEvent.keyDown(input(), { key: "ArrowDown" });
  fireEvent.keyDown(input(), { key: "ArrowUp" });
  expect(screen.getByRole("option", { name: /Alpha B result/ })).toHaveAttribute("aria-selected", "true");
  fireEvent.keyDown(input(), { key: "Enter" });
  await waitFor(() => expect(getAppRouter().state.location.pathname).toBe("/synthetic-b/issues/B-1"), NETWORK_WAIT);
}, TEST_TIMEOUT);

it("cancels an in-flight search when the production router switches workspace", async () => {
  await mountShell();
  await openSearch();
  holdA = true;
  fireEvent.change(input(), { target: { value: "alpha" } });
  // Issues, projects and docs: one held search request each. Docs search runs
  // on its own debounce, so waiting for only the first two raced its arrival.
  await waitFor(() => expect(held.length).toBe(3), NETWORK_WAIT);
  await notify("synthetic-b");
  await act(async () => {
    for (const { response, payload } of held.splice(0)) response.end(JSON.stringify(payload));
  });
  expect(input()).toHaveValue("");
  expect(screen.queryByRole("option", { name: /Alpha A result/ })).not.toBeInTheDocument();
  fireEvent.change(input(), { target: { value: "alpha" } });
  await screen.findByRole("option", { name: /Alpha B result/ }, NETWORK_WAIT);
}, TEST_TIMEOUT);

it("resets a returning workspace while preserving same-workspace search navigation", async () => {
  await mountShell();
  await openSearch();
  fireEvent.change(input(), { target: { value: "alpha" } });
  await screen.findByRole("option", { name: /Alpha A result/ }, NETWORK_WAIT);
  // Notifications within A preserve the query and its results.
  await notify("synthetic-a");
  expect(input()).toHaveValue("alpha");
  expect(screen.getByRole("option", { name: /Alpha A result/ })).toBeInTheDocument();
  await notify("synthetic-b");
  expect(input()).toHaveValue("");
  fireEvent.change(input(), { target: { value: "alpha" } });
  await screen.findByRole("option", { name: /Alpha B result/ }, NETWORK_WAIT);
  await notify("synthetic-a");
  expect(input()).toHaveValue("");
  expect(screen.queryByRole("option", { name: /Alpha B result/ })).not.toBeInTheDocument();
  fireEvent.keyDown(input(), { key: "Escape" });
  await openSearch();
  expect(input()).toHaveValue("");
}, TEST_TIMEOUT);
