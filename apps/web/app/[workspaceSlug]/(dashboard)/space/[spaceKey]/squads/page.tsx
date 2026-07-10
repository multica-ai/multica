"use client";

import { use } from "react";
import { SpaceSquadsPage } from "@multica/views/spaces";

export default function Page({
  params,
}: {
  params: Promise<{ spaceKey: string }>;
}) {
  const { spaceKey } = use(params);
  return <SpaceSquadsPage spaceKey={spaceKey} />;
}
