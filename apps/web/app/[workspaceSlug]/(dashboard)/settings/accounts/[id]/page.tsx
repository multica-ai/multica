"use client";

import { use } from "react";
import { AccountDetailPage } from "@multica/cerebro-runtime/views";

export default function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  return <AccountDetailPage accountId={id} />;
}
