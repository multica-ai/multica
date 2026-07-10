"use client";

import { use } from "react";
import { SpaceSettingsPage } from "@multica/views/spaces";

export default function Page({
  params,
}: {
  params: Promise<{ spaceKey: string }>;
}) {
  const { spaceKey } = use(params);
  return <SpaceSettingsPage spaceKey={spaceKey} />;
}
