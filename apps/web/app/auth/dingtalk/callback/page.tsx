"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { sanitizeNextUrl, useAuthStore } from "@multica/core/auth";
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
import { Loader2 } from "lucide-react";

// DingTalk OAuth callback. DingTalk redirects back with ?authCode=...&state=...
// (note the capital C — different from Google's ?code=). Exchange the authCode
// for a Multica session; the backend resolves the enterprise email server-side.

// An authCode is single-use: DingTalk rejects a second exchange with
// "invalidParameter.authCode.notFound". The effect below can re-run after the
// first exchange already succeeded (useSearchParams identity change, Suspense
// remount), so remember codes already handed to the API for the lifetime of
// this page module. A re-fired effect must render the (eventual) result of the
// first attempt, never replay a consumed code.
const exchangedAuthCodes = new Set<string>();

function DingTalkCallbackContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const qc = useQueryClient();
  const loginWithDingTalk = useAuthStore((s) => s.loginWithDingTalk);
  const [error, setError] = useState("");

  useEffect(() => {
    const authCode = searchParams.get("authCode");
    if (!authCode) {
      setError("Missing authorization code");
      return;
    }
    if (exchangedAuthCodes.has(authCode)) {
      // Duplicate run of the effect: the first exchange is still in flight
      // (or already navigated). Touching the code again would consume the
      // login that is already succeeding.
      return;
    }
    exchangedAuthCodes.add(authCode);

    // State carries the original `next` URL across the round-trip; treat it as
    // attacker-controlled and sanitize before redirecting (same as Google).
    const state = searchParams.get("state") || "";
    const nextPart = state.split(",").find((p) => p.startsWith("next:"));
    const nextUrl = sanitizeNextUrl(nextPart ? nextPart.slice(5) : null);

    loginWithDingTalk(authCode)
      .then(async (loggedInUser) => {
        const wsList = await api.listWorkspaces();
        qc.setQueryData(workspaceKeys.list(), wsList);
        const onboarded = loggedInUser.onboarded_at != null;
        if (nextUrl) {
          router.push(nextUrl);
          return;
        }
        router.push(resolvePostAuthDestination(wsList, onboarded));
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Login failed");
      });
  }, [searchParams, loginWithDingTalk, router, qc]);

  if (error) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            <CardTitle className="text-display-sm">Login Failed</CardTitle>
            <CardDescription>{error}</CardDescription>
          </CardHeader>
        </Card>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-display-sm">Signing in...</CardTitle>
          <CardDescription>Please wait while we complete your login</CardDescription>
        </CardHeader>
        <CardContent className="flex justify-center">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        </CardContent>
      </Card>
    </div>
  );
}

export default function DingTalkCallbackPage() {
  return (
    <Suspense fallback={null}>
      <DingTalkCallbackContent />
    </Suspense>
  );
}
