"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { sanitizeNextUrl, useAuthStore } from "@multica/core/auth";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { paths, resolvePostAuthDestination } from "@multica/core/paths";
import { api } from "@multica/core/api";
import {
  parseCliOAuthStatePart,
  redirectToCliCallback,
} from "@multica/views/auth";
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

function CallbackContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const qc = useQueryClient();
  const loginWithGoogle = useAuthStore((s) => s.loginWithGoogle);
  const loginWithDingTalk = useAuthStore((s) => s.loginWithDingTalk);
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
    if (state.startsWith("dingtalk.")) {
      api
        .completeDingTalkBinding(code, state)
        .then(({ next_path }) => {
          router.push(next_path || paths.root());
        })
        .catch((err) => {
          setError(
            err instanceof Error ? err.message : "DingTalk connection failed",
          );
        });
      return;
    }

    const stateParts = state.split(",").filter(Boolean);
    const cliCallback =
      stateParts
        .map((part) => parseCliOAuthStatePart(part))
        .find((part) => part !== null) ?? null;

    // DingTalk login flow
    if (stateParts.includes("provider:dingtalk")) {
      const isDesktop = stateParts.includes("platform:desktop");
      const nextPart = stateParts.find((p) => p.startsWith("next:"));
      const nextUrl = sanitizeNextUrl(nextPart ? nextPart.slice(5) : null);
      const redirectUri = `${window.location.origin}/auth/callback`;

      if (isDesktop) {
        api
          .dingtalkLogin(code, redirectUri)
          .then(({ token }) => {
            setDesktopToken(token);
            window.location.href = `multica://auth/callback?token=${encodeURIComponent(token)}`;
          })
          .catch((err) => {
            setError(err instanceof Error ? err.message : "Login failed");
          });
      } else if (cliCallback) {
        api
          .dingtalkLogin(code, redirectUri)
          .then(({ token }) => {
            setLoggedInCookie();
            redirectToCliCallback(cliCallback.url, token, cliCallback.state);
          })
          .catch((err) => {
            setError(err instanceof Error ? err.message : "Login failed");
          });
      } else {
        loginWithDingTalk(code, redirectUri)
          .then(async (loggedInUser) => {
            const wsList = await api.listWorkspaces();
            qc.setQueryData(workspaceKeys.list(), wsList);
            const onboarded = loggedInUser.onboarded_at != null;
            if (!onboarded) {
              router.push(paths.onboarding());
              return;
            }
            router.push(
              nextUrl || resolvePostAuthDestination(wsList, onboarded),
            );
          })
          .catch((err) => {
            setError(err instanceof Error ? err.message : "Login failed");
          });
      }
      return;
    }

    // Google login flow
    const isDesktop = stateParts.includes("platform:desktop");
    const nextPart = stateParts.find((p) => p.startsWith("next:"));
    const nextUrl = sanitizeNextUrl(nextPart ? nextPart.slice(5) : null);

    const redirectUri = `${window.location.origin}/auth/callback`;

    if (isDesktop) {
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
    } else if (cliCallback) {
      api
        .googleLogin(code, redirectUri)
        .then(({ token }) => {
          setLoggedInCookie();
          redirectToCliCallback(cliCallback.url, token, cliCallback.state);
        })
        .catch((err) => {
          setError(err instanceof Error ? err.message : "Login failed");
        });
    } else {
      // Normal web flow
      loginWithGoogle(code, redirectUri)
        .then(async (loggedInUser) => {
          const wsList = await api.listWorkspaces();
          qc.setQueryData(workspaceKeys.list(), wsList);
          const onboarded = loggedInUser.onboarded_at != null;
          if (!onboarded) {
            router.push(paths.onboarding());
            return;
          }
          router.push(
            nextUrl || resolvePostAuthDestination(wsList, onboarded),
          );
        })
        .catch((err) => {
          setError(err instanceof Error ? err.message : "Login failed");
        });
    }
  }, [searchParams, loginWithGoogle, loginWithDingTalk, router, qc]);

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
            <CardTitle className="text-2xl">Authentication Failed</CardTitle>
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
