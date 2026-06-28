"use client";

import { use } from "react";
import { AutopilotDetailPage } from "@multica/views/autopilots/components/autopilot-detail-page";

export default function Page({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <AutopilotDetailPage autopilotId={id} />;
}
