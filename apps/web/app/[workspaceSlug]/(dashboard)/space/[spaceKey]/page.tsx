"use client";

import { use } from "react";
import { SpaceOverviewPage } from "@multica/views/spaces";

export default function Page({
  params,
}: {
  params: Promise<{ spaceKey: string }>;
}) {
  const { spaceKey } = use(params);
  return <SpaceOverviewPage spaceKey={spaceKey} />;
}
