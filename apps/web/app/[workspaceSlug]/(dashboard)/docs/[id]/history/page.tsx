"use client";

import { use } from "react";
import { DocHistoryPage } from "@multica/views/docs";

export default function DocHistoryRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <DocHistoryPage docId={id} />;
}
