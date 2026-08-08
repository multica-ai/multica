"use client";

import { useEffect } from "react";

/** Sets the client-side title for routes whose data is loaded after hydration. */
export function PageTitle({ title }: { title: string }) {
  useEffect(() => {
    document.title = title;
  }, [title]);

  return null;
}
