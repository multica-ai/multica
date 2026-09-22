"use client";

import { use } from "react";
import { KnowledgeAskPage } from "@multica/views/knowledge";

export default function Page({ params }: { params: Promise<{ baseId: string }> }) {
  const { baseId } = use(params);
  return <KnowledgeAskPage baseId={baseId} />;
}
