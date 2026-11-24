/**
 * @vitest-environment jsdom
 */
import { act, render, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import { ApiClient, ApiError } from "../api/client";
import { configStore } from "../config";
import { daemonCommands } from "../runtimes/commands";
import {
  createAuthStore,
  registerAuthStore,
  useAuthStore,
} from "../auth";
import type { StorageAdapter, User, Workspace } from "../types";
import { workspaceKeys } from "../workspace/queries";
import {
  registerDraftCleanup,
  __clearDraftCleanupRegistryForTest,
} from "../drafts/cleanup-registry";
import { AuthInitializer } from "./auth-initializer";

const logger = vi.hoisted(() => ({
  debug: vi.fn(),
  info: vi.fn(),
  warn: vi.fn(),
  error: vi.fn(),
}));

vi.mock("../logger", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../logger")>();
  return { ...actual, createLogger: () => logger };
});

vi.mock("../analytics", () => ({
  captureSignupSource: vi.fn(),
  identify: vi.fn(),
  initAnalytics: vi.fn(),
  resetAnalytics: vi.fn(),
}));

const fakeUser = {
  id: "user-1",
  name: "Alice",
  email: "alice@example.com",
  avatar_url: null,
} as User;

const fakeWorkspaces = [{ id: "ws-1", slug: "acme" }] as Workspace[];

function makeStorage(initial: Record<string, string> = {}): StorageAdapter & {
  snapshot: () => Record<string, string>;
} {
  const values = { ...initial };
  return {
    getItem: (key) => values[key] ?? null,
    setItem: (key, value) => {
      values[key] = value;
    },
    removeItem: (key) => {
      delete values[key];
    },
    keys: () => Object.keys(values),
    snapshot: () => ({ ...values }),
  };
}

function makeApi(overrides: Partial<ApiClient> = {}): ApiClient {
  return {
    getConfig: vi.fn().mockResolvedValue({}),
    getMe: vi.fn().mockResolvedValue(fakeUser),
    listWorkspaces: vi.fn().mockResolvedValue(fakeWorkspaces),
    setToken: vi.fn(),
    ...overrides,
  } as unknown as ApiClient;
}

function makeApiWithConfigResponse(config: unknown): ApiClient {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(config))));
  const configApi = new ApiClient("https://api.example.test");
  return makeApi({ getConfig: () => configApi.getConfig() });
}

function renderInitializer({
  api,
  storage = makeStorage({ multica_token: "token-1" }),
  cookieAuth = false,
  platform = "desktop",
}: {
  api: ApiClient;
  storage?: StorageAdapter;
  cookieAuth?: boolean;
  platform?: "desktop" | "web";
}) {
  const onLogin = vi.fn();
  const onLogout = vi.fn();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  setApiInstance(api);
  registerAuthStore(
    createAuthStore({ api, storage, cookieAuth, onLogin, onLogout }),
  );

  const result = render(
    <QueryClientProvider client={queryClient}>
      <AuthInitializer
        cookieAuth={cookieAuth}
        identity={{ platform }}
        onLogin={onLogin}
        storage={storage}
      >
        <div>child</div>
      </AuthInitializer>
    </QueryClientProvider>,
  );

  return { ...result, onLogin, onLogout, queryClient };
}

beforeEach(() => {
  vi.clearAllMocks();
  configStore.setState({
    daemonConfigStatus: "loading",
    daemonSetupTarget: "unknown",
    daemonServerUrl: "",
    daemonAppUrl: "",
  });
  __clearDraftCleanupRegistryForTest();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("AuthInitializer recovery", () => {
  it("keeps self-host commands available when an older API omits the target", async () => {
    renderInitializer({ api: makeApiWithConfigResponse({
      daemon_server_url: " https://api.example.com/// ",
      daemon_app_url: "https://app.example.com/",
    }) });

    await waitFor(() => {
      expect(configStore.getState().daemonConfigStatus).toBe("ready");
    });
    const config = configStore.getState();
    expect(config.daemonSetupTarget).toBe("self_host");
    const commands = daemonCommands({
      status: config.daemonConfigStatus,
      target: config.daemonSetupTarget,
      serverUrl: config.daemonServerUrl,
      appUrl: config.daemonAppUrl,
    });
    expect(commands).toMatchObject({
      status: "ready",
      setupCmd: "multica setup self-host --server-url https://api.example.com --app-url https://app.example.com",
    });
    if (commands.status !== "ready") throw new Error("expected self-host commands");
    expect(commands.tokenCmd).toContain("server_url https://api.example.com");
    expect(commands.tokenCmd).toContain("app_url https://app.example.com");
    expect(commands.tokenCmd).not.toContain("multica.ai");
  });

  it.each([
    { name: "both URLs absent", response: {} },
    { name: "app URL absent", response: { daemon_server_url: "https://api.example.com" } },
    { name: "server URL absent", response: { daemon_app_url: "https://app.example.com" } },
    {
      name: "blank server URL",
      response: { daemon_server_url: " / ", daemon_app_url: "https://app.example.com" },
    },
    {
      name: "blank app URL",
      response: { daemon_server_url: "https://api.example.com", daemon_app_url: "  " },
    },
    {
      name: "malformed server URL",
      response: { daemon_server_url: {}, daemon_app_url: "https://app.example.com" },
    },
    {
      name: "malformed app URL",
      response: { daemon_server_url: "https://api.example.com", daemon_app_url: 123 },
    },
    ...["unknown", "future_target", null].map((target) => ({
      name: `explicit target ${JSON.stringify(target)}`,
      response: {
        daemon_setup_target: target,
        daemon_server_url: "https://api.example.com",
        daemon_app_url: "https://app.example.com",
      },
    })),
    {
      name: "unreadable config",
      response: {
        cdn_domain: 123,
        daemon_server_url: "https://api.example.com",
        daemon_app_url: "https://app.example.com",
      },
    },
  ])("keeps commands unavailable for $name", async ({ response }) => {
    renderInitializer({ api: makeApiWithConfigResponse(response) });
    await waitFor(() => {
      expect(configStore.getState().daemonConfigStatus).toBe("ready");
    });
    const config = configStore.getState();
    expect(config.daemonSetupTarget).toBe("unknown");
    expect(daemonCommands({
      status: config.daemonConfigStatus,
      target: config.daemonSetupTarget,
      serverUrl: config.daemonServerUrl,
      appUrl: config.daemonAppUrl,
    })).toEqual({ status: "unknown" });
  });

  it("honors an explicit Cloud target even when both URLs are present", async () => {
    renderInitializer({ api: makeApiWithConfigResponse({
      daemon_setup_target: "cloud",
      daemon_server_url: "https://api.example.com",
      daemon_app_url: "https://app.example.com",
    }) });
    await waitFor(() => {
      expect(configStore.getState().daemonConfigStatus).toBe("ready");
    });
    expect(configStore.getState().daemonSetupTarget).toBe("cloud");
  });

  it("keeps setup unavailable until a delayed config response confirms the target", async () => {
    let resolveConfig!: (config: Awaited<ReturnType<ApiClient["getConfig"]>>) => void;
    const getConfig = vi.fn(() => new Promise<Awaited<ReturnType<ApiClient["getConfig"]>>>(
      (resolve) => { resolveConfig = resolve; },
    ));
    renderInitializer({ api: makeApi({ getConfig }) });
    expect(configStore.getState().daemonConfigStatus).toBe("loading");
    expect(configStore.getState().daemonSetupTarget).toBe("unknown");
    await act(async () => resolveConfig({
      cdn_domain: "",
      allow_signup: true,
      daemon_setup_target: "self_host",
      daemon_server_url: "https://api.example.com",
      daemon_app_url: "https://app.example.com",
    }));
    expect(configStore.getState()).toMatchObject({
      daemonConfigStatus: "ready",
      daemonSetupTarget: "self_host",
      daemonServerUrl: "https://api.example.com",
      daemonAppUrl: "https://app.example.com",
    });
  });

  it("exposes config failure and recovers the setup target on the existing retry", async () => {
    vi.useFakeTimers();
    const getConfig = vi.fn()
      .mockRejectedValueOnce(new TypeError("network unavailable"))
      .mockResolvedValue({ daemon_setup_target: "cloud" });
    renderInitializer({ api: makeApi({ getConfig }) });
    await act(async () => {
      await Promise.resolve();
    });
    expect(configStore.getState().daemonConfigStatus).toBe("error");
    expect(configStore.getState().daemonSetupTarget).toBe("unknown");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(getConfig).toHaveBeenCalledTimes(2);
    expect(configStore.getState()).toMatchObject({
      daemonConfigStatus: "ready",
      daemonSetupTarget: "cloud",
    });
  });

  it("publishes the API origin used by persisted attachment links", async () => {
    const api = makeApi({
      getConfig: vi.fn().mockResolvedValue({
        attachment_public_url: "https://api.example.test",
      }),
    });
    renderInitializer({ api });

    await waitFor(() => {
      expect(configStore.getState().attachmentPublicUrl).toBe("https://api.example.test");
    });
  });

  it("keeps the token and recovers on the online event after a network failure", async () => {
    const storage = makeStorage({ multica_token: "token-1" });
    const getMe = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("fetch failed"))
      .mockResolvedValue(fakeUser);
    const api = makeApi({ getMe });
    const { onLogout } = renderInitializer({ api, storage });

    await waitFor(() => {
      expect(useAuthStore.getState().status).toBe("recovering");
    });
    expect(storage.snapshot().multica_token).toBe("token-1");
    expect(onLogout).not.toHaveBeenCalled();

    act(() => window.dispatchEvent(new Event("online")));

    await waitFor(() => {
      expect(useAuthStore.getState().user).toEqual(fakeUser);
    });
    expect(getMe).toHaveBeenCalledTimes(2);
    expect(onLogout).not.toHaveBeenCalled();
  });

  it("lets a manual retry restart a recoverable auth attempt", async () => {
    const getMe = vi
      .fn()
      .mockRejectedValueOnce(new ApiError("unavailable", 503, "Unavailable"))
      .mockResolvedValue(fakeUser);
    const api = makeApi({ getMe });
    const { onLogout } = renderInitializer({ api });

    await waitFor(() => {
      expect(useAuthStore.getState().status).toBe("recovering");
    });
    act(() => useAuthStore.getState().retryAuthentication());

    await waitFor(() => {
      expect(useAuthStore.getState().status).toBe("authenticated");
    });
    expect(getMe).toHaveBeenCalledTimes(2);
    expect(onLogout).not.toHaveBeenCalled();
  });

  it("continues automatic retries at the capped backoff interval", async () => {
    vi.useFakeTimers();
    const getMe = vi.fn().mockRejectedValue(new TypeError("still offline"));
    const api = makeApi({ getMe });
    const { onLogout } = renderInitializer({ api });

    await act(async () => {
      await Promise.resolve();
    });
    expect(useAuthStore.getState().status).toBe("recovering");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(31_000);
    });
    expect(getMe).toHaveBeenCalledTimes(6);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });
    expect(getMe).toHaveBeenCalledTimes(7);
    expect(logger.error).toHaveBeenCalledTimes(1);
    expect(onLogout).not.toHaveBeenCalled();
  });

  it("does not invalidate a verified desktop session when workspace loading fails", async () => {
    const listWorkspaces = vi
      .fn()
      .mockRejectedValue(new TypeError("network unavailable"));
    const api = makeApi({ listWorkspaces });
    const { onLogout, queryClient } = renderInitializer({ api });

    await waitFor(() => {
      expect(useAuthStore.getState().status).toBe("authenticated");
      expect(
        queryClient.getQueryState(workspaceKeys.list())?.status,
      ).toBe("error");
    });
    expect(useAuthStore.getState().user).toEqual(fakeUser);
    expect(onLogout).not.toHaveBeenCalled();
  });

  it("publishes web auth independently when workspace loading fails", async () => {
    const listWorkspaces = vi
      .fn()
      .mockRejectedValue(new TypeError("backend restarting"));
    const api = makeApi({ listWorkspaces });
    const { onLogout, queryClient } = renderInitializer({
      api,
      cookieAuth: true,
      platform: "web",
    });

    await waitFor(() => {
      expect(useAuthStore.getState().status).toBe("authenticated");
      expect(queryClient.getQueryState(workspaceKeys.list())?.status).toBe(
        "error",
      );
    });
    expect(useAuthStore.getState().user).toEqual(fakeUser);
    expect(onLogout).not.toHaveBeenCalled();
  });

  it("reloads app config after auth recovers from a transient failure", async () => {
    const getConfig = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("network unavailable"))
      .mockResolvedValue({ feature_flags: { recovered: true } });
    const getMe = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("network unavailable"))
      .mockResolvedValue(fakeUser);
    const api = makeApi({ getConfig, getMe });
    renderInitializer({ api });

    await waitFor(() => {
      expect(useAuthStore.getState().status).toBe("recovering");
    });
    act(() => useAuthStore.getState().retryAuthentication());

    await waitFor(() => {
      expect(useAuthStore.getState().status).toBe("authenticated");
      expect(getConfig).toHaveBeenCalledTimes(2);
    });
  });

  it("keeps retrying app config until it loads", async () => {
    vi.useFakeTimers();
    const getConfig = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("network unavailable"))
      .mockRejectedValueOnce(new TypeError("still unavailable"))
      .mockResolvedValue({ feature_flags: { recovered: true } });
    const api = makeApi({ getConfig });
    renderInitializer({ api });

    await act(async () => {
      await Promise.resolve();
    });
    expect(getConfig).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000);
    });
    expect(getConfig).toHaveBeenCalledTimes(3);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(getConfig).toHaveBeenCalledTimes(3);
  });

  // A → 401 → B, where B shares a workspace with A so the post-login stale-tab
  // validator prunes nothing. B must find none of A's client state. What
  // exactly gets cleared is pinned in platform/session-cleanup.test.ts; this
  // asserts the 401 is wired to it at all.
  it("erases the previous user's client state when a live session is rejected", async () => {
    const shared = "acme";
    const storage = makeStorage({
      multica_token: "token-1",
      "multica_comment_drafts:acme": '{"issue-1":"A private draft"}',
      multica_tabs: '[{"path":"/acme/issues/secret"}]',
    });
    const resetInMemory = vi.fn();
    registerDraftCleanup({
      storageKey: "multica_comment_drafts",
      workspaceScoped: true,
      resetInMemory,
    });
    const api = makeApi({
      listWorkspaces: vi
        .fn()
        .mockResolvedValue([{ id: "ws-1", slug: shared }] as Workspace[]),
    });
    const { queryClient } = renderInitializer({ api, storage });

    await waitFor(() => {
      expect(useAuthStore.getState().status).toBe("authenticated");
    });
    await waitFor(() => {
      expect(queryClient.getQueryData(workspaceKeys.list())).toHaveLength(1);
    });
    queryClient.setQueryData(["issues", "ws-1"], [{ id: "i-1" }]);

    act(() => {
      useAuthStore.getState().sessionExpired();
    });

    await waitFor(() => {
      expect(queryClient.getQueryData(["issues", "ws-1"])).toBeUndefined();
    });
    expect(storage.snapshot()).toEqual({});
    expect(resetInMemory).toHaveBeenCalled();
    expect(useAuthStore.getState().expired).toBe(true);
  });

  // Elon's cold-start repro: A closes the app, the token expires while it is
  // shut, and the next launch is rejected at the identity probe. This never
  // passes through `authenticated`, and there is no workspace list to
  // enumerate slugs from — so both the trigger and the sweep have to work
  // without either.
  // The failure mode that must never happen: a laptop with no connectivity
  // gets signed out and asked for credentials it cannot verify. Only a 401
  // ends a session — everything else waits.
  it.each([
    ["offline", new TypeError("fetch failed")],
    ["server error", new ApiError("boom", 500, "Internal Server Error")],
    ["gateway timeout", new ApiError("slow", 504, "Gateway Timeout")],
  ])("keeps the session and every draft when getMe fails: %s", async (_label, error) => {
    const before = {
      multica_token: "token-1",
      "multica_comment_drafts:acme": '{"issue-1":"unsent work"}',
      multica_tabs: '[{"path":"/acme/issues/1"}]',
    };
    const storage = makeStorage(before);
    const resetInMemory = vi.fn();
    registerDraftCleanup({
      storageKey: "multica_comment_drafts",
      workspaceScoped: true,
      resetInMemory,
    });
    const api = makeApi({ getMe: vi.fn().mockRejectedValue(error) });
    renderInitializer({ api, storage });

    await waitFor(() => {
      expect(useAuthStore.getState().status).toBe("recovering");
    });

    expect(useAuthStore.getState().status).not.toBe("unauthenticated");
    expect(storage.snapshot()).toEqual(before);
    expect(resetInMemory).not.toHaveBeenCalled();
  });

  it("erases the previous session on a cold start with a stale token", async () => {
    const storage = makeStorage({
      multica_token: "stale-token",
      "multica_comment_drafts:acme": '{"issue-1":"A private draft"}',
      "multica:chat:activeSessionId:acme": "session-1",
      multica_tabs: '[{"path":"/acme/issues/secret"}]',
      multica_locale: "zh-Hans",
    });
    registerDraftCleanup({
      storageKey: "multica_comment_drafts",
      workspaceScoped: true,
      resetInMemory: vi.fn(),
    });
    const api = makeApi({
      getMe: vi
        .fn()
        .mockRejectedValue(new ApiError("unauthorized", 401, "Unauthorized")),
    });
    const { queryClient } = renderInitializer({ api, storage });

    await waitFor(() => {
      expect(useAuthStore.getState().status).toBe("unauthenticated");
    });
    await waitFor(() => {
      // Only the device preference survives.
      expect(storage.snapshot()).toEqual({ multica_locale: "zh-Hans" });
    });
    expect(queryClient.getQueryData(workspaceKeys.list())).toBeUndefined();
  });

  it("publishes a definitive logout for a genuine 401", async () => {
    const storage = makeStorage({ multica_token: "token-1" });
    const getMe = vi.fn().mockImplementation(() => {
      storage.removeItem("multica_token");
      return Promise.reject(new ApiError("unauthorized", 401, "Unauthorized"));
    });
    const api = makeApi({ getMe });
    const { onLogout } = renderInitializer({ api, storage });

    await waitFor(() => {
      expect(useAuthStore.getState().status).toBe("unauthenticated");
    });
    expect(storage.snapshot().multica_token).toBeUndefined();
    expect(onLogout).toHaveBeenCalledOnce();
  });
});
