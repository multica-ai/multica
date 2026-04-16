"use client";

import { use } from "react";
import { WorkflowDetailPage } from "@multica/views/workflows/components";

export default function Page({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <WorkflowDetailPage workflowId={id} />;
}
