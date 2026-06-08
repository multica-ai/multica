"use client";

import { use } from "react";
import { ConnectionEditPage } from "@multica/cerebro-connections/views";

export default function Page({ params }: { params: Promise<{ connId: string }> }) {
  const { connId } = use(params);
  return <ConnectionEditPage connId={connId} />;
}
