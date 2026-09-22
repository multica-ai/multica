"use client";

import { use } from "react";
import { KnowledgeDocumentPage } from "@multica/views/knowledge";

export default function Page({ params }: { params: Promise<{ baseId: string; documentId: string }> }) {
  const { baseId, documentId } = use(params);
  return <KnowledgeDocumentPage baseId={baseId} documentId={documentId} />;
}
