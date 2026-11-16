// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { QueryClient } from "@tanstack/react-query";
import type { StateCreator } from "zustand";

const hooks = vi.hoisted(() => ({ qc: undefined as unknown as QueryClient }));
vi.mock("@tanstack/react-query", async (original) => {
  const actual = await original<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQueryClient: () => hooks.qc,
    useMutation: (options: ConstructorParameters<typeof actual.MutationObserver>[1]) => {
      const observer = new actual.MutationObserver(hooks.qc, options);
      return {
        mutateAsync: (variables: unknown, callbacks: Parameters<typeof observer.mutate>[1]) =>
          observer.mutate(variables, callbacks),
      };
    },
  };
});
vi.mock("zustand", async (original) => {
  const actual = await original<typeof import("zustand")>();
  const bind = (initializer: StateCreator<unknown>) => {
    const store = actual.create(initializer);
    return Object.assign((select: (state: unknown) => unknown) => select(store.getState()), store);
  };
  return { ...actual, create: (initializer?: StateCreator<unknown>) => initializer ? bind(initializer) : bind };
});

const disk = vi.hoisted(() => ({
  values: new Map<string, string>(),
  readConfig: null as Promise<string | null> | null,
  deleteToken: null as Promise<void> | null,
  failWrite: false,
  clearCookies: null as Promise<void> | null,
  cookieClears: 0,
}));
vi.mock("expo-secure-store", () => ({
  getItemAsync: vi.fn(async (key: string) =>
    key === "multica_server_config" && disk.readConfig ? disk.readConfig : disk.values.get(key) ?? null),
  setItemAsync: vi.fn(async (key: string, value: string) => {
    if (disk.failWrite) throw new Error("Storage unavailable");
    disk.values.set(key, value);
  }),
  deleteItemAsync: vi.fn(async (key: string) => {
    if (key === "multica_token") await disk.deleteToken;
    disk.values.delete(key);
  }),
}));
vi.mock("react-native", () => ({
  Platform: { OS: "android" },
  AppState: { addEventListener: () => ({ remove: vi.fn() }) },
  Networking: {
    clearCookies: (done: (cleared: boolean) => void) => {
      disk.cookieClears++;
      void (disk.clearCookies ?? Promise.resolve()).then(() => done(true));
    },
  },
}));
vi.mock("expo-constants", () => ({ default: { expoConfig: { version: "0.1.2" } } }));
vi.mock("@react-native-community/netinfo", () => ({
  default: { addEventListener: () => vi.fn() },
}));

const first = { apiUrl: "https://a.example.test:8443/proxy", webUrl: "https://web.a.test/ui" };
const second = { apiUrl: "https://b.example.test:9443", webUrl: "https://b.example.test:9443" };
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

beforeEach(() => {
  vi.resetModules();
  disk.values = new Map([["multica_server_config", JSON.stringify(first)], ["multica_token", "a-token"]]);
  disk.readConfig = null;
  disk.deleteToken = null;
  disk.failWrite = false;
  disk.clearCookies = null;
  disk.cookieClears = 0;
  vi.spyOn(console, "log").mockImplementation(() => {});
});
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe("persistent instance/session isolation", () => {
  it.each(["chat", "inbox", "project", "comment"])("does not revive %s cache or failed-comment state after an actual server switch", async (kind) => {
    const { initializeServerConfig } = await import("./server-config-store");
    await initializeServerConfig();
    const { api } = await import("./api");
    const { useAuthStore } = await import("./auth-store");
    const { useWorkspaceStore } = await import("./workspace-store");
    const { queryClient } = await import("./query-client");
    const { changeServerConfig } = await import("./server-session");
    const { useFailedCommentsStore } = await import("./stores/failed-comments-store");
    hooks.qc = queryClient;
    api.setToken("a-token");
    useAuthStore.setState({ user: { id: "user-a" } as never });
    await useWorkspaceStore.getState().setCurrentWorkspace("ws", "demo");
    const late = deferred<Response>();
    const fetch = vi.fn(() => late.promise);
    vi.stubGlobal("fetch", fetch);
    let pending: Promise<unknown>;
    if (kind === "chat") {
      const { chatKeys } = await import("./queries/chat");
      queryClient.setQueryData(chatKeys.sessions("ws"), [{ id: "old", title: "private" }]);
      pending = (await import("./mutations/chat")).useDeleteChatSession().mutateAsync("old");
    } else if (kind === "inbox") {
      const { inboxKeys } = await import("./queries/inbox");
      queryClient.setQueryData(inboxKeys.list("ws"), [{ id: "old", issue_id: "issue" }]);
      pending = (await import("./mutations/inbox")).useArchiveInbox().mutateAsync("old");
    } else if (kind === "project") {
      const { projectKeys } = await import("./queries/projects");
      queryClient.setQueryData(projectKeys.list("ws"), [{ id: "old", title: "private" }]);
      pending = (await import("./mutations/projects")).useDeleteProject("old").mutateAsync();
    } else {
      pending = (await import("./mutations/issues")).useCreateComment("issue").mutateAsync({ content: "private draft" });
    }
    const rejected = expect(pending).rejects.toThrow("Server session changed");
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledOnce());
    await changeServerConfig(second);
    expect(queryClient.getQueryCache().getAll()).toEqual([]);
    late.resolve(new Response(null, { status: 204 }));
    await rejected;
    expect(queryClient.getQueryCache().getAll()).toEqual([]);
    expect(useFailedCommentsStore.getState().failed).toEqual({});
  });

  it("stops an old optimistic continuation before it can dispatch on the new server", async () => {
    const { initializeServerConfig } = await import("./server-config-store");
    await initializeServerConfig();
    const { queryClient } = await import("./query-client");
    hooks.qc = queryClient;
    const cancel = deferred<void>();
    vi.spyOn(queryClient, "cancelQueries").mockReturnValueOnce(cancel.promise);
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    const pending = (await import("./mutations/chat")).useDeleteChatSession().mutateAsync("old");
    const rejected = expect(pending).rejects.toThrow("Server session changed");
    await vi.waitFor(() => expect(queryClient.cancelQueries).toHaveBeenCalledOnce());
    await (await import("./server-session")).changeServerConfig(second);
    cancel.resolve();
    await rejected;
    expect(fetch).not.toHaveBeenCalled();
    expect(queryClient.getQueryCache().getAll()).toEqual([]);
  });

  it("waits for configuration before restoring auth or making any request", async () => {
    const read = deferred<string | null>();
    disk.readConfig = read.promise;
    const cookies = deferred<void>();
    disk.clearCookies = cookies.promise;
    const fetch = vi.fn(async () => Response.json({ id: "user-a" }));
    vi.stubGlobal("fetch", fetch);
    const { useAuthStore } = await import("./auth-store");
    const initialize = useAuthStore.getState().initialize();
    await vi.waitFor(() => expect(fetch).not.toHaveBeenCalled());
    read.resolve(JSON.stringify(first));
    await vi.waitFor(() => expect(disk.cookieClears).toBe(1));
    expect(fetch).not.toHaveBeenCalled();
    cookies.resolve();
    await initialize;
    expect(fetch).toHaveBeenCalledWith(`${first.apiUrl}/api/me`, expect.objectContaining({
      headers: expect.objectContaining({ Authorization: "Bearer a-token" }),
    }));
    expect(useAuthStore.getState().user?.id).toBe("user-a");
  });

  it("fails closed when the saved instance cannot be parsed", async () => {
    disk.values.set("multica_server_config", "{broken");
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    const { useAuthStore } = await import("./auth-store");
    await expect(useAuthStore.getState().initialize()).rejects.toThrow();
    expect(fetch).not.toHaveBeenCalled();
    expect(disk.values.get("multica_token")).toBe("a-token");
  });

  it("aborts reads/uploads, stops WS, clears identity/caches and removes the old token before publishing a new instance", async () => {
    const { initializeServerConfig, useServerConfigStore } = await import("./server-config-store");
    await initializeServerConfig();
    const { api } = await import("./api");
    const { useAuthStore } = await import("./auth-store");
    const { useWorkspaceStore } = await import("./workspace-store");
    const { queryClient } = await import("./query-client");
    const { registerSessionConnection } = await import("./realtime/session-connections");
    const { useChatDraftsStore } = await import("./stores/chat-drafts-store");
    const { changeServerConfig } = await import("./server-session");
    api.setToken("a-token");
    useAuthStore.setState({ user: { id: "user-a" } as never });
    await useWorkspaceStore.getState().setCurrentWorkspace("workspace-a", "a");
    queryClient.setQueryData(["private"], "old data");
    useChatDraftsStore.getState().setDraft("draft-a", "old draft");
    const stopped = vi.fn();
    registerSessionConnection(stopped);
    const late = deferred<Response>();
    const fetch = vi.fn((_url: RequestInfo | URL, _init?: RequestInit) => late.promise);
    vi.stubGlobal("fetch", fetch);
    const unauthorized = vi.fn();
    api.setOptions({ onUnauthorized: unauthorized });
    const oldRead = api.getMe();
    const oldUpload = api.uploadFile({ uri: "file:///a", name: "a", type: "text/plain" });
    // Attach rejection handlers before releasing delayed responses.
    const readResult = expect(oldRead).rejects.toThrow("Server session changed");
    const uploadResult = expect(oldUpload).rejects.toThrow("Server session changed");
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
    const deletion = deferred<void>();
    disk.deleteToken = deletion.promise;
    const cookies = deferred<void>();
    disk.clearCookies = cookies.promise;
    const change = changeServerConfig(second);
    expect(stopped).toHaveBeenCalledOnce();
    expect(api.getToken()).toBeNull();
    expect(useAuthStore.getState().user).toBeNull();
    expect(queryClient.getQueryData(["private"])).toBeUndefined();
    expect(useChatDraftsStore.getState().drafts).toEqual({});
    expect(useWorkspaceStore.getState().currentWorkspaceSlug).toBeNull();
    for (const call of fetch.mock.calls) {
      expect((call[1] as RequestInit).signal?.aborted).toBe(true);
      expect((call[1] as RequestInit).credentials).toBe("omit");
    }
    expect(useServerConfigStore.getState().config).toEqual(first);
    await expect(api.sendCode("synthetic@example.test")).rejects.toThrow("Server session changed");
    expect(fetch).toHaveBeenCalledTimes(2);
    deletion.resolve();
    await vi.waitFor(() => expect(disk.values.has("multica_token")).toBe(false));
    expect(useServerConfigStore.getState().config).toEqual(first);
    cookies.resolve();
    await change;
    expect(disk.values.has("multica_token")).toBe(false);
    expect(JSON.parse(disk.values.get("multica_server_config")!)).toEqual(second);
    late.resolve(Response.json({ message: "old 401" }, { status: 401 }));
    await Promise.all([readResult, uploadResult]);
    expect(unauthorized).not.toHaveBeenCalled();
    fetch.mockImplementation(async () => new Response(null, { status: 204 }));
    await api.sendCode("synthetic@example.test");
    expect(fetch).toHaveBeenLastCalledWith(`${second.apiUrl}/auth/send-code`, expect.objectContaining({
      headers: expect.not.objectContaining({ Authorization: expect.anything(), "X-Workspace-Slug": expect.anything() }),
    }));
    // Cold-start restore sees the persisted pair before using any identity.
    vi.resetModules();
    const restored = await import("./server-config-store");
    await restored.initializeServerConfig();
    expect(restored.getServerConfig()).toEqual(second);
  });

  it("keeps the old endpoint and signs out if saving the new pair fails", async () => {
    const config = await import("./server-config-store");
    await config.initializeServerConfig();
    const { api } = await import("./api");
    api.setToken("a-token");
    const { changeServerConfig } = await import("./server-session");
    disk.failWrite = true;
    await expect(changeServerConfig(second)).rejects.toThrow("Storage unavailable");
    expect(config.getServerConfig()).toEqual(first);
    expect(api.getToken()).toBeNull();
    expect(disk.values.has("multica_token")).toBe(false);
    expect(config.useServerConfigStore.getState().switching).toBe(false);
  });
});
