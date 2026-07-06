"use client";

import { use } from "react";
import { TeamDetailPage } from "@multica/views/teams";

export default function Page({
  params,
}: {
  params: Promise<{ teamKey: string }>;
}) {
  const { teamKey } = use(params);
  return <TeamDetailPage teamKey={teamKey} />;
}
