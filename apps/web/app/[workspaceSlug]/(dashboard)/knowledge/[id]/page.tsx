"use client";

import { use } from "react";
import { KnowledgePage } from "@multica/views/knowledge";

export default function Page({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <KnowledgePage knowledgeId={id} />;
}
