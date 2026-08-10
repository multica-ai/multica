"use client";

import { use } from "react";
import { AutopilotDetailRoute } from "@multica/views/autopilots/components";

export default function Page({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <AutopilotDetailRoute autopilotId={id} />;
}
