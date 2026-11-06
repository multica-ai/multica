// @vitest-environment node
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, ApiClient, setApiInstance } from "../api";
import { setCurrentWorkspace } from "../platform/workspace-storage";
import type { ChatSession } from "../types/chat";
import {
  chatKeys,
  chatPinnedAgentsOptions,
  chatSessionsOptions,
  hasPendingChatTasksOptions,
  pendingChatTasksOptions,
} from "./queries";

const workspaceA = "11111111-1111-4111-8111-111111111111";
const workspaceB = "22222222-2222-4222-8222-222222222222";
const workspacesBySlug: Record<string, string> = {
  "workspace-a": workspaceA,
  "workspace-b": workspaceB,
};

const session = (workspaceId: string): ChatSession => ({
  id: `session-${workspaceId}`,
  workspace_id: workspaceId,
  agent_id: `agent-${workspaceId}`,
  creator_id: "member-1",
  title: `Chat in ${workspaceId}`,
  status: "active",
  has_unread: false,
  created_at: "2026-11-05T00:00:00Z",
  updated_at: "2026-11-05T00:00:00Z",
});

function responseBody(path: string, workspaceId: string): unknown {
  switch (path) {
    case "/api/chat/sessions":
      return [session(workspaceId)];
    case "/api/chat/pinned-agents":
      return [{ agent_id: `agent-${workspaceId}`, position: 1 }];
    case "/api/chat/pending-tasks":
      return { tasks: [{
        task_id: `task-${workspaceId}`,
        chat_session_id: `session-${workspaceId}`,
        status: "running",
      }] };
    case "/api/chat/pending-tasks/has-any":
      return { has_pending: workspaceId === workspaceA };
    default:
      throw new Error(`Unexpected request: ${path}`);
  }
}

function stubWorkspaceServer(failFirst = false) {
  const requestedWorkspaces: string[] = [];
  vi.stubGlobal("fetch", vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
    const headers = new Headers(init?.headers);
    const slug = headers.get("X-Workspace-Slug");
    // Match server resolution: a nonempty slug takes precedence over the UUID.
    const workspaceId = slug
      ? workspacesBySlug[slug]
      : headers.get("X-Workspace-ID");
    if (!workspaceId) throw new Error("Request has no valid workspace");
    requestedWorkspaces.push(workspaceId);
    if (failFirst && requestedWorkspaces.length === 1) {
      setCurrentWorkspace("workspace-b", workspaceB);
      return new Response(JSON.stringify({ error: "temporarily unavailable" }), { status: 503 });
    }
    const url = new URL(input instanceof Request ? input.url : String(input));
    return new Response(JSON.stringify(responseBody(url.pathname, workspaceId)), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }));
  return requestedWorkspaces;
}

let queryClient: QueryClient;

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  setApiInstance(new ApiClient("https://api.example.test"));
  setCurrentWorkspace("workspace-a", workspaceA);
});

afterEach(() => {
  queryClient.clear();
  setCurrentWorkspace(null, null);
  vi.unstubAllGlobals();
});

describe("chat collection workspace ownership", () => {
  it("keeps a sessions retry in its original workspace after navigation", async () => {
    const requestedWorkspaces = stubWorkspaceServer(true);

    await queryClient.fetchQuery({
      ...chatSessionsOptions(workspaceA),
      retry: 1,
      retryDelay: 0,
    });

    expect(requestedWorkspaces).toEqual([workspaceA, workspaceA]);
    expect(queryClient.getQueryData(chatKeys.sessions(workspaceA))).toEqual([session(workspaceA)]);
    expect(queryClient.getQueryData(chatKeys.sessions(workspaceB))).toBeUndefined();
  });

  it("refetches a retained workspace observer without using the newly active workspace", async () => {
    const requestedWorkspaces = stubWorkspaceServer();
    const options = chatSessionsOptions(workspaceA);
    const observer = new QueryObserver(queryClient, options);
    const unsubscribe = observer.subscribe(() => undefined);
    try {
      await queryClient.fetchQuery(options);
      setCurrentWorkspace("workspace-b", workspaceB);

      await queryClient.invalidateQueries({ queryKey: chatKeys.sessions(workspaceA) });

      expect(requestedWorkspaces).toEqual([workspaceA, workspaceA]);
      expect(observer.getCurrentResult().data).toEqual([session(workspaceA)]);
      expect(queryClient.getQueryData(chatKeys.sessions(workspaceA))).toEqual([session(workspaceA)]);
    } finally {
      unsubscribe();
    }
  });

  const collections: Array<{
    name: string;
    path: string;
    fetch: () => Promise<unknown>;
  }> = [
    { name: "sessions", path: "/api/chat/sessions", fetch: () => queryClient.fetchQuery(chatSessionsOptions(workspaceA)) },
    { name: "pinned agents", path: "/api/chat/pinned-agents", fetch: () => queryClient.fetchQuery(chatPinnedAgentsOptions(workspaceA)) },
    { name: "pending tasks", path: "/api/chat/pending-tasks", fetch: () => queryClient.fetchQuery(pendingChatTasksOptions(workspaceA)) },
    { name: "pending indicator", path: "/api/chat/pending-tasks/has-any", fetch: () => queryClient.fetchQuery(hasPendingChatTasksOptions(workspaceA)) },
  ];

  it.each(collections)("fetches $name for the key workspace when options are created after navigation", async ({ path, fetch }) => {
    const requestedWorkspaces = stubWorkspaceServer();
    setCurrentWorkspace("workspace-b", workspaceB);

    expect(await fetch()).toEqual(responseBody(path, workspaceA));
    expect(requestedWorkspaces).toEqual([workspaceA]);
  });

  it.each([
    { name: "an explicit slug", scope: "workspace-a", expected: workspaceA },
    { name: "the current workspace by default", scope: undefined, expected: workspaceB },
  ])("preserves listChatSessions routing with $name", async ({ scope, expected }) => {
    const requestedWorkspaces = stubWorkspaceServer();
    setCurrentWorkspace("workspace-b", workspaceB);

    expect(await api.listChatSessions({ status: "all" }, scope)).toEqual([session(expected)]);
    expect(requestedWorkspaces).toEqual([expected]);
  });
});
