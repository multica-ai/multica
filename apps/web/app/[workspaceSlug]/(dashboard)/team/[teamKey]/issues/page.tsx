"use client";

import { use } from "react";
import { TeamIssuesPage } from "@multica/views/teams";

export default function Page({
  params,
}: {
  params: Promise<{ teamKey: string }>;
}) {
  const { teamKey } = use(params);
  return <TeamIssuesPage teamKey={teamKey} />;
}
