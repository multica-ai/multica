"use client";

import { use } from "react";
import { DocDetailPage } from "@multica/views/docs";

export default function DocDetailRoute({
  params,
}: {
  params: Promise<{ docId: string }>;
}) {
  const { docId } = use(params);
  return <DocDetailPage docId={docId} />;
}
