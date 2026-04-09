import { clearLoggedInCookie } from "@/features/auth/auth-cookie";

export function clearStoredSession() {
  if (typeof window === "undefined") return;

  localStorage.removeItem("multica_token");
  localStorage.removeItem("multica_workspace_id");
  clearLoggedInCookie();
}

export function getUnauthorizedRedirectPath(pathname: string): string | null {
  if (pathname === "/" || pathname === "/login" || pathname.startsWith("/auth/")) {
    return null;
  }

  return "/";
}
