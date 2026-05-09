"use client";

// Phase 7a — Web entry for the Release detail page. The shared
// ShipReleasePage component lives in @multica/views; this file just
// extracts the [id] param from Next's router and forwards it.

import { use } from "react";
import { ShipReleasePage } from "@multica/views/ship";

export default function Page({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <ShipReleasePage releaseId={id} />;
}
