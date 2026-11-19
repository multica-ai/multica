import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { clearClientSessionData } from "@multica/core/platform";

/** Ends the session and erases what it left on the device, like the web's logout. */
export function useSignOut(): () => void {
  const queryClient = useQueryClient();
  const logout = useAuthStore((state) => state.logout);
  return useCallback(() => {
    clearClientSessionData(queryClient);
    logout();
  }, [queryClient, logout]);
}
