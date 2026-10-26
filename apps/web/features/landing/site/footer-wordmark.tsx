"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";

/*
 * The footer's closing wordmark: "multica" set in Multica Dot, the brand's
 * 5 × 7 dot-matrix face (guidelines v2.0), with 0.8u dots on a 1u grid. The
 * tittle of the i is the agent dot in amber.
 *
 * Motion is a single entrance the first time the footer comes into view: the
 * letters fade and grow in as a soft wave from the left, and the agent dot
 * settles in last. Under reduced motion it is the finished, still frame.
 */

const GLYPHS: Record<string, string[]> = {
  m: [".....", ".....", "XX.X.", "X.X.X", "X.X.X", "X.X.X", "X.X.X"],
  u: [".....", ".....", "X...X", "X...X", "X...X", "X..XX", ".XX.X"],
  l: ["XX...", ".X...", ".X...", ".X...", ".X...", ".X...", "XXX.."],
  t: [".X...", ".X...", "XXXX.", ".X...", ".X...", ".X..X", "..XX."],
  i: [".A...", ".....", "XX...", ".X...", ".X...", ".X...", "XXX.."],
  c: [".....", ".....", ".XXXX", "X....", "X....", "X....", ".XXXX"],
  a: [".....", ".....", ".XXX.", "....X", ".XXXX", "X...X", ".XXXX"],
};

const U = 10;
const R = 4;
const ROWS = 7;

type Dot = { r: number; c: number; agent: boolean };

/** Lays `text` out glyph by glyph, trimming each glyph's empty columns and leaving one column between letters. */
function layout(text: string) {
  const dots: Dot[] = [];
  let x = 0;
  for (const ch of text) {
    const glyph = GLYPHS[ch]!;
    const used = glyph.flatMap((row) => [...row].flatMap((v, c) => (v === "." ? [] : [c])));
    const lo = Math.min(...used);
    const hi = Math.max(...used);
    glyph.forEach((row, r) =>
      [...row].forEach((v, c) => {
        if (v !== ".") dots.push({ r, c: x + c - lo, agent: v === "A" });
      }),
    );
    x += hi - lo + 2;
  }
  return { dots, cols: x - 1 };
}

const { dots: DOTS, cols: COLS } = layout("multica");

// The wave runs left to right with a slight downward lean; the agent dot
// lands once the letters are in.
const COL_STEP = 18;
const ROW_STEP = 12;
const AGENT_DELAY = COLS * COL_STEP + 250;

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
      viewBox={`0 0 ${COLS * U} ${ROWS * U}`}
      role="img"
      aria-label="Multica"
      data-shown={shown}
      className={cn("group block h-auto w-full text-site-ink", className)}
    >
      {DOTS.map((dot) => (
        <circle
          key={`${dot.r},${dot.c}`}
          cx={dot.c * U + U / 2}
          cy={dot.r * U + U / 2}
          r={R}
          style={{ transitionDelay: `${dot.agent ? AGENT_DELAY : dot.c * COL_STEP + dot.r * ROW_STEP}ms` }}
          className={cn(
            "origin-center scale-50 opacity-0 transition-[opacity,scale] [transform-box:fill-box]",
            "group-data-[shown=true]:scale-100 group-data-[shown=true]:opacity-100",
            "motion-reduce:scale-100 motion-reduce:opacity-100 motion-reduce:transition-none",
            dot.agent
              ? "fill-site-agent duration-500 ease-[cubic-bezier(0.34,1.4,0.64,1)]"
              : "fill-current duration-700 ease-[cubic-bezier(0.32,0.72,0,1)]",
          )}
        />
      ))}
    </svg>
  );
}
