"use client";

import { use } from "react";
import { SprintsPage } from "@multica/views/sprints";

export default function ProjectSprintsPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <SprintsPage projectId={id} />;
}
