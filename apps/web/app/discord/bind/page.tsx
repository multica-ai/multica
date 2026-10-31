"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { DiscordBindPage } from "@multica/views/discord";

// /discord/bind?token=<raw> is the bot's "link your account" destination.
// Suspense wraps useSearchParams per Next.js 15's CSR-bailout rule.
function DiscordBindPageContent() {
  const searchParams = useSearchParams();
  const token = searchParams.get("token");
  return <DiscordBindPage token={token} />;
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <DiscordBindPageContent />
    </Suspense>
  );
}
