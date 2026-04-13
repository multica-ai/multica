"use client";

import { useEffect } from "react";
import { useNavigationStore } from "@multica/core/navigation";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceStore } from "@multica/core/workspace";
import { useNavigation } from "../navigation";

export function useDashboardGuard(loginPath = "/", onboardingPath?: string) {
  const { pathname, replace } = useNavigation();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const workspace = useWorkspaceStore((s) => s.workspace);

  useEffect(() => {
    if (isLoading) return;
    if (!user) {
      replace(loginPath);
      return;
    }
    if (!workspace && onboardingPath) {
      replace(onboardingPath);
    }
  }, [user, isLoading, workspace, replace, loginPath, onboardingPath]);

  useEffect(() => {
    useNavigationStore.getState().onPathChange(pathname);
  }, [pathname]);

  return { user, isLoading, workspace };
}
