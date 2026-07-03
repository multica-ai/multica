"use client";

import { use } from "react";
import { TeamProjectsPage } from "@multica/views/teams";

export default function Page({
  params,
}: {
  params: Promise<{ teamKey: string }>;
}) {
  const { teamKey } = use(params);
  return <TeamProjectsPage teamKey={teamKey} />;
}
