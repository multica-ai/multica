"use client";

import { use } from "react";
// CEREBRO-PATCH(skills-direct-exports): import the detail route directly so /skills can stay lightweight.
import { SkillDetailPage } from "@multica/views/skills/components/skill-detail-page";

export default function SkillDetailRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <SkillDetailPage skillId={id} />;
}
