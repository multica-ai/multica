"use client";

import { useEffect, useState, type ReactNode } from "react";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { useT } from "../i18n";

/** Replace the title icon without changing its footprint or flashing on fast requests. */
export function RefreshablePageIcon({
  refreshing,
  children,
}: {
  refreshing: boolean;
  children: ReactNode;
}) {
  return (
    <span className="flex size-4 shrink-0 items-center justify-center text-muted-foreground">
      {refreshing ? <PendingIcon>{children}</PendingIcon> : children}
    </span>
  );
}

// Mount a fresh delay for each refresh; completion also cancels a pending timer.
function PendingIcon({ children }: { children: ReactNode }) {
  const { t } = useT("common");
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    const timer = setTimeout(() => setVisible(true), 300);
    return () => clearTimeout(timer);
  }, []);

  return visible ? (
    <Spinner aria-label={t(($) => $.loading)} className="size-4 motion-reduce:animate-none" />
  ) : children;
}
