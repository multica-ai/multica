"use client";

import { useEffect, useState, Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useAuthStore } from "@multica/core/auth";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";

function JoinInner() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const code = searchParams.get("code");
  const user = useAuthStore((s) => s.user);
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<"loading" | "error" | "success">("loading");
  const [message, setMessage] = useState("Joining workspace...");

  useEffect(() => {
    if (!code) {
      setStatus("error");
      setMessage("No invite code found. Please use a valid share link.");
      return;
    }

    if (!user) {
      // Redirect to login with return URL
      router.push(`/login?next=${encodeURIComponent(`/join?code=${code}`)}`);
      return;
    }

    api.joinByShareLink(code)
      .then(async (result) => {
        setStatus("success");
        setMessage("You've joined the workspace!");
        await useAuthStore.getState().refreshMe();
        const list = await api.listWorkspaces().catch(() => [] as Workspace[]);
        queryClient.setQueryData(workspaceKeys.list(), list);
        setTimeout(() => {
          router.push(`/${result.workspace_slug || result.workspace_id}/issues`);
        }, 1500);
      })
      .catch(async (e) => {
        const msg = e instanceof Error ? e.message : "";
        if (msg.includes("already a member")) {
          setMessage("Already a member — redirecting...");
          await useAuthStore.getState().refreshMe();
          try {
            const workspaces = await api.listWorkspaces();
            queryClient.setQueryData(workspaceKeys.list(), workspaces as any);
            const first = workspaces[0];
            if (first) {
              router.push(`/${first.slug}/issues`);
              return;
            }
          } catch {}
          router.push("/");
          return;
        }
        setStatus("error");
        setMessage(msg || "Failed to join workspace. The link may have expired.");
      });
  }, [code, user, router]);

  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/30 p-4">
      <Card className="w-full max-w-md">
        <CardContent className="space-y-4 pt-6">
          <h1 className="text-xl font-semibold text-center">
            {status === "loading" ? "Joining..." : status === "success" ? "Joined!" : "Oops"}
          </h1>
          <p className="text-center text-muted-foreground">{message}</p>
          {status === "success" && (
            <p className="text-center text-sm text-muted-foreground">Redirecting...</p>
          )}
          {status === "error" && (
            <div className="flex justify-center gap-2">
              <Button variant="outline" onClick={() => router.push("/")}>
                Go Home
              </Button>
              {!user && (
                <Button onClick={() => router.push(`/login?next=${encodeURIComponent(`/join?code=${code}`)}`)}>
                  Log In
                </Button>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

export default function JoinPage() {
  return (
    <Suspense fallback={<div className="flex min-h-screen items-center justify-center">Loading...</div>}>
      <JoinInner />
    </Suspense>
  );
}
