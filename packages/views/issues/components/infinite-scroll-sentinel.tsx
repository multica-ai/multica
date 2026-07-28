"use client";

import { useEffect, useRef } from "react";
import { Loader2 } from "lucide-react";

/** Sentinel that triggers once per visibility transition. Callback/loading
 * changes are read through refs/props without rebuilding the observer, so a
 * completed page fetch cannot retrigger while the sentinel remains visible. */
export function InfiniteScrollSentinel({
  onVisible,
  loading,
  rootMargin = "100px",
  className = "flex items-center justify-center py-2",
}: {
  onVisible: () => void;
  loading: boolean;
  rootMargin?: string;
  className?: string;
}) {
  const sentinelRef = useRef<HTMLDivElement>(null);
  const onVisibleRef = useRef(onVisible);
  onVisibleRef.current = onVisible;

  useEffect(() => {
    const node = sentinelRef.current;
    if (!node) return;
    const observer = new IntersectionObserver(
      ([entry]) => { if (entry?.isIntersecting) onVisibleRef.current(); },
      { rootMargin },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [rootMargin]);

  return (
    <div ref={sentinelRef} className={className} aria-hidden>
      {loading && <Loader2 className="size-3 animate-spin text-muted-foreground" />}
    </div>
  );
}
