"use client";

import { useEffect } from "react";
import { useNavigationStore } from "@multica/core/navigation";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceStore } from "@multica/core/workspace";
import { useNavigation } from "../navigation";

export function useDashboardGuard(loginPath = "/", onboardingPath?: string) {
  const { pathname, push } = useNavigation();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const workspace = useWorkspaceStore((s) => s.workspace);

  useEffect(() => {
    if (isLoading) return;
    if (!user) {
      push(loginPath);
      return;
    }
    if (!workspace && onboardingPath) {
      push(onboardingPath);
    }
  }, [user, isLoading, workspace, push, loginPath, onboardingPath]);

  useEffect(() => {
    useNavigationStore.getState().onPathChange(pathname);
  }, [pathname]);

  return { user, isLoading, workspace };
}
