import { Stack, Redirect, usePathname } from "expo-router";
import { useAuthStore } from "@/data/auth-store";

/**
 * Auth-required layout. A valid credential remains admitted while offline;
 * only the one-time upgrade edge case without a user snapshot lands on the
 * explicit /offline screen.
 *
 * Workspace membership is enforced one level deeper at [workspace]/_layout —
 * not here — because select-workspace.tsx itself is auth-required but
 * workspace-less.
 */
export default function AppLayout() {
  const user = useAuthStore((s) => s.user);
  const hasToken = useAuthStore((s) => s.hasToken);
  const pathname = usePathname();
  if (!hasToken) return <Redirect href="/login" />;
  if (!user && pathname !== "/offline") return <Redirect href="/offline" />;
  return <Stack screenOptions={{ headerShown: false }} />;
}
