"use client";

import { use } from "react";
import { SprintBoard } from "@multica/views/projects/components";

export default function SprintDetailPage({
  params,
}: {
  params: Promise<{ sprintId: string }>;
}) {
  const { sprintId } = use(params);
  return <SprintBoard sprintId={sprintId} />;
}
