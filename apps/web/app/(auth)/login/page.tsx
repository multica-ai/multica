"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { sanitizeNextUrl, useAuthStore } from "@wallts/core/auth";
import { workspaceKeys } from "@wallts/core/workspace/queries";
import {
  paths,
  resolvePostAuthDestination,
  useHasOnboarded,
} from "@wallts/core/paths";
import { api } from "@wallts/core/api";
import type { Workspace } from "@wallts/core/types";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@wallts/ui/components/ui/card";
import { Button } from "@wallts/ui/components/ui/button";
import { Loader2 } from "lucide-react";
import { setLoggedInCookie } from "@/features/auth/auth-cookie";
import Link from "next/link";
import { LoginPage, validateCliCallback } from "@wallts/views/auth";
import { useT } from "@wallts/views/i18n";

/**
 * Pick where a logged-in user with no explicit ?next= should land.
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
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const searchParams = useSearchParams();

  const cliCallbackRaw = searchParams.get("cli_callback");
  const cliState = searchParams.get("cli_state") || "";
  const platform = searchParams.get("platform");
  const isDesktopHandoff = platform === "desktop" && !cliCallbackRaw;
  const nextUrl = sanitizeNextUrl(searchParams.get("next"));

  const [desktopToken, setDesktopToken] = useState<string | null>(null);
  const [desktopError, setDesktopError] = useState("");
  const hasOnboarded = useHasOnboarded();

  useEffect(() => {
    if (isLoading || !user || cliCallbackRaw) return;
    if (isDesktopHandoff) {
      api
        .issueCliToken()
        .then(({ token }) => {
          setDesktopToken(token);
          window.location.href = `wallts://auth/callback?token=${encodeURIComponent(token)}`;
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
  }, [isLoading, user, router, nextUrl, cliCallbackRaw, isDesktopHandoff, hasOnboarded, qc]);

  const handleSuccess = async () => {
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

  if (isDesktopHandoff && user) {
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
                  window.location.href = `wallts://auth/callback?token=${encodeURIComponent(desktopToken)}`;
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
      cliCallback={
        cliCallbackRaw && validateCliCallback(cliCallbackRaw)
          ? { url: cliCallbackRaw, state: cliState }
          : undefined
      }
      onTokenObtained={setLoggedInCookie}
      extra={
        <span className="text-xs text-muted-foreground">
          {t(($) => $.web.prefer_desktop)}{" "}
          <Link
            href="/download"
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
