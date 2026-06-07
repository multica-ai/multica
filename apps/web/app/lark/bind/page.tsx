"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { LarkBindPage } from "@multica/views/lark";

// /lark/bind?token=<raw> is the Bot's "you're not bound yet, click here"
// destination. Suspense wraps useSearchParams per Next.js 15's CSR-bailout
// rule; the loading text never paints in practice because the redemption
// page itself renders the "redeeming…" state immediately.
function LarkBindPageContent() {
  const searchParams = useSearchParams();
  const token = searchParams.get("token");
  return <LarkBindPage token={token} />;
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <LarkBindPageContent />
    </Suspense>
  );
}
