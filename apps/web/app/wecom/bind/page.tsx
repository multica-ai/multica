"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { WecomBindPage } from "@multica/views/wecom";

// /wecom/bind?token=<raw> is the smart-bot's "link your Multica account"
// destination. Suspense wraps useSearchParams per Next.js 15's CSR-bailout
// rule; the loading text never paints in practice because the redemption
// page itself renders the "redeeming…" state immediately.
function WecomBindPageContent() {
  const searchParams = useSearchParams();
  const token = searchParams.get("token");
  return <WecomBindPage token={token} />;
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <WecomBindPageContent />
    </Suspense>
  );
}
