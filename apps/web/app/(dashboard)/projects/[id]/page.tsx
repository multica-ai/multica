"use client";

import { use } from "react";
import { ProjectDetailPage } from "@/features/projects/components/project-detail-page";

export default function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  return <ProjectDetailPage projectId={id} />;
}
