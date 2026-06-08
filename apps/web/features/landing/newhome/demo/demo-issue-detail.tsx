"use client";

import { useEffect, useRef } from "react";
import { IssueDetail } from "@multica/views/issues/components";

export function DemoIssueDetail({
  issueId,
  initialScrollTop = 0,
}: {
  issueId: string;
  initialScrollTop?: number;
}) {
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (initialScrollTop <= 0) return;

    let cancelled = false;
    const applyScroll = () => {
      if (cancelled) return;
      const root = rootRef.current;
      if (!root) return;

      const scrollables = Array.from(root.querySelectorAll<HTMLElement>("div"))
        .filter((el) => el.scrollHeight - el.clientHeight > 80)
        .sort(
          (a, b) =>
            b.scrollHeight - b.clientHeight - (a.scrollHeight - a.clientHeight),
        );
      const target =
        scrollables.find((el) => {
          const className =
            typeof el.className === "string" ? el.className : "";
          return (
            className.includes("relative") &&
            className.includes("flex-1") &&
            className.includes("overflow-y-auto")
          );
        }) ?? scrollables[0];

      if (!target) return;
      const maxScroll = Math.max(0, target.scrollHeight - target.clientHeight);
      target.scrollTop = Math.min(initialScrollTop, maxScroll);
    };

    const frame = window.requestAnimationFrame(applyScroll);
    const timers = [
      window.setTimeout(applyScroll, 250),
      window.setTimeout(applyScroll, 800),
    ];

    return () => {
      cancelled = true;
      window.cancelAnimationFrame(frame);
      timers.forEach((timer) => window.clearTimeout(timer));
    };
  }, [initialScrollTop, issueId]);

  return (
    <div ref={rootRef} className="h-full overflow-auto [scrollbar-width:thin]">
      <IssueDetail issueId={issueId} onDone={() => {}} onDelete={() => {}} />
    </div>
  );
}
