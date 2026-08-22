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
  clearCachedUser: vi.fn(),
  clearToken: vi.fn(),
  getCachedUser: vi.fn(),
  getToken: vi.fn(),
  setCachedUser: vi.fn(),
  setToken: vi.fn(),
};

vi.mock("./api", () => ({ api, ApiError }));
vi.mock("./secure-storage", () => secureStorage);
vi.mock("./workspace-store", () => ({
  useWorkspaceStore: { getState: () => ({ restoreSlug: vi.fn() }) },
}));

const { useAuthStore } = await import("./auth-store");

const USER = { id: "user-1", email: "offline@example.com" } as User;

describe("useAuthStore.initialize", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    secureStorage.getToken.mockResolvedValue(null);
    secureStorage.getCachedUser.mockResolvedValue(null);
    api.getMe.mockResolvedValue(USER);
    useAuthStore.setState({
      user: null,
      isLoading: true,
      hasToken: false,
      isOffline: false,
    });
  });

  it("keeps the login gate closed when no token exists", async () => {
    await useAuthStore.getState().initialize();

    expect(api.getMe).not.toHaveBeenCalled();
    expect(useAuthStore.getState()).toMatchObject({
      user: null,
      hasToken: false,
      isLoading: false,
    });
  });

  it("hydrates a saved user after a non-401 network failure", async () => {
    secureStorage.getToken.mockResolvedValue("token");
    secureStorage.getCachedUser.mockResolvedValue(USER);
    api.getMe.mockRejectedValue(new Error("network unavailable"));

    await useAuthStore.getState().initialize();

    expect(secureStorage.clearToken).not.toHaveBeenCalled();
    expect(useAuthStore.getState()).toMatchObject({
      user: USER,
      hasToken: true,
      isOffline: true,
      isLoading: false,
    });
  });

  it("keeps a legacy token admitted when offline but no snapshot exists", async () => {
    secureStorage.getToken.mockResolvedValue("token");
    api.getMe.mockRejectedValue(new Error("network unavailable"));

    await useAuthStore.getState().initialize();

    expect(secureStorage.clearToken).not.toHaveBeenCalled();
    expect(useAuthStore.getState()).toMatchObject({
      user: null,
      hasToken: true,
      isOffline: true,
      isLoading: false,
    });
  });

  it("clears the token and cached user on a real 401", async () => {
    secureStorage.getToken.mockResolvedValue("expired-token");
    secureStorage.getCachedUser.mockResolvedValue(USER);
    api.getMe.mockRejectedValue(new ApiError("expired", 401));

    await useAuthStore.getState().initialize();

    expect(secureStorage.clearToken).toHaveBeenCalledOnce();
    expect(secureStorage.clearCachedUser).toHaveBeenCalledOnce();
    expect(useAuthStore.getState()).toMatchObject({
      user: null,
      hasToken: false,
      isOffline: false,
      isLoading: false,
    });
  });
});
