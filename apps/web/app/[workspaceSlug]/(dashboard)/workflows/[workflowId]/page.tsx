"use client";

import { use } from "react";
import { WorkflowEditorPage } from "@multica/cerebro-workflows";

export default function WorkflowEditRoute({
  params,
}: {
  params: Promise<{ workflowId: string }>;
}) {
  const { workflowId } = use(params);
  return <WorkflowEditorPage workflowId={workflowId} />;
}
