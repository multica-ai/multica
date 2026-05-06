"use client";

import { use } from "react";
import { MemoryDetailPage } from "@multica/views/memory/components";

export default function MemoryArtifactDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <MemoryDetailPage id={id} />;
}
