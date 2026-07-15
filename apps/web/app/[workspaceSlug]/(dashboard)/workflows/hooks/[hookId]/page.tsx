"use client";

import { use } from "react";
import { WorkflowHookEditorPage } from "@multica/cerebro-workflows";

export default function HookEditRoute({ params }: { params: Promise<{ hookId: string }> }) {
  const { hookId } = use(params);
  return <WorkflowHookEditorPage hookId={hookId} />;
}
