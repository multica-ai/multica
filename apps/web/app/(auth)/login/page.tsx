"use client";

import { Suspense, useEffect } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { paths } from "@multica/core/paths";
import type { Workspace } from "@multica/core/types";
import { setLoggedInCookie } from "@/features/auth/auth-cookie";
import { LoginPage, validateCliCallback } from "@multica/views/auth";

const googleClientId = process.env.NEXT_PUBLIC_GOOGLE_CLIENT_ID;
const feishuClientId = process.env.NEXT_PUBLIC_FEISHU_APP_ID;

function LoginPageContent() {
  const router = useRouter();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const searchParams = useSearchParams();

  const cliCallbackRaw = searchParams.get("cli_callback");
  const cliState = searchParams.get("cli_state") || "";
  const platform = searchParams.get("platform");
  const provider = searchParams.get("provider");
  // `next` carries a protected URL the user was originally headed to
  // (e.g. /invite/{id}). With URL-driven workspaces there is no legacy
  // "/issues" default — if `next` is absent we decide after login based on
  // the user's workspace list.
  const nextUrl = searchParams.get("next");

  // Already authenticated — honor ?next= or fall back to first workspace
  // (or /workspaces/new if the user has none). Skip this entire path when
  // the user arrived to authorize the CLI.
  useEffect(() => {
    if (isLoading || !user || cliCallbackRaw) return;
    if (nextUrl) {
      router.replace(nextUrl);
      return;
    }
    const list = qc.getQueryData<Workspace[]>(workspaceKeys.list()) ?? [];
    const [first] = list;
    router.replace(
      first ? paths.workspace(first.slug).issues() : paths.newWorkspace(),
    );
  }, [isLoading, user, router, nextUrl, cliCallbackRaw, qc]);

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

  const buildProviderState = (providerName: "google" | "feishu") =>
    [
      `provider:${providerName}`,
      platform === "desktop" ? "platform:desktop" : "",
      nextUrl ? `next:${nextUrl}` : "",
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
              state: buildProviderState("google"),
            }
          : undefined
      }
      feishu={
        feishuClientId
          ? {
              clientId: feishuClientId,
              redirectUri: `${window.location.origin}/auth/callback`,
              state: buildProviderState("feishu"),
            }
          : undefined
      }
      cliCallback={
        cliCallbackRaw && validateCliCallback(cliCallbackRaw)
          ? { url: cliCallbackRaw, state: cliState }
          : undefined
      }
      onTokenObtained={setLoggedInCookie}
      autoStartProvider={
        provider === "google" || provider === "feishu"
          ? provider
          : undefined
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
