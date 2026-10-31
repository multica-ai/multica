"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { WORDMARK } from "./brand-logo";

/*
 * The footer's closing wordmark, at the full width of the page.
 *
 * Motion is the brand's "writing" entrance, played once the first time the
 * footer comes into view: each letter is drawn along its centre-line in
 * reading order, 150ms apart, and the tittle of the i pops in last. Under
 * reduced motion it is the finished, still frame.
 */

const LETTER_STEP = 150;
const TITTLE_DELAY = 4 * LETTER_STEP + 480;

export function FooterWordmark({ className }: { className?: string }) {
  const svg = useRef<SVGSVGElement>(null);
  const [shown, setShown] = useState(false);

  useEffect(() => {
    const el = svg.current;
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
    <svg
      ref={svg}
      viewBox={`0 ${WORDMARK.top} ${WORDMARK.width} ${WORDMARK.height}`}
      role="img"
      aria-label="Multica"
      data-shown={shown}
      className={cn("group block h-auto w-full text-site-ink", className)}
    >
      <g fill="none" stroke="currentColor" strokeWidth={WORDMARK.stroke}>
        {WORDMARK.letters.map((d, index) => (
          <path
            key={d}
            d={d}
            pathLength={1}
            strokeDasharray="1 1"
            style={{ transitionDelay: `${index * LETTER_STEP}ms` }}
            className={cn(
              "[stroke-dashoffset:1] transition-[stroke-dashoffset] duration-500 ease-[cubic-bezier(0.65,0,0.35,1)]",
              "group-data-[shown=true]:[stroke-dashoffset:0]",
              "motion-reduce:transition-none motion-reduce:[stroke-dashoffset:0]",
            )}
          />
        ))}
      </g>
      <circle
        {...WORDMARK.tittle}
        fill="currentColor"
        style={{ transitionDelay: `${TITTLE_DELAY}ms` }}
        className={cn(
          "origin-center scale-0 transition-[scale] duration-450 ease-[cubic-bezier(0.34,1.56,0.64,1)] [transform-box:fill-box]",
          "group-data-[shown=true]:scale-100",
          "motion-reduce:scale-100 motion-reduce:transition-none",
        )}
      />
    </svg>
  );
}
