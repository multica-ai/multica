import { Redirect, Stack } from "expo-router";
import { useAuthStore } from "@/data/auth-store";

export default function AuthLayout() {
  const user = useAuthStore((s) => s.user);

  // Declarative redirect out of the auth group the moment a user exists.
  // Both login flows (email verifyCode, Feishu loginWithFeishuCode) also
  // call `router.replace("/")` imperatively, but that fires in the same
  // tick the app returns to the foreground from the Feishu app / H5 — a
  // moment when React Navigation occasionally drops the navigation event,
  // leaving the user stranded on the login screen until a manual reload.
  // This guard is state-driven, not timing-driven: when `user` is set,
  // the layout re-renders and redirects to "/", which app/index.tsx then
  // resolves to /select-workspace or /[slug]/inbox. On logout
  // (onUnauthorized clears user) this is null again, so login renders.
  if (user) return <Redirect href="/" />;

  return <Stack screenOptions={{ headerShown: false }} />;
}
