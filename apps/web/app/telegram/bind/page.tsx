"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { TelegramBindPage } from "@multica/views/telegram";

// /telegram/bind?token=<raw> is the bot's "link your account" destination.
// Suspense wraps useSearchParams per Next.js 15's CSR-bailout rule.
function TelegramBindPageContent() {
  const searchParams = useSearchParams();
  const token = searchParams.get("token");
  return <TelegramBindPage token={token} />;
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <TelegramBindPageContent />
    </Suspense>
  );
}
