"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { paths } from "@multica/core/paths";
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

function CallbackContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const qc = useQueryClient();
  const loginWithGoogle = useAuthStore((s) => s.loginWithGoogle);
  const loginWithFeishu = useAuthStore((s) => s.loginWithFeishu);
  const [error, setError] = useState("");
  const [desktopToken, setDesktopToken] = useState<string | null>(null);

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

    const state = searchParams.get("state") || "";
    const stateParts = state.split(",").filter(Boolean);
    const providerPart = stateParts.find((p) => p.startsWith("provider:"));
    const provider = providerPart?.slice(9) || "google";
    const isDesktop = stateParts.includes("platform:desktop");
    const nextPart = stateParts.find((p) => p.startsWith("next:"));
    const nextUrl = nextPart ? nextPart.slice(5) : null;

    const redirectUri = `${window.location.origin}/auth/callback`;
    const completeWebLogin =
      provider === "feishu" ? loginWithFeishu : loginWithGoogle;
    const exchangeDesktopToken =
      provider === "feishu"
        ? api.feishuLogin.bind(api)
        : api.googleLogin.bind(api);

    if (isDesktop) {
      exchangeDesktopToken(code, redirectUri)
        .then(({ token }) => {
          setDesktopToken(token);
          window.location.href = `multica://auth/callback?token=${encodeURIComponent(token)}`;
        })
        .catch((err) => {
          setError(err instanceof Error ? err.message : "Login failed");
        });
      return;
    }

    completeWebLogin(code, redirectUri)
      .then(async () => {
        const wsList = await api.listWorkspaces();
        qc.setQueryData(workspaceKeys.list(), wsList);
        // URL is now the source of truth for the current workspace — the
        // [workspaceSlug]/layout syncs stores + cookie once we navigate.
        // Honor ?next= first (e.g. came from /invite/{id}), otherwise land
        // in the first workspace's issues, or /workspaces/new for zero-workspace users.
        const [first] = wsList;
        const defaultDest = first
          ? paths.workspace(first.slug).issues()
          : paths.newWorkspace();
        router.push(nextUrl || defaultDest);
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Login failed");
      });
  }, [searchParams, loginWithFeishu, loginWithGoogle, router, qc]);

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
            <a
              href={paths.login()}
              className="text-primary underline-offset-4 hover:underline"
            >
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
