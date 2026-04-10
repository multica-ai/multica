"use client";

import { useEffect } from "react";
import { useNavigationStore } from "@multica/core/navigation";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceStore } from "@multica/core/workspace";
import { useNavigation } from "../navigation";

export function useDashboardGuard(loginPath = "/") {
  const { pathname, push } = useNavigation();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const workspace = useWorkspaceStore((s) => s.workspace);

  useEffect(() => {
    if (!isLoading && !user) push(loginPath);
  }, [user, isLoading, push, loginPath]);

  useEffect(() => {
    useNavigationStore.getState().onPathChange(pathname);
  }, [pathname]);

  return { user, isLoading, workspace };
}
