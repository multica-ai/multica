import { Redirect } from "expo-router";

/**
 * Catch-all for unmatched routes. Redirects to root so index.tsx handles
 * auth-aware routing. Also prevents the Expo Router SDK 55 built-in error
 * page from crashing on window.location.origin (undefined in RN).
 */
export default function NotFound() {
  return <Redirect href="/" />;
}
