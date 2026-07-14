"use client";

import { use } from "react";
import { AppDetailPage } from "@multica/cerebro-apps";

export default function AppDetailRoute({ params }: { params: Promise<{ appId: string }> }) {
  const { appId } = use(params);
  return <AppDetailPage appId={appId} runtimeBaseUrl="/api/cerebro/apps-runtime" />;
}
