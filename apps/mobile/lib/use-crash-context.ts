import { useEffect } from "react";
import { usePathname } from "expo-router";
import { useWorkspaceStore } from "@/data/workspace-store";
import { setCrashRouteContext, setCrashWorkspaceContext } from "@/lib/crash-reporting";

export function useCrashContext() {
  const pathname = usePathname();
  const workspaceSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  useEffect(() => {
    setCrashRouteContext(pathname);
  }, [pathname]);

  useEffect(() => {
    setCrashWorkspaceContext(workspaceSlug);
  }, [workspaceSlug]);
}
