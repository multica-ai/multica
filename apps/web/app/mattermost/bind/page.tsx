"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { MattermostBindPage } from "@multica/views/mattermost";

// /mattermost/bind?token=<raw> is the bot's "link your account" destination.
// Suspense wraps useSearchParams per Next.js 15's CSR-bailout rule.
function MattermostBindPageContent() {
  const searchParams = useSearchParams();
  const token = searchParams.get("token");
  return <MattermostBindPage token={token} />;
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <MattermostBindPageContent />
    </Suspense>
  );
}
