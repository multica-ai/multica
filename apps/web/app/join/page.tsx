"use client";

import { useEffect, useState, Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { api } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useAuthStore } from "@multica/core/auth";

function JoinInner() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const code = searchParams.get("code");
  const user = useAuthStore((s) => s.user);
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
      router.push(`/login?redirect=${encodeURIComponent(`/join?code=${code}`)}`);
      return;
    }

    api.joinByShareLink(code)
      .then((result) => {
        setStatus("success");
        setMessage("You've joined the workspace!");
        setTimeout(() => {
          router.push(`/${result.workspace_id}`);
        }, 1500);
      })
      .catch((e) => {
        setStatus("error");
        setMessage(e instanceof Error ? e.message : "Failed to join workspace. The link may have expired.");
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
                <Button onClick={() => router.push(`/login?redirect=${encodeURIComponent(`/join?code=${code}`)}`)}>
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
