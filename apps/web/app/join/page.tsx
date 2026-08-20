"use client";

import { Suspense, useEffect } from "react";
import { useSearchParams } from "next/navigation";
import { documentNavigation } from "@/platform/web-host-path";

function JoinHandoff() {
  const searchParams = useSearchParams();

  useEffect(() => {
    const token = searchParams.get("token") ?? searchParams.get("code") ?? "";
    const destination = token
      ? `/tag/join?token=${encodeURIComponent(token)}`
      : "/tag/join";
    documentNavigation.replace(destination);
  }, [searchParams]);

  return (
    <div className="flex min-h-screen items-center justify-center">
      Opening invitation…
    </div>
  );
}

/** Legacy document entry; the Tag host and VIBES authority own the join flow. */
export default function JoinPage() {
  return (
    <Suspense>
      <JoinHandoff />
    </Suspense>
  );
}
