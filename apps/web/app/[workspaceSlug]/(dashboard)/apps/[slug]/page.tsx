"use client";

import { use } from "react";
import { AppDetailPage } from "@multica/views/apps";

export default function AppDetailRoute({
  params,
}: {
  params: Promise<{ slug: string }>;
}) {
  const { slug } = use(params);
  return <AppDetailPage toolkitSlug={slug} />;
}
