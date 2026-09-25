"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { DingTalkBindPage } from "@multica/views/dingtalk";

// /dingtalk/bind?token=<raw> is the bot's "link your account" destination.
// Suspense wraps useSearchParams per Next.js 15's CSR-bailout rule; the loading
// text never paints in practice because the redemption page itself renders the
// "redeeming…" state immediately.
function DingTalkBindPageContent() {
  const searchParams = useSearchParams();
  const token = searchParams.get("token");
  return <DingTalkBindPage token={token} />;
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <DingTalkBindPageContent />
    </Suspense>
  );
}
