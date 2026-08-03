// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { createAuthStore, registerAuthStore } from "../auth";
import type { StorageAdapter, User, Workspace } from "../types";
import { workspaceKeys } from "../workspace/queries";
import { AuthInitializer } from "./auth-initializer";

const user = {
  id: "u1",
  name: "Alice",
  email: "alice@example.com",
  avatar_url: null,
  onboarded_at: "2026-01-01T00:00:00Z",
  onboarding_questionnaire: {},
  starter_content_state: "imported",
  language: null,
  profile_description: "",
  timezone: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} satisfies User;

const workspaces = [
  {
    id: "ws-1",
    name: "Loop",
    slug: "loop",
    description: null,
    context: null,
    settings: {},
    repos: [],
    issue_prefix: "LOOP",
    avatar_url: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
] satisfies Workspace[];

function storage(initial: Record<string, string> = {}): StorageAdapter {
  const data = { ...initial };
  return {
    getItem: (key) => data[key] ?? null,
    setItem: (key, value) => {
      data[key] = value;
    },
    removeItem: (key) => {
      delete data[key];
    },
  };
}

function api(): ApiClient {
  return {
    getConfig: vi.fn().mockRejectedValue(new Error("skip config")),
    getMe: vi.fn().mockResolvedValue(user),
    listWorkspaces: vi.fn().mockResolvedValue(workspaces),
    setToken: vi.fn(),
  } as unknown as ApiClient;
}

afterEach(() => {
  cleanup();
});

describe("AuthInitializer", () => {
  it.each([
    ["cookie", true, {}],
    ["token", false, { multica_token: "token" }],
  ] as const)(
    "seeds workspace list before opening the auth gate in %s mode",
    async (_mode, cookieAuth, initialStorage) => {
      const queryClient = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      const fakeApi = api();
      const fakeStorage = storage(initialStorage);
      setApiInstance(fakeApi);
      registerAuthStore(
        createAuthStore({
          api: fakeApi,
          storage: fakeStorage,
          cookieAuth,
        }),
      );

      const onLogin = vi.fn(() => {
        expect(queryClient.getQueryData(workspaceKeys.list())).toEqual(
          workspaces,
        );
      });

      render(
        <QueryClientProvider client={queryClient}>
          <AuthInitializer
            cookieAuth={cookieAuth}
            storage={fakeStorage}
            onLogin={onLogin}
          >
            <div />
          </AuthInitializer>
        </QueryClientProvider>,
      );

      await waitFor(() => expect(onLogin).toHaveBeenCalledTimes(1));
    },
  );
});
