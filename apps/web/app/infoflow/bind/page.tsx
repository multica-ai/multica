"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { InfoFlowBindPage } from "@multica/views/infoflow";

// /infoflow/bind?token=<raw> is the bot's "link your account" destination —
// the path server/internal/integrations/infoflow/replier.go builds into the
// binding prompt (OutboundReplierConfig.BindingPath defaults to it) and the
// reserved-slug list keeps global. Suspense wraps useSearchParams per Next.js
// 15's CSR-bailout rule; the fallback never paints because the redemption page
// renders the "redeeming…" state immediately.
function InfoFlowBindPageContent() {
  const searchParams = useSearchParams();
  const token = searchParams.get("token");
  return <InfoFlowBindPage token={token} />;
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <InfoFlowBindPageContent />
    </Suspense>
  );
}