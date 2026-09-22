"use client";

import { use } from "react";
import { KnowledgeBasePage } from "@multica/views/knowledge";

export default function Page({ params }: { params: Promise<{ baseId: string }> }) {
  const { baseId } = use(params);
  return <KnowledgeBasePage baseId={baseId} />;
}
