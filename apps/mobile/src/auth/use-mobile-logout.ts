import { useCallback } from "react";
import { useAuthStore } from "@multica/core/auth";
import { useCoreQueryClient } from "@multica/core/provider";
import {
  clearWorkspaceStorage,
  defaultStorage,
} from "@multica/core/platform";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { disableCurrentMobilePushRegistration } from "../push/mobile-push-registration";

export function useMobileLogout() {
  const queryClient = useCoreQueryClient();
  const authLogout = useAuthStore((state) => state.logout);

  return useCallback(async () => {
    await disableCurrentMobilePushRegistration().catch(() => {});

    const cachedWorkspaces =
      queryClient.getQueryData<Workspace[]>(workspaceKeys.list()) ?? [];

    for (const workspace of cachedWorkspaces) {
      clearWorkspaceStorage(defaultStorage, workspace.slug);
    }

    queryClient.clear();
    authLogout();
  }, [authLogout, queryClient]);
}
