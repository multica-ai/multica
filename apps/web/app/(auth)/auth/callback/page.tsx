"use client";

import { Suspense, useEffect, useRef } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { api } from "@/shared/api";
import { setLoggedInCookie } from "@/features/auth/auth-cookie";

function OAuthCallbackContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const setUser = useAuthStore((s) => s.setUser);
  const hydrateWorkspace = useWorkspaceStore((s) => s.hydrateWorkspace);
  const hasRun = useRef(false);

  useEffect(() => {
    if (hasRun.current) return;
    hasRun.current = true;

    const token = searchParams.get("token");
    const next = searchParams.get("next") || "/issues";
    const error = searchParams.get("error");

    // Strip token from URL to prevent leaking via Referer/history.
    window.history.replaceState({}, "", window.location.pathname);

    if (error) {
      router.replace(`/login?error=${encodeURIComponent(error)}`);
      return;
    }

    if (!token) {
      router.replace("/login");
      return;
    }

    // Store the token and initialize the session.
    localStorage.setItem("multica_token", token);
    api.setToken(token);
    setLoggedInCookie();

    api
      .getMe()
      .then(async (user) => {
        setUser(user);
        const wsList = await api.listWorkspaces();
        const lastWsId = localStorage.getItem("multica_workspace_id");
        await hydrateWorkspace(wsList, lastWsId);
        router.replace(next);
      })
      .catch(() => {
        // Token was invalid — clear and redirect to login.
        localStorage.removeItem("multica_token");
        api.setToken(null);
        router.replace("/login");
      });
  }, [searchParams, router, setUser, hydrateWorkspace]);

  return (
    <div className="flex min-h-screen items-center justify-center">
      <p className="text-muted-foreground">Signing you in...</p>
    </div>
  );
}

export default function OAuthCallbackPage() {
  return (
    <Suspense fallback={null}>
      <OAuthCallbackContent />
    </Suspense>
  );
}
