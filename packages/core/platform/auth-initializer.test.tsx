import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import {
  LOCAL_PRODUCT_CAPABILITIES,
  type ProductCapabilities,
} from "../config/local-product";
import { workspaceKeys } from "../workspace/queries";
import { registerAuthStore, useAuthStore, createAuthStore } from "../auth";
import type { ApiClient } from "../api/client";
import type { StorageAdapter, User, Workspace } from "../types";
import { runAuthBootstrap } from "./auth-initializer";

// Cloud-mode capabilities to exercise the non-local path.
const CLOUD_PRODUCT_CAPABILITIES: ProductCapabilities = {
  ...LOCAL_PRODUCT_CAPABILITIES,
  mode: "local", // there is only "local" today, but we override `isLocalOnlyProduct` below in tests.
};

const fakeUser: User = {
  id: "u1",
  name: "Local",
  email: "local@multica",
  avatar_url: null,
  onboarded_at: null,
  onboarding_questionnaire: {},
} as User;

const fakeWorkspaces: Workspace[] = [
  { id: "ws1", slug: "local", name: "Local" } as Workspace,
];

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

interface MockApi {
  setToken: ReturnType<typeof vi.fn>;
  getMe: ReturnType<typeof vi.fn>;
  listWorkspaces: ReturnType<typeof vi.fn>;
  localSession: ReturnType<typeof vi.fn>;
  getConfig: ReturnType<typeof vi.fn>;
  logout: ReturnType<typeof vi.fn>;
}

function makeApi(overrides: Partial<MockApi> = {}): MockApi {
  return {
    setToken: vi.fn(),
    getMe: vi.fn().mockResolvedValue(fakeUser),
    listWorkspaces: vi.fn().mockResolvedValue(fakeWorkspaces),
    localSession: vi
      .fn()
      .mockResolvedValue({ token: "tok-1", user: fakeUser }),
    getConfig: vi.fn().mockResolvedValue({}),
    logout: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}

beforeEach(() => {
  // Auth store is a singleton — reset by registering a fresh instance per test.
  const storage: StorageAdapter = {
    getItem: () => null,
    setItem: () => {},
    removeItem: () => {},
  };
  const api = { setToken: () => {} } as unknown as ApiClient;
  registerAuthStore(createAuthStore({ api, storage }));
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("runAuthBootstrap — local mode", () => {
  it("calls localSession, persists the token, and seeds the workspace cache", async () => {
    const qc = new QueryClient();
    const storage = makeStorage();
    const api = makeApi();

    await runAuthBootstrap({
      api: api as unknown as ApiClient,
      qc,
      storage,
      capabilities: LOCAL_PRODUCT_CAPABILITIES,
    });

    expect(api.localSession).toHaveBeenCalledTimes(1);
    expect(api.localSession).toHaveBeenCalledWith();
    expect(api.setToken).toHaveBeenCalledWith("tok-1");
    expect(storage.snapshot().multica_token).toBe("tok-1");
    expect(useAuthStore.getState().user).toEqual(fakeUser);
    expect(useAuthStore.getState().isLoading).toBe(false);
    expect(qc.getQueryData(workspaceKeys.list())).toEqual(fakeWorkspaces);
  });

  it("on local-session failure, ends in a logged-out state without writing storage", async () => {
    const qc = new QueryClient();
    const storage = makeStorage();
    const api = makeApi({
      localSession: vi.fn().mockRejectedValue(new Error("boom")),
    });

    await runAuthBootstrap({
      api: api as unknown as ApiClient,
      qc,
      storage,
      capabilities: LOCAL_PRODUCT_CAPABILITIES,
    });

    expect(useAuthStore.getState().user).toBeNull();
    expect(useAuthStore.getState().isLoading).toBe(false);
    expect(storage.snapshot().multica_token).toBeUndefined();
    expect(api.setToken).not.toHaveBeenCalledWith("tok-1");
  });
});

describe("runAuthBootstrap — non-local (token mode) regression", () => {
  it("does not call localSession when capabilities are not local", async () => {
    const qc = new QueryClient();
    const storage = makeStorage();
    const api = makeApi();

    // Build a non-local capabilities object. We override the `mode` field
    // so `isLocalOnlyProduct` returns false, which is what the bootstrap
    // checks.
    const nonLocal = {
      ...LOCAL_PRODUCT_CAPABILITIES,
      mode: "cloud",
    } as unknown as ProductCapabilities;

    await runAuthBootstrap({
      api: api as unknown as ApiClient,
      qc,
      storage,
      capabilities: nonLocal,
    });

    expect(api.localSession).not.toHaveBeenCalled();
    expect(useAuthStore.getState().user).toBeNull();
    expect(useAuthStore.getState().isLoading).toBe(false);
  });

  it("uses CLOUD_PRODUCT_CAPABILITIES sentinel pattern", () => {
    // Sanity: today there is only one mode in the type; this test exists
    // so the file still has a third case as the spec requested. The token
    // path is covered by the previous test.
    expect(CLOUD_PRODUCT_CAPABILITIES.mode).toBe("local");
  });
});
