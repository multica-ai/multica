"use client";

import { Suspense } from "react";
import { OAuthCallbackPage } from "@multica/views/auth";

export default function CallbackPage() {
  return (
    <Suspense fallback={null}>
      <OAuthCallbackPage />
    </Suspense>
  );
}
