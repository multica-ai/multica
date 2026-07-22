"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { sanitizeNextUrl, useAuthStore } from "@multica/core/auth";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { paths, resolvePostAuthDestination } from "@multica/core/paths";
import { api } from "@multica/core/api";
import type { User } from "@multica/core/types";
import { validateCliCallback, redirectToCliCallback } from "@multica/views/auth";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@multica/ui/components/ui/card";
import { Button } from "@multica/ui/components/ui/button";
import { Loader2 } from "lucide-react";

function CallbackContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const qc = useQueryClient();
  const loginWithGoogle = useAuthStore((s) => s.loginWithGoogle);
  const loginWithOIDC = useAuthStore((s) => s.loginWithOIDC);
  const [error, setError] = useState("");
  const [desktopToken, setDesktopToken] = useState<string | null>(null);

  useEffect(() => {
    const errorParam = searchParams.get("error");
    if (errorParam) {
      setError(errorParam === "access_denied" ? "Access denied" : errorParam);
      return;
    }

    const code = searchParams.get("code");
    if (!code) {
      setError("Missing authorization code");
      return;
    }

    const state = searchParams.get("state") || "";
    const redirectUri = `${window.location.origin}/auth/callback`;

    const parseAppState = (appState: string) => {
      const stateParts = appState.split(",");
      const nextPart = stateParts.find((part) => part.startsWith("next:"));
      const cliCallbackPart = stateParts.find((part) =>
        part.startsWith("cli_callback:"),
      );
      const cliStatePart = stateParts.find((part) =>
        part.startsWith("cli_state:"),
      );
      let cliCallbackRaw: string | null = null;
      let cliState = "";
      try {
        cliCallbackRaw = cliCallbackPart
          ? decodeURIComponent(cliCallbackPart.slice("cli_callback:".length))
          : null;
        cliState = cliStatePart
          ? decodeURIComponent(cliStatePart.slice("cli_state:".length))
          : "";
      } catch {
        cliCallbackRaw = null;
      }
      return {
        isDesktop: stateParts.includes("platform:desktop"),
        nextUrl: sanitizeNextUrl(nextPart ? nextPart.slice(5) : null),
        cliCallback:
          cliCallbackRaw && validateCliCallback(cliCallbackRaw)
            ? cliCallbackRaw
            : null,
        cliState,
      };
    };

    const completeWebLogin = async (loggedInUser: User, nextUrl: string | null) => {
      const wsList = await api.listWorkspaces();
      qc.setQueryData(workspaceKeys.list(), wsList);
      const onboarded = loggedInUser.onboarded_at != null;

      if (nextUrl) {
        router.push(nextUrl);
        return;
      }

      if (!onboarded) {
        try {
          const invites = await api.listMyInvitations();
          if (invites.length > 0) {
            qc.setQueryData(workspaceKeys.myInvitations(), invites);
            router.push(paths.invitations());
            return;
          }
        } catch {
          // A failed invitation lookup must not trap the user on the callback page.
        }
      }

      router.push(resolvePostAuthDestination(wsList, onboarded));
    };

    if (state.startsWith("oidc.")) {
      loginWithOIDC(code, state)
        .then(({ user, token, appState }) => {
          const destination = parseAppState(appState);
          if (destination.cliCallback) {
            redirectToCliCallback(
              destination.cliCallback,
              token,
              destination.cliState,
            );
            return;
          }
          if (destination.isDesktop) {
            setDesktopToken(token);
            window.location.href = `multica://auth/callback?token=${encodeURIComponent(token)}`;
            return;
          }
          return completeWebLogin(user, destination.nextUrl);
        })
        .catch((err) => {
          setError(err instanceof Error ? err.message : "Login failed");
        });
      return;
    }

    const destination = parseAppState(state);

    if (destination.cliCallback) {
      // CLI login flow: exchange the Google code for a JWT, then redirect the
      // token back to the CLI's local HTTP listener (e.g. WSL2 host).
      api
        .googleLogin(code, redirectUri)
        .then(({ token }) => {
          redirectToCliCallback(
            destination.cliCallback!,
            token,
            destination.cliState,
          );
        })
        .catch((err) => {
          setError(err instanceof Error ? err.message : "Login failed");
        });
    } else if (destination.isDesktop) {
      // Desktop flow: exchange code for token, then redirect via deep link
      api
        .googleLogin(code, redirectUri)
        .then(({ token }) => {
          setDesktopToken(token);
          window.location.href = `multica://auth/callback?token=${encodeURIComponent(token)}`;
        })
        .catch((err) => {
          setError(err instanceof Error ? err.message : "Login failed");
        });
    } else {
      // Normal web flow
      loginWithGoogle(code, redirectUri)
        .then((loggedInUser) =>
          completeWebLogin(loggedInUser, destination.nextUrl),
        )
        .catch((err) => {
          setError(err instanceof Error ? err.message : "Login failed");
        });
    }
  }, [searchParams, loginWithGoogle, loginWithOIDC, router, qc]);

  if (desktopToken) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            <CardTitle className="text-2xl">Opening Multica</CardTitle>
            <CardDescription>
              You should see a prompt to open the Multica desktop app. If
              nothing happens, click the button below.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex justify-center">
            <Button
              variant="outline"
              onClick={() => {
                window.location.href = `multica://auth/callback?token=${encodeURIComponent(desktopToken)}`;
              }}
            >
              Open Multica Desktop
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            <CardTitle className="text-2xl">Login Failed</CardTitle>
            <CardDescription>{error}</CardDescription>
          </CardHeader>
          <CardContent className="flex justify-center">
            <a href={paths.login()} className="text-primary underline-offset-4 hover:underline">
              Back to login
            </a>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-2xl">Signing in...</CardTitle>
          <CardDescription>Please wait while we complete your login</CardDescription>
        </CardHeader>
        <CardContent className="flex justify-center">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        </CardContent>
      </Card>
    </div>
  );
}

export default function CallbackPage() {
  return (
    <Suspense fallback={null}>
      <CallbackContent />
    </Suspense>
  );
}
