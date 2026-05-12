"use client";

import { use } from "react";
import { WorkflowRunsPage } from "@multica/cerebro-workflows";

export default function PerWorkflowRunsRoute({
  params,
}: {
  params: Promise<{ workflowId: string }>;
}) {
  const { workflowId } = use(params);
  return <WorkflowRunsPage workflowId={workflowId} />;
}
