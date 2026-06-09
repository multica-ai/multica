"use client";

import { use } from "react";
import { ProjectDetail } from "@multica/views/projects/components";

export default function ProjectDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <ProjectDetail projectId={id} />;
}
