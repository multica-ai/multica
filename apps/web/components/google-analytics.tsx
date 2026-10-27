"use client";

import { useEffect } from "react";
import { installGoogleAnalytics } from "@/lib/google-analytics";

export function GoogleAnalytics({ measurementId }: { measurementId: string }) {
  useEffect(() => {
    installGoogleAnalytics(measurementId);
  }, [measurementId]);

  return null;
}
