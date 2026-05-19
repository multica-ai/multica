"use client";

import { useSearchParams } from "next/navigation";
import { AutopilotCreatePage } from "@multica/cerebro-autopilot-pages";

export default function Page() {
  const searchParams = useSearchParams();
  const templateId = searchParams.get("template") ?? undefined;
  return <AutopilotCreatePage templateId={templateId} />;
}
