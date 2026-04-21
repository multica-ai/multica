"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { sanitizeNextUrl, useAuthStore } from "@multica/core/auth";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { paths } from "@multica/core/paths";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@multica/ui/components/ui/card";
import { Button } from "@multica/ui/components/ui/button";
import { Loader2 } from "lucide-react";
import { setLoggedInCookie } from "@/features/auth/auth-cookie";
import { LoginPage, validateCliCallback } from "@multica/views/auth";

const googleClientId = process.env.NEXT_PUBLIC_GOOGLE_CLIENT_ID;
const feishuAppId = process.env.NEXT_PUBLIC_FEISHU_APP_ID;

function LoginPageContent() {
  const router = useRouter();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const searchParams = useSearchParams();

  const cliCallbackRaw = searchParams.get("cli_callback");
  const cliState = searchParams.get("cli_state") || "";
  const platform = searchParams.get("platform");
  const isDesktopHandoff = platform === "desktop" && !cliCallbackRaw;
  // `next` carries a protected URL the user was originally headed to
  // (e.g. /invite/{id}). With URL-driven workspaces there is no legacy
  // "/issues" default — if `next` is absent we decide after login based on
  // the user's workspace list. Sanitize first so a crafted `?next=https://evil`
  // cannot bounce the user off-origin after a successful login.
  const nextUrl = sanitizeNextUrl(searchParams.get("next"));

  const [desktopToken, setDesktopToken] = useState<string | null>(null);
  const [desktopError, setDesktopError] = useState("");

  // Already authenticated — honor ?next= or fall back to first workspace
  // (or /workspaces/new if the user has none). Skip this entire path when
  // the user arrived to authorize the CLI.
  useEffect(() => {
    if (isLoading || !user || cliCallbackRaw) return;
    if (isDesktopHandoff) {
      // Desktop opened the browser for login but the web session is already
      // authenticated — mint a bearer token from the cookie session and hand
      // it off via deep link instead of silently redirecting to the workspace.
      api
        .issueCliToken()
        .then(({ token }) => {
          setDesktopToken(token);
          window.location.href = `multica://auth/callback?token=${encodeURIComponent(token)}`;
        })
        .catch((err) => {
          setDesktopError(
            err instanceof Error ? err.message : "Failed to prepare Desktop sign-in",
          );
        });
      return;
    }
    if (nextUrl) {
      router.replace(nextUrl);
      return;
    }
    const list = qc.getQueryData<Workspace[]>(workspaceKeys.list()) ?? [];
    const [first] = list;
    router.replace(
      first ? paths.workspace(first.slug).issues() : paths.newWorkspace(),
    );
  }, [isLoading, user, router, nextUrl, cliCallbackRaw, isDesktopHandoff, qc]);

  const handleSuccess = () => {
    if (nextUrl) {
      router.push(nextUrl);
      return;
    }
    // The LoginPage view populates the workspace list cache before calling
    // onSuccess, so it's safe to read here.
    const list = qc.getQueryData<Workspace[]>(workspaceKeys.list()) ?? [];
    const [first] = list;
    router.push(
      first ? paths.workspace(first.slug).issues() : paths.newWorkspace(),
    );
  };

  // Build OAuth state: encode platform + next URL so the callback can
  // redirect to the right place after login. Shared between Google and
  // Feishu since the encoding is the same.
  const oauthState = [
    platform === "desktop" ? "platform:desktop" : "",
    nextUrl ? `next:${nextUrl}` : "",
  ]
    .filter(Boolean)
    .join(",") || undefined;

  // While the desktop handoff is in progress (or has produced a token/error),
  // render a dedicated screen instead of flashing the login form or redirecting
  // away to a workspace page.
  if (isDesktopHandoff && user) {
    if (desktopError) {
      return (
        <div className="flex min-h-screen items-center justify-center">
          <Card className="w-full max-w-sm">
            <CardHeader className="text-center">
              <CardTitle className="text-2xl">Sign-in Failed</CardTitle>
              <CardDescription>{desktopError}</CardDescription>
            </CardHeader>
          </Card>
        </div>
      );
    }
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            <CardTitle className="text-2xl">Opening Multica</CardTitle>
            <CardDescription>
              {desktopToken
                ? "You should see a prompt to open the Multica desktop app. If nothing happens, click the button below."
                : "Preparing Desktop sign-in..."}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex justify-center">
            {desktopToken ? (
              <Button
                variant="outline"
                onClick={() => {
                  window.location.href = `multica://auth/callback?token=${encodeURIComponent(desktopToken)}`;
                }}
              >
                Open Multica Desktop
              </Button>
            ) : (
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            )}
          </CardContent>
        </Card>
      </div>
    );
  }

  // Next.js will SSR this client component on first request, so window is
  // undefined during the server render. Compute origin lazily and substitute
  // an empty string on the server; hydration replaces it with the real value
  // before the user can click either OAuth button.
  const origin = typeof window !== "undefined" ? window.location.origin : "";

  return (
    <LoginPage
      onSuccess={handleSuccess}
      google={
        googleClientId
          ? {
              clientId: googleClientId,
              redirectUri: `${origin}/auth/callback`,
              state: oauthState,
            }
          : undefined
      }
      feishu={
        feishuAppId
          ? {
              appId: feishuAppId,
              redirectUri: `${origin}/auth/feishu/callback`,
              state: oauthState,
            }
          : undefined
      }
      cliCallback={
        cliCallbackRaw && validateCliCallback(cliCallbackRaw)
          ? { url: cliCallbackRaw, state: cliState }
          : undefined
      }
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
