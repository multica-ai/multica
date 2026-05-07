"use client";

import { Suspense, useEffect, useMemo, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { sanitizeNextUrl, useAuthStore } from "@multica/core/auth";
import { useConfigStore } from "@multica/core/config";
import { workspaceKeys } from "@multica/core/workspace/queries";
import {
  paths,
  resolvePostAuthDestination,
  useHasOnboarded,
} from "@multica/core/paths";
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
import { captureDownloadIntent } from "@multica/core/analytics";
import { setLoggedInCookie } from "@/features/auth/auth-cookie";
import Link from "next/link";
import {
  LoginPage,
  buildCliOAuthStatePart,
  validateCliCallback,
} from "@multica/views/auth";
import { useT } from "@multica/views/i18n";

const googleClientId = process.env.NEXT_PUBLIC_GOOGLE_CLIENT_ID;
const buildTimeDingTalkClientId = process.env.NEXT_PUBLIC_DINGTALK_CLIENT_ID;
const buildTimeHideEmailLogin = process.env.NEXT_PUBLIC_HIDE_EMAIL_LOGIN === "true";
const mobileAuthCallback = "wujieai-multicam://auth/callback";

interface RuntimeAuthConfig {
  googleClientId?: string;
  dingtalkClientId?: string;
  dingtalkOAuthScope?: string;
  hideEmailLogin?: boolean;
}

/**
 * Pick where a logged-in user with no explicit `?next=` should land.
 * Un-onboarded users with pending invitations on their email get routed to
 * the batch /invitations page; everyone else falls through to the standard
 * resolver. A network blip on listMyInvitations is non-fatal — we fall
 * through rather than trap the user on an error screen.
 */
async function resolveLoggedInDestination(
  qc: QueryClient,
  hasOnboarded: boolean,
  workspaces: Workspace[],
): Promise<string> {
  if (!hasOnboarded) {
    try {
      const invites = await api.listMyInvitations();
      if (invites.length > 0) {
        qc.setQueryData(workspaceKeys.myInvitations(), invites);
        return paths.invitations();
      }
    } catch {
      // fall through
    }
  }
  return resolvePostAuthDestination(workspaces, hasOnboarded);
}

function LoginPageContent() {
  const router = useRouter();
  const qc = useQueryClient();
  const { t } = useT("auth");
  const googleClientId = useConfigStore((state) => state.googleClientId);
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const searchParams = useSearchParams();

  const cliCallbackRaw = searchParams.get("cli_callback");
  const cliState = searchParams.get("cli_state") || "";
  const hasValidCliCallback = Boolean(
    cliCallbackRaw && validateCliCallback(cliCallbackRaw),
  );
  const cliCallback = useMemo(
    () =>
      cliCallbackRaw && hasValidCliCallback
        ? { url: cliCallbackRaw, state: cliState }
        : undefined,
    [cliCallbackRaw, cliState, hasValidCliCallback],
  );
  const cliOAuthStatePart = useMemo(
    () => (cliCallback ? buildCliOAuthStatePart(cliCallback) : ""),
    [cliCallback],
  );
  const platform = searchParams.get("platform");
  const isDesktopHandoff = platform === "desktop" && !cliCallbackRaw;
  const isMobileHandoff = platform === "mobile" && !cliCallbackRaw;
  const requestedProvider = searchParams.get("provider");
  // `next` carries a protected URL the user was originally headed to
  // (e.g. /invite/{id}). With URL-driven workspaces there is no legacy
  // "/issues" default — if `next` is absent we decide after login based on
  // the user's workspace list. Sanitize first so a crafted `?next=https://evil`
  // cannot bounce the user off-origin after a successful login.
  const nextUrl = sanitizeNextUrl(searchParams.get("next"));

  const [desktopToken, setDesktopToken] = useState<string | null>(null);
  const [desktopError, setDesktopError] = useState("");
  const [runtimeAuthConfig, setRuntimeAuthConfig] =
    useState<RuntimeAuthConfig>({});
  const hasOnboarded = useHasOnboarded();

  useEffect(() => {
    api
      .getConfig()
      .then((cfg) => {
        setRuntimeAuthConfig({
          googleClientId: cfg.google_client_id || undefined,
          dingtalkClientId: cfg.dingtalk_client_id || undefined,
          dingtalkOAuthScope: cfg.dingtalk_oauth_scope || undefined,
          hideEmailLogin: cfg.hide_email_login,
        });
      })
      .catch(() => {
        // Runtime config is optional; build-time env keeps existing deployments working.
      });
  }, []);

  const dingtalkClientId =
    runtimeAuthConfig.dingtalkClientId || buildTimeDingTalkClientId;
  const resolvedGoogleClientId =
    runtimeAuthConfig.googleClientId || googleClientId;
  const hideEmailLogin =
    runtimeAuthConfig.hideEmailLogin ?? buildTimeHideEmailLogin;

  // Already authenticated — honor ?next= or fall back to first workspace
  // (or /onboarding if the user has none). Skip this entire path when
  // the user arrived to authorize the CLI.
  useEffect(() => {
    if (isLoading || !user || hasValidCliCallback) return;
    if (isDesktopHandoff || isMobileHandoff) {
      // Native clients opened the browser for login but the web session is
      // already authenticated — mint a bearer token from the cookie session
      // and hand it off via deep link instead of redirecting to a workspace.
      api
        .issueCliToken()
        .then(({ token }) => {
          setDesktopToken(token);
          const callbackUrl = isMobileHandoff
            ? mobileAuthCallback
            : "multica://auth/callback";
          window.location.href = `${callbackUrl}?token=${encodeURIComponent(token)}`;
        })
        .catch((err) => {
          setDesktopError(
err instanceof Error
              ? err.message
              : t(($) => $.web.desktop_handoff.prepare_failed),
          );
        });
      return;
    }
    if (nextUrl) {
      router.replace(nextUrl);
      return;
    }
    const list = qc.getQueryData<Workspace[]>(workspaceKeys.list()) ?? [];
void resolveLoggedInDestination(qc, hasOnboarded, list).then((dest) =>
      router.replace(dest),
    );
  }, [isLoading, user, router, nextUrl, hasValidCliCallback, isDesktopHandoff, isMobileHandoff, hasOnboarded, qc]);

  const handleSuccess = async () => {
    // Read the latest user snapshot directly — the closure's `hasOnboarded`
    // was captured before login completed and would be stale here.
    const currentUser = useAuthStore.getState().user;
    const onboarded = currentUser?.onboarded_at != null;
    if (nextUrl) {
      router.push(nextUrl);
      return;
    }
    const list = qc.getQueryData<Workspace[]>(workspaceKeys.list()) ?? [];
    const dest = await resolveLoggedInDestination(qc, onboarded, list);
    router.push(dest);
  };

  // Build Google OAuth state: encode platform + next URL so the callback
  // can redirect to the right place after login.
  const googleState = [
    platform === "desktop" ? "platform:desktop" : "",
    platform === "mobile" ? "platform:mobile" : "",
    nextUrl ? `next:${nextUrl}` : "",
    cliOAuthStatePart,
  ]
    .filter(Boolean)
    .join(",") || undefined;

  const dingtalkState = [
    platform === "desktop" ? "platform:desktop" : "",
    platform === "mobile" ? "platform:mobile" : "",
    nextUrl ? `next:${nextUrl}` : "",
    cliOAuthStatePart,
  ]
    .filter(Boolean)
    .join(",") || undefined;

  useEffect(() => {
    if (user || hasValidCliCallback || !isMobileHandoff) return;
    if (requestedProvider !== "google" && requestedProvider !== "dingtalk") {
      return;
    }

    if (requestedProvider === "google" && resolvedGoogleClientId) {
      const params = new URLSearchParams({
        client_id: resolvedGoogleClientId,
        redirect_uri: `${window.location.origin}/auth/callback`,
        response_type: "code",
        scope: "openid email profile",
        access_type: "offline",
        prompt: "select_account",
      });
      if (googleState) params.set("state", googleState);
      window.location.href = `https://accounts.google.com/o/oauth2/v2/auth?${params}`;
      return;
    }

    if (requestedProvider === "dingtalk" && dingtalkClientId) {
      const params = new URLSearchParams({
        client_id: dingtalkClientId,
        redirect_uri: `${window.location.origin}/auth/callback`,
        response_type: "code",
        scope: runtimeAuthConfig.dingtalkOAuthScope || "openid corpid Contact.User.Read",
        prompt: "consent",
      });
      const stateParts = ["provider:dingtalk"];
      if (dingtalkState) stateParts.push(dingtalkState);
      params.set("state", stateParts.join(","));
      window.location.href = `https://login.dingtalk.com/oauth2/auth?${params}`;
    }
  }, [
    dingtalkClientId,
    dingtalkState,
    googleState,
    hasValidCliCallback,
    isMobileHandoff,
    requestedProvider,
    resolvedGoogleClientId,
    runtimeAuthConfig.dingtalkOAuthScope,
    user,
  ]);

  // While the desktop handoff is in progress (or has produced a token/error),
  // render a dedicated screen instead of flashing the login form or redirecting
  // away to a workspace page.
  if ((isDesktopHandoff || isMobileHandoff) && user) {
    const appName = isMobileHandoff ? "Multica mobile app" : "Multica desktop app";
    const openLabel = isMobileHandoff ? "Open Multica Mobile" : "Open Multica Desktop";
    const callbackUrl = isMobileHandoff ? mobileAuthCallback : "multica://auth/callback";

    if (desktopError) {
      return (
        <div className="flex min-h-screen items-center justify-center">
          <Card className="w-full max-w-sm">
            <CardHeader className="text-center">
              <CardTitle className="text-2xl">
                {t(($) => $.web.desktop_handoff.failed_title)}
              </CardTitle>
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
            <CardTitle className="text-2xl">
              {t(($) => $.web.desktop_handoff.opening_title)}
            </CardTitle>
            <CardDescription>
              {desktopToken
? t(($) => $.web.desktop_handoff.opening_description)
                : t(($) => $.web.desktop_handoff.preparing)}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex justify-center">
            {desktopToken ? (
              <Button
                variant="outline"
                onClick={() => {
                  window.location.href = `${callbackUrl}?token=${encodeURIComponent(desktopToken)}`;
                }}
              >
{t(($) => $.web.desktop_handoff.open_button)}
              </Button>
            ) : (
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            )}
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <LoginPage
      onSuccess={handleSuccess}
      google={
        resolvedGoogleClientId
          ? {
              clientId: resolvedGoogleClientId,
              redirectUri: `${window.location.origin}/auth/callback`,
              state: googleState,
            }
          : undefined
      }
      dingtalk={
        dingtalkClientId
          ? {
              clientId: dingtalkClientId,
              redirectUri: `${window.location.origin}/auth/callback`,
              state: dingtalkState,
              scope: runtimeAuthConfig.dingtalkOAuthScope,
            }
          : undefined
      }
      hideEmailLogin={hideEmailLogin}
      cliCallback={cliCallback}
      onTokenObtained={setLoggedInCookie}
      extra={
        <span className="text-xs text-muted-foreground">
          {t(($) => $.web.prefer_desktop)}{" "}
          <Link
            href="/download"
            onClick={() => captureDownloadIntent("login")}
            className="font-medium text-foreground underline decoration-foreground/30 underline-offset-4 hover:decoration-foreground/70"
          >
            {t(($) => $.web.download)}
          </Link>
        </span>
      }
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
