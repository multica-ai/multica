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

const restoreQueryCacheForUser = vi.fn();
const clearQueryCacheForUser = vi.fn();
const clearDraftsForUser = vi.fn();
const clearChatOutboxForUser = vi.fn();
const clearChatOutbox = vi.fn();

vi.mock("./api", () => ({ api, ApiError }));
vi.mock("./secure-storage", () => secureStorage);
vi.mock("./workspace-store", () => ({
  useWorkspaceStore: { getState: () => ({ restoreSlug: vi.fn() }) },
}));
vi.mock("./query-persistence", () => ({
  restoreQueryCacheForUser,
  clearQueryCacheForUser,
}));
vi.mock("./stores/draft-persistence", () => ({
  clearDraftsForUser,
  clearChatOutboxForUser,
}));
vi.mock("./stores/chat-outbox-store", () => ({ clearChatOutbox }));

const { useAuthStore } = await import("./auth-store");

const USER = { id: "user-1", email: "offline@example.com" } as User;

describe("useAuthStore.initialize", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    secureStorage.getToken.mockResolvedValue(null);
    secureStorage.getCachedUser.mockResolvedValue(null);
    api.getMe.mockResolvedValue(USER);
    restoreQueryCacheForUser.mockResolvedValue(undefined);
    clearQueryCacheForUser.mockResolvedValue(undefined);
    clearDraftsForUser.mockResolvedValue(undefined);
    clearChatOutboxForUser.mockResolvedValue(undefined);
    clearChatOutbox.mockReset();
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
    expect(restoreQueryCacheForUser).toHaveBeenCalledWith(USER.id);
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

  it("keeps drafts and the outbox when a cold start receives a real 401", async () => {
    secureStorage.getToken.mockResolvedValue("expired-token");
    secureStorage.getCachedUser.mockResolvedValue(USER);
    api.getMe.mockRejectedValue(new ApiError("expired", 401));

    await useAuthStore.getState().initialize();

    expect(secureStorage.clearToken).toHaveBeenCalledOnce();
    expect(secureStorage.clearCachedUser).toHaveBeenCalledOnce();
    expect(clearQueryCacheForUser).toHaveBeenCalledWith(USER.id);
    expect(clearDraftsForUser).not.toHaveBeenCalled();
    expect(clearChatOutboxForUser).not.toHaveBeenCalled();
    expect(clearChatOutbox).not.toHaveBeenCalled();
    expect(useAuthStore.getState()).toMatchObject({
      user: null,
      hasToken: false,
      isOffline: false,
      isLoading: false,
    });
  });

  it("does not clear any local partition when a 401 has no cached user snapshot", async () => {
    secureStorage.getToken.mockResolvedValue("expired-token");
    api.getMe.mockRejectedValue(new ApiError("expired", 401));

    await useAuthStore.getState().initialize();

    expect(clearDraftsForUser).not.toHaveBeenCalled();
    expect(clearChatOutboxForUser).not.toHaveBeenCalled();
  });

  it("clears this user's drafts and outbox only on explicit logout", async () => {
    useAuthStore.setState({ user: USER, hasToken: true });

    await useAuthStore.getState().logout();

    expect(clearDraftsForUser).toHaveBeenCalledWith(USER.id);
    expect(clearChatOutboxForUser).toHaveBeenCalledWith(USER.id);
    expect(clearChatOutbox).toHaveBeenCalledOnce();
  });
});
