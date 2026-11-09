"use client";

import { useEffect, useRef, useState } from "react";
import { MulticaWordmark } from "@multica/ui/components/common/multica-wordmark";
import { cn } from "@multica/ui/lib/utils";

/*
 * The footer's closing wordmark, at the full width of the page.
 *
 * It writes itself in once, the first time the footer comes into view: the
 * letters are drawn in reading order and the star spins onto the i. Until then
 * it holds its space without showing. Under reduced motion it is the finished,
 * still frame.
 */
export function FooterWordmark({ className }: { className?: string }) {
  const box = useRef<HTMLDivElement>(null);
  const [shown, setShown] = useState(false);

  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (!entry?.isIntersecting) return;
        setShown(true);
        observer.disconnect();
      },
      { threshold: 0.5 },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  return (
    <div ref={box} className={className}>
      <MulticaWordmark
        key={shown ? "write" : "wait"}
        writeDelayMs={shown ? 0 : undefined}
        className={cn("block h-auto w-full text-site-ink", !shown && "invisible")}
      />
    </div>
  );
}
