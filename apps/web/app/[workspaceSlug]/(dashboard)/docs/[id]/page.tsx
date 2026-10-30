"use client";

import { use } from "react";
import { DocDetailPage } from "@multica/views/docs";

export default function DocDetailRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <DocDetailPage docId={id} />;
}
