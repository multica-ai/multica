"use client";

import { use } from "react";
import { WorkflowOverviewPage } from "@multica/views/workflows/components/overview";

export default function Page({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <WorkflowOverviewPage workflowId={id} />;
}
