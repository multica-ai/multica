"use client";

import { useMemo } from "react";
import { create } from "zustand";
import type { User } from "@/shared/types";
import { api } from "@/shared/api";
import {
  getAppQueryClient,
  prepareQueryCacheForLogin,
  prepareQueryCacheForLogout,
  queryKeys,
} from "@/shared/query";
import { useCurrentUserQuery } from "./queries";
import { setLoggedInCookie, clearLoggedInCookie } from "./auth-cookie";

interface AuthSessionState {
  isLoading: boolean;
  initialize: () => Promise<void>;
  sendCode: (email: string) => Promise<void>;
  verifyCode: (email: string, code: string) => Promise<User>;
  logout: () => void;
  setUser: (user: User) => void;
}

type AuthStoreState = AuthSessionState & {
  user: User | null;
};

type AuthStoreSelector<T> = (state: AuthStoreState) => T;

interface AuthStoreHook {
  <T>(selector: AuthStoreSelector<T>): T;
  getState: () => AuthStoreState;
  setState: (partial: Partial<AuthStoreState>) => void;
}

const useAuthSessionStore = create<AuthSessionState>((set) => ({
  isLoading: true,

  initialize: async () => {
    const token = localStorage.getItem("multica_token");
    if (!token) {
      void prepareQueryCacheForLogout(getAppQueryClient());
      set({ isLoading: false });
      return;
    }

    api.setToken(token);
  },

  sendCode: async (email: string) => {
    await api.sendCode(email);
  },

  verifyCode: async (email: string, code: string) => {
    const { token, user } = await api.verifyCode(email, code);
    await prepareQueryCacheForLogin(getAppQueryClient());
    localStorage.setItem("multica_token", token);
    api.setToken(token);
    setLoggedInCookie();
    getAppQueryClient().setQueryData(queryKeys.session.me(), user);
    set({ isLoading: false });
    return user;
  },

  logout: () => {
    void prepareQueryCacheForLogout(getAppQueryClient());
    localStorage.removeItem("multica_token");
    localStorage.removeItem("multica_workspace_id");
    api.setToken(null);
    api.setWorkspaceId(null);
    clearLoggedInCookie();
    set({ isLoading: false });
  },

  setUser: (user: User) => {
    getAppQueryClient().setQueryData(queryKeys.session.me(), user);
  },
}));

function getAuthSnapshot(): AuthStoreState {
  return {
    ...useAuthSessionStore.getState(),
    user:
      getAppQueryClient().getQueryData<User | null>(queryKeys.session.me()) ??
      null,
  };
}

export const useAuthStore = ((selector: AuthStoreSelector<unknown>) => {
  const sessionState = useAuthSessionStore();
  const currentUserQuery = useCurrentUserQuery();

  const snapshot = useMemo<AuthStoreState>(
    () => ({
      ...sessionState,
      user: currentUserQuery.data ?? null,
    }),
    [currentUserQuery.data, sessionState],
  );

  return selector(snapshot);
}) as AuthStoreHook;

useAuthStore.getState = getAuthSnapshot;

useAuthStore.setState = (partial) => {
  if (Object.prototype.hasOwnProperty.call(partial, "user")) {
    getAppQueryClient().setQueryData(
      queryKeys.session.me(),
      partial.user ?? null,
    );
  }

  const localPartial: Partial<AuthSessionState> = {};
  if (Object.prototype.hasOwnProperty.call(partial, "isLoading")) {
    localPartial.isLoading = partial.isLoading ?? false;
  }

  if (Object.keys(localPartial).length > 0) {
    useAuthSessionStore.setState(localPartial);
  }
};
