"use client";

import { use } from "react";
import { OneShotRunPage } from "@multica/views/oneshot";

export default function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  return <OneShotRunPage taskId={id} />;
}
