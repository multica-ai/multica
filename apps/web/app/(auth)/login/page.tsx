"use client";

import { Suspense, useEffect } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceStore } from "@multica/core/workspace";
import { setLoggedInCookie } from "@/features/auth/auth-cookie";
import { LoginPage, validateCliCallback } from "@multica/views/auth";

const googleClientId = process.env.NEXT_PUBLIC_GOOGLE_CLIENT_ID;

function LoginPageContent() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const searchParams = useSearchParams();

  const cliCallbackRaw = searchParams.get("cli_callback");
  const cliState = searchParams.get("cli_state") || "";
  const platform = searchParams.get("platform");
  const nextUrl = searchParams.get("next") || "/issues";

  // Already authenticated — redirect to dashboard (skip if CLI callback)
  useEffect(() => {
    if (!isLoading && user && !cliCallbackRaw) {
      router.replace(nextUrl);
    }
  }, [isLoading, user, router, nextUrl, cliCallbackRaw]);

  const lastWorkspaceId =
    typeof window !== "undefined"
      ? localStorage.getItem("multica_workspace_id")
      : null;

  const handleSuccess = () => {
    const ws = useWorkspaceStore.getState().workspace;
    router.push(ws ? nextUrl : "/onboarding");
  };

  // Build Google OAuth state: encode platform + next URL so the callback
  // can redirect to the right place after login.
  const googleState = [
    platform === "desktop" ? "platform:desktop" : "",
    nextUrl !== "/issues" ? `next:${nextUrl}` : "",
  ]
    .filter(Boolean)
    .join(",") || undefined;

  return (
    <LoginPage
      onSuccess={handleSuccess}
      google={
        googleClientId
          ? {
              clientId: googleClientId,
              redirectUri: `${window.location.origin}/auth/callback`,
              state: googleState,
            }
          : undefined
      }
      cliCallback={
        cliCallbackRaw && validateCliCallback(cliCallbackRaw)
          ? { url: cliCallbackRaw, state: cliState }
          : undefined
      }
      lastWorkspaceId={lastWorkspaceId}
      onTokenObtained={setLoggedInCookie}
    />
  );
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <LoginPageContent />
    </Suspense>
  );
}
