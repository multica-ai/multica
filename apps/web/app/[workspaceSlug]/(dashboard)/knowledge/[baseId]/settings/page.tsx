"use client";

import { use } from "react";
import { KnowledgeSettingsPage } from "@multica/views/knowledge";

export default function Page({ params }: { params: Promise<{ baseId: string }> }) {
  const { baseId } = use(params);
  return <KnowledgeSettingsPage baseId={baseId} />;
}
