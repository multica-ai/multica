"use client";

import { use } from "react";
import { EpicDetail } from "@multica/views/epics/components";

export default function EpicDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <EpicDetail epicId={id} />;
}
