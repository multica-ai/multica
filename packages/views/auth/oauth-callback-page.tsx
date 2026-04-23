"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  buildDesktopDeepLink,
  decodeOAuthState,
  sanitizeNextUrl,
  useAuthStore,
} from "@multica/core/auth";
import { useConfigStore } from "@multica/core/config";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { paths, resolvePostAuthDestination } from "@multica/core/paths";
import { api } from "@multica/core/api";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@multica/ui/components/ui/card";
import { Button } from "@multica/ui/components/ui/button";
import { Loader2 } from "lucide-react";
import { useNavigation } from "../navigation/context";

export function OAuthCallbackPage() {
  const { searchParams, push } = useNavigation();
  const qc = useQueryClient();
  const loginWithOAuth = useAuthStore((s) => s.loginWithOAuth);
  const oauthProviders = useConfigStore((s) => s.oauthProviders);
  const [error, setError] = useState("");
  const [desktopToken, setDesktopToken] = useState<string | null>(null);
  const exchangedRef = useRef(false);

  useEffect(() => {
    const code = searchParams.get("code");
    if (!code) {
      setError("Missing authorization code");
      return;
    }

    const errorParam = searchParams.get("error");
    if (errorParam) {
      setError(errorParam === "access_denied" ? "Access denied" : errorParam);
      return;
    }

    // Wait for /api/config — otherwise the provider allowlist is empty and
    // a valid provider state would be rejected here.
    if (Object.keys(oauthProviders).length === 0) return;

    // Single-use: an OAuth code can only be exchanged once; StrictMode, effect
    // re-runs, or the user refreshing the tab must not trigger a second POST.
    if (exchangedRef.current) return;

    const { providerId, platform, next, nonce } = decodeOAuthState(
      searchParams.get("state"),
    );
    const isDesktop = platform === "desktop";
    const nextUrl = sanitizeNextUrl(next ?? null);

    const providerCfg = providerId ? oauthProviders[providerId] : undefined;
    if (!providerId || !providerCfg) {
      setError("This sign-in method is not enabled on this instance");
      return;
    }
    if (!nonce) {
      setError("Missing OAuth state");
      return;
    }

    const redirectUri = `${window.location.origin}${providerCfg.callbackPath}`;
    exchangedRef.current = true;

    if (isDesktop) {
      api
        .oauthLogin(providerId, code, redirectUri, nonce)
        .then(({ token }) => {
          setDesktopToken(token);
          window.location.href = buildDesktopDeepLink(token);
        })
        .catch((err) => {
          setError(err instanceof Error ? err.message : "Login failed");
        });
      return;
    }

    loginWithOAuth(providerId, code, redirectUri, nonce)
      .then(async (loggedInUser) => {
        const wsList = await api.listWorkspaces();
        qc.setQueryData(workspaceKeys.list(), wsList);
        const onboarded = loggedInUser.onboarded_at != null;
        if (!onboarded) {
          push(paths.onboarding());
          return;
        }
        push(nextUrl || resolvePostAuthDestination(wsList, onboarded));
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Login failed");
      });
  }, [searchParams, loginWithOAuth, oauthProviders, push, qc]);

  if (desktopToken) {
    return (
      <CallbackCard title="Opening Multica" description="You should see a prompt to open the Multica desktop app. If nothing happens, click the button below.">
        <Button
          variant="outline"
          onClick={() => {
            window.location.href = buildDesktopDeepLink(desktopToken);
          }}
        >
          Open Multica Desktop
        </Button>
      </CallbackCard>
    );
  }

  if (error) {
    return (
      <CallbackCard title="Login Failed" description={error}>
        <a
          href={paths.login()}
          className="text-primary underline-offset-4 hover:underline"
        >
          Back to login
        </a>
      </CallbackCard>
    );
  }

  return (
    <CallbackCard title="Signing in..." description="Please wait while we complete your login">
      <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
    </CallbackCard>
  );
}

function CallbackCard({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <div className="flex min-h-screen items-center justify-center">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-2xl">{title}</CardTitle>
          <CardDescription>{description}</CardDescription>
        </CardHeader>
        <CardContent className="flex justify-center">{children}</CardContent>
      </Card>
    </div>
  );
}
