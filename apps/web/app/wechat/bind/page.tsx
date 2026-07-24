"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { WechatBindPage } from "@multica/views/wechat";

// /wechat/bind?token=<raw> is the bot's "link your account" destination. Suspense
// wraps useSearchParams per Next.js 15's CSR-bailout rule; the loading text
// never paints in practice because the redemption page itself renders the
// "redeeming…" state immediately.
function WechatBindPageContent() {
  const searchParams = useSearchParams();
  const token = searchParams.get("token");
  return <WechatBindPage token={token} />;
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <WechatBindPageContent />
    </Suspense>
  );
}
