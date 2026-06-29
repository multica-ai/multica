"use client";

import { use } from "react";
import { DocumentDetailPage } from "@multica/views/documents";

export default function DocumentDetailRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <DocumentDetailPage documentId={id} />;
}
