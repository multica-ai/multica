"use client";

import { use } from "react";
import { WorkflowForm } from "@multica/cerebro-workflows";

export default function WorkflowEditRoute({
  params,
}: {
  params: Promise<{ workflowId: string }>;
}) {
  const { workflowId } = use(params);
  return <WorkflowForm workflowId={workflowId} />;
}
