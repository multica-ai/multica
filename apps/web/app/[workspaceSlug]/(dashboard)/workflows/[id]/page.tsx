"use client";

import { use } from "react";
import { WorkflowEditorPage } from "@multica/views/workflows";

export default function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  return <WorkflowEditorPage workflowId={id} />;
}
