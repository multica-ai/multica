"use client";

import { use } from "react";
import { WorkflowRunPage } from "@multica/views/workflows/components";

export default function Page({
  params,
}: {
  params: Promise<{ id: string; runId: string }>;
}) {
  const { id, runId } = use(params);
  return <WorkflowRunPage workflowId={id} runId={runId} />;
}
