import { describe, expect, it, vi } from "vitest";
import { ApiError, type ApiClient } from "../api/client";
import type { StorageAdapter, User } from "../types";
import { createAuthStore } from "./store";

const fakeUser: User = {
  id: "u1",
  name: "Alice",
  email: "alice@example.com",
  avatar_url: null,
} as User;

function makeStorage(initial: Record<string, string> = {}): StorageAdapter & {
  snapshot: () => Record<string, string>;
} {
  const data = { ...initial };
  return {
    getItem: (k) => data[k] ?? null,
    setItem: (k, v) => {
      data[k] = v;
    },
    removeItem: (k) => {
      delete data[k];
    },
    snapshot: () => ({ ...data }),
  };
}

function makeApi(): ApiClient {
  return {
    setToken: vi.fn(),
  } as unknown as ApiClient;
}

describe("authStore", () => {
  it("publishes a retry request instead of silently ignoring it", () => {
    const storage = makeStorage({ multica_token: "t" });
    const api = makeApi();
    const store = createAuthStore({ api, storage });

    store.setState({ isLoading: true, status: "recovering" });
    store.getState().retryAuthentication();

    expect(store.getState().status).toBe("authenticating");
    expect(store.getState().retryGeneration).toBe(1);
  });

  it("explicit logout still clears credentials and publishes unauthenticated state", () => {
    const storage = makeStorage({ multica_token: "t" });
    const api = makeApi();
    const onLogout = vi.fn();
    const store = createAuthStore({ api, storage, onLogout });

    store.setState({ user: fakeUser, status: "authenticated", isLoading: false });
    store.getState().logout();

    expect(storage.snapshot().multica_token).toBeUndefined();
    expect(api.setToken).toHaveBeenCalledWith(null);
    expect(onLogout).toHaveBeenCalledOnce();
    expect(store.getState().user).toBeNull();
    expect(store.getState().status).toBe("unauthenticated");
  });
});

describe("authStore.loginWithLdap", () => {
  const ldapUser: User = {
    id: "u-ldap",
    name: "Alice Zhang",
    email: "alice@corp.example.com",
    avatar_url: null,
  } as User;

  function makeLdapApi(impl?: () => Promise<unknown>) {
    return {
      setToken: vi.fn(),
      loginWithLdap: vi.fn(
        impl ?? (async () => ({ token: "jwt-abc", user: ldapUser })),
      ),
    } as unknown as ApiClient & { loginWithLdap: ReturnType<typeof vi.fn> };
  }

  it("signs the returned user in and persists the token in localStorage mode", async () => {
    const storage = makeStorage();
    const api = makeLdapApi();
    const onLogin = vi.fn();
    const store = createAuthStore({ api, storage, onLogin });

    const user = await store.getState().loginWithLdap("alice", "pw");

    expect(user).toBe(ldapUser);
    expect(api.loginWithLdap).toHaveBeenCalledWith("alice", "pw");
    expect(storage.snapshot().multica_token).toBe("jwt-abc");
    expect(api.setToken).toHaveBeenCalledWith("jwt-abc");
    expect(onLogin).toHaveBeenCalledOnce();
    expect(store.getState().user).toBe(ldapUser);
    expect(store.getState().status).toBe("authenticated");
  });

  it("leaves storage alone in cookie mode, where the server owns the session", async () => {
    const storage = makeStorage();
    const api = makeLdapApi();
    const store = createAuthStore({ api, storage, cookieAuth: true });

    await store.getState().loginWithLdap("alice", "pw");

    expect(storage.snapshot().multica_token).toBeUndefined();
    expect(api.setToken).not.toHaveBeenCalled();
    expect(store.getState().status).toBe("authenticated");
  });

  it("rejects and changes nothing when the directory refuses the credentials", async () => {
    const storage = makeStorage();
    const api = makeLdapApi(async () => {
      throw new ApiError("invalid username or password", 401, "Unauthorized");
    });
    const store = createAuthStore({ api, storage });
    store.setState({ user: null, isLoading: false, status: "unauthenticated" });

    await expect(store.getState().loginWithLdap("alice", "wrong")).rejects.toMatchObject({
      status: 401,
    });

    expect(store.getState().user).toBeNull();
    expect(store.getState().status).toBe("unauthenticated");
    expect(storage.snapshot().multica_token).toBeUndefined();
    expect(api.setToken).not.toHaveBeenCalled();
  });
});
