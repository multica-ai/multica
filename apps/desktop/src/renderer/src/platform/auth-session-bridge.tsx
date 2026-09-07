import { useEffect } from "react";
import { useAuthStore, type AuthStatus } from "@multica/core/auth";

export function authSessionReportValue(
  status: AuthStatus,
  userId: string | null,
): string | null | undefined {
  if (status === "authenticated") return userId ?? undefined;
  if (status === "unauthenticated") return null;
  return undefined;
}

/**
 * Keep the main process informed of definitive account transitions without
 * sharing credentials between renderer processes. Recoverable startup errors
 * deliberately emit nothing so issue windows and tab state remain intact.
 */
export function DesktopAuthSessionBridge() {
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const status = useAuthStore((state) => state.status);

  useEffect(() => {
    const reportValue = authSessionReportValue(status, userId);
    if (reportValue === undefined) return;
    // Optional chaining keeps renderer HMR safe during the brief interval in
    // which an old preload is still attached to the refreshed React tree.
    window.desktopAPI.reportAuthSession?.(reportValue);
  }, [status, userId]);

  return null;
}
