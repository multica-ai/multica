"use client";

import { use } from "react";
import { AutopilotEditPage } from "@multica/cerebro-autopilot-pages";

export default function Page({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <AutopilotEditPage autopilotId={id} />;
}
