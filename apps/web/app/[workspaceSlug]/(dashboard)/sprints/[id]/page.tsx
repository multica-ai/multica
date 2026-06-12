"use client";

import { use } from "react";
import { SprintDetail } from "@multica/views/sprints/components";

export default function SprintDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <SprintDetail sprintId={id} />;
}
