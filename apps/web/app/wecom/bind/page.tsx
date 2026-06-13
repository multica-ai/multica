"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { WecomBindPage } from "@multica/views/wecom";

function WecomBindPageContent() {
  const searchParams = useSearchParams();
  const token = searchParams.get("token");
  return <WecomBindPage token={token} />;
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <WecomBindPageContent />
    </Suspense>
  );
}
