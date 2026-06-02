"use client";

import { Suspense, useEffect } from "react";
import { useRouter } from "next/navigation";
import { paths } from "@wallts/core/paths";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@wallts/ui/components/ui/card";
import { Loader2 } from "lucide-react";

/**
 * OAuth callback page — deprecated.
 * Google OAuth has been removed in favor of name-based login.
 * This page now simply redirects back to the login screen.
 */
function CallbackContent() {
  const router = useRouter();

  useEffect(() => {
    router.replace(paths.login());
  }, [router]);

  return (
    <div className="flex min-h-screen items-center justify-center">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-2xl">Redirecting...</CardTitle>
          <CardDescription>Redirecting to login</CardDescription>
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
