import { Stack, Redirect } from "expo-router";
import { useAuthStore } from "@/data/auth-store";

/**
 * Auth-required layout. Redirects to /login when no user is loaded.
 *
 * Workspace membership is enforced one level deeper at [workspace]/_layout —
 * not here — because select-workspace.tsx itself is auth-required but
 * workspace-less.
 */
export default function AppLayout() {
  const user = useAuthStore((s) => s.user);
  if (!user) return <Redirect href="/login" />;
  return <Stack screenOptions={{ headerShown: false }} />;
}
