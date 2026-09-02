import { beforeEach, describe, expect, it, vi } from "vitest";
import type { User } from "@multica/core/types";

const api = {
  setToken: vi.fn(),
  getMe: vi.fn(),
  sendCode: vi.fn(),
  verifyCode: vi.fn(),
};

class ApiError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
  }
}

const secureStorage = {
  clearSession: vi.fn(),
  getSession: vi.fn(),
  saveSession: vi.fn(),
  saveSessionUser: vi.fn(),
};

vi.mock("./api", () => ({ api, ApiError }));
vi.mock("./secure-storage", () => secureStorage);
vi.mock("./workspace-store", () => ({
  useWorkspaceStore: { getState: () => ({ restoreSlug: vi.fn() }) },
}));

const { useAuthStore } = await import("./auth-store");

const USER = { id: "user-1", email: "offline@example.com" } as User;

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

beforeEach(() => {
  vi.clearAllMocks();
  secureStorage.getSession.mockResolvedValue(null);
  secureStorage.clearSession.mockResolvedValue(undefined);
  secureStorage.saveSession.mockResolvedValue(undefined);
  secureStorage.saveSessionUser.mockResolvedValue(undefined);
  api.getMe.mockResolvedValue(USER);
  useAuthStore.setState({
    user: null,
    isLoading: true,
    hasToken: false,
    isOffline: false,
  });
});

describe("useAuthStore.initialize", () => {
  it("keeps the login gate closed when no session exists", async () => {
    await useAuthStore.getState().initialize();

    expect(api.getMe).not.toHaveBeenCalled();
    expect(useAuthStore.getState()).toMatchObject({
      user: null,
      hasToken: false,
      isLoading: false,
    });
  });

  it("hydrates a saved user after a non-401 network failure", async () => {
    secureStorage.getSession.mockResolvedValue({ token: "t", user: USER });
    api.getMe.mockRejectedValue(new Error("network unavailable"));

    await useAuthStore.getState().initialize();

    expect(secureStorage.clearSession).not.toHaveBeenCalled();
    expect(useAuthStore.getState()).toMatchObject({
      user: USER,
      hasToken: true,
      isOffline: true,
      isLoading: false,
    });
  });

  it("keeps a legacy credential admitted when offline but no snapshot exists", async () => {
    secureStorage.getSession.mockResolvedValue({ token: "t", user: null });
    api.getMe.mockRejectedValue(new Error("network unavailable"));

    await useAuthStore.getState().initialize();

    expect(secureStorage.clearSession).not.toHaveBeenCalled();
    expect(useAuthStore.getState()).toMatchObject({
      user: null,
      hasToken: true,
      isOffline: true,
      isLoading: false,
    });
  });

  it("clears the session on a real 401", async () => {
    secureStorage.getSession.mockResolvedValue({ token: "expired", user: USER });
    api.getMe.mockRejectedValue(new ApiError("expired", 401));

    await useAuthStore.getState().initialize();

    expect(secureStorage.clearSession).toHaveBeenCalledOnce();
    expect(useAuthStore.getState()).toMatchObject({
      user: null,
      hasToken: false,
      isOffline: false,
      isLoading: false,
    });
  });

  it("re-persists the snapshot alongside the credential it was verified with", async () => {
    secureStorage.getSession.mockResolvedValue({ token: "t", user: null });

    await useAuthStore.getState().initialize();

    expect(secureStorage.saveSession).toHaveBeenCalledWith("t", USER);
  });

  it("reports isLoading while a run is in flight", async () => {
    secureStorage.getSession.mockResolvedValue({ token: "t", user: USER });
    const getMe = deferred<User>();
    api.getMe.mockReturnValue(getMe.promise);

    const run = useAuthStore.getState().initialize();
    await Promise.resolve();

    // The /offline Retry button and its spinner both key off this. It was
    // previously only ever written as false, so neither ever engaged.
    expect(useAuthStore.getState().isLoading).toBe(true);

    getMe.resolve(USER);
    await run;
    expect(useAuthStore.getState().isLoading).toBe(false);
  });

  it("lands isLoading false even when the run throws unexpectedly", async () => {
    secureStorage.getSession.mockRejectedValue(new Error("keychain locked"));

    await expect(useAuthStore.getState().initialize()).rejects.toThrow(
      "keychain locked",
    );
    expect(useAuthStore.getState().isLoading).toBe(false);
  });

  it("collapses concurrent calls into a single run", async () => {
    secureStorage.getSession.mockResolvedValue({ token: "t", user: USER });
    const getMe = deferred<User>();
    api.getMe.mockReturnValue(getMe.promise);

    // Tapping Retry repeatedly used to start overlapping runs whose final
    // `set` calls raced; the loser's conclusion won.
    const runs = [
      useAuthStore.getState().initialize(),
      useAuthStore.getState().initialize(),
      useAuthStore.getState().initialize(),
    ];
    getMe.resolve(USER);
    await Promise.all(runs);

    expect(api.getMe).toHaveBeenCalledTimes(1);
    expect(secureStorage.getSession).toHaveBeenCalledTimes(1);
  });

  it("does not let a later non-401 run resurrect a session a 401 cleared", async () => {
    secureStorage.getSession.mockResolvedValue({ token: "expired", user: USER });
    api.getMe.mockRejectedValue(new ApiError("expired", 401));

    await useAuthStore.getState().initialize();

    // The 401 emptied storage; a subsequent retry must find nothing rather
    // than write hasToken/user back on top of an invalidated credential.
    secureStorage.getSession.mockResolvedValue(null);
    api.getMe.mockRejectedValue(new Error("network unavailable"));
    await useAuthStore.getState().initialize();

    expect(useAuthStore.getState()).toMatchObject({
      user: null,
      hasToken: false,
      isOffline: false,
    });
  });
});

describe("useAuthStore.verifyCode", () => {
  it("persists the credential and its snapshot in one write", async () => {
    api.verifyCode.mockResolvedValue({ token: "fresh", user: USER });

    await useAuthStore.getState().verifyCode("a@example.com", "123456");

    expect(secureStorage.saveSession).toHaveBeenCalledExactlyOnceWith(
      "fresh",
      USER,
    );
    expect(useAuthStore.getState()).toMatchObject({
      user: USER,
      hasToken: true,
    });
  });
});

describe("useAuthStore.setUser", () => {
  it("updates the snapshot without touching the credential", async () => {
    const next = { ...USER, email: "renamed@example.com" };

    useAuthStore.getState().setUser(next);

    expect(useAuthStore.getState().user).toEqual(next);
    expect(secureStorage.saveSessionUser).toHaveBeenCalledWith(next);
  });

  it("swallows a failed background write instead of rejecting unhandled", async () => {
    secureStorage.saveSessionUser.mockRejectedValue(new Error("keychain"));

    expect(() => useAuthStore.getState().setUser(USER)).not.toThrow();
    await Promise.resolve();
    await Promise.resolve();

    expect(useAuthStore.getState().user).toEqual(USER);
  });
});
