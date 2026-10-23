"use client";

import { useEffect, useRef } from "react";
import { cn } from "@multica/ui/lib/utils";
import { usePrefersReducedMotion } from "./demo/mock-kit";

/*
 * The footer's closing wordmark: "multica" set in Multica Dot, the brand's
 * 5 × 7 dot-matrix face (guidelines v2.0), with 0.8u dots on a 1u grid. Every
 * cell of the grid carries a dot at 8% of the ink, like the mark's unlit
 * dots, and the tittle of the i is the agent dot in amber.
 *
 * Motion, all from the mark's own vocabulary: the letters light up column by
 * column when the footer comes into view and the agent dot lands with a
 * ripple; dots swell under the pointer; and now and then an amber glint runs
 * through the letters. Under reduced motion it is the finished, still frame.
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
const FAINT = 0.08;

type Cell = { r: number; c: number; lit: boolean; agent: boolean };

/** Lays `text` out glyph by glyph, trimming each glyph's empty columns and leaving one column between letters. */
function layout(text: string) {
  const marks = new Map<string, "X" | "A">();
  let x = 0;
  for (const ch of text) {
    const glyph = GLYPHS[ch]!;
    const used = glyph.flatMap((row) => [...row].flatMap((v, c) => (v === "." ? [] : [c])));
    const lo = Math.min(...used);
    const hi = Math.max(...used);
    glyph.forEach((row, r) =>
      [...row].forEach((v, c) => {
        if (v !== ".") marks.set(`${r},${x + c - lo}`, v as "X" | "A");
      }),
    );
    x += hi - lo + 2;
  }
  const cols = x - 1;
  const cells: Cell[] = [];
  for (let r = 0; r < ROWS; r++) {
    for (let c = 0; c < cols; c++) {
      const mark = marks.get(`${r},${c}`);
      cells.push({ r, c, lit: mark !== undefined, agent: mark === "A" });
    }
  }
  return { cells, cols };
}

const { cells: CELLS, cols: COLS } = layout("multica");
const AGENT = CELLS.find((cell) => cell.agent)!;

// Entrance: column scan, then the agent dot and its ripple.
const SCAN_START = 150;
const SCAN_STEP = 32;
const POP_MS = 320;
const AGENT_AT = SCAN_START + COLS * SCAN_STEP + 200;
const RIPPLE_MS = 1200;
const RIPPLE_SPEED = 30 / RIPPLE_MS;
// Idle glint, in columns per millisecond, repeating after the entrance.
const GLINT_EVERY = 7000;
const GLINT_SPEED = 0.016;
// Pointer lens, in grid cells.
const LENS = 3.2;

const clamp01 = (value: number) => Math.min(1, Math.max(0, value));
const bump = (value: number) => Math.exp(-value * value);

type Dot = { alpha: number; scale: number; amber: boolean };

/** The look of `cell` `t` ms after the footer first came into view. */
function dotAt(cell: Cell, t: number, lens: { x: number; y: number; strength: number } | null): Dot {
  let alpha = cell.lit ? 1 : FAINT;
  let scale = 1;
  let amber = cell.agent;

  if (cell.agent) {
    const u = clamp01((t - AGENT_AT) / 400);
    alpha = t < AGENT_AT ? FAINT : 1;
    scale = t < AGENT_AT ? 1 : 1 + 0.6 * Math.sin(Math.PI * u) * (1 - u);
  } else if (cell.lit) {
    const since = t - (SCAN_START + cell.c * SCAN_STEP + cell.r * 6);
    alpha = since < 0 ? FAINT : Math.max(FAINT, clamp01(since / 90));
    scale = since < 0 ? 1 : 1 + 0.4 * (1 - clamp01(since / POP_MS)) ** 2;
  }

  // The agent dot's landing sends a ring out through the grid: lit dots
  // bounce, unlit ones flash amber as it passes.
  const ripple = t - AGENT_AT;
  if (ripple > 0 && ripple < RIPPLE_MS && !cell.agent) {
    const distance = Math.hypot(cell.r - AGENT.r, cell.c - AGENT.c);
    const wave = bump((distance - ripple * RIPPLE_SPEED) / 1.2) * (1 - ripple / RIPPLE_MS);
    if (cell.lit) scale *= 1 + 0.3 * wave;
    else if (0.6 * wave > alpha) {
      alpha = 0.6 * wave;
      amber = true;
    }
  }

  // After the entrance, a glint sweeps the letters from time to time.
  const idle = t - (AGENT_AT + RIPPLE_MS);
  if (idle > 0 && cell.lit && !cell.agent) {
    const front = ((idle % GLINT_EVERY) * GLINT_SPEED) - 4;
    const glint = bump((cell.c - front) / 1.1);
    if (glint > 0.5) amber = true;
    scale *= 1 + 0.25 * glint;
  }

  if (lens && lens.strength > 0) {
    const near = bump(Math.hypot(cell.c - lens.x, cell.r - lens.y) / LENS) * lens.strength;
    // Capped so a swollen dot (r 4 → 5) never touches its neighbours.
    scale *= 1 + 0.25 * near;
    if (!cell.lit) alpha = Math.max(alpha, FAINT + 0.1 * near);
  }
  return { alpha, scale, amber };
}

export function FooterWordmark({ className }: { className?: string }) {
  const svg = useRef<SVGSVGElement>(null);
  const reducedMotion = usePrefersReducedMotion();

  useEffect(() => {
    const el = svg.current;
    if (!el || reducedMotion) return;
    const dots = [...el.querySelectorAll("circle")];
    const shown = CELLS.map(() => ({ alpha: -1, scale: -1, amber: false }));
    const paint = (t: number, lens: Parameters<typeof dotAt>[2]) => {
      CELLS.forEach((cell, index) => {
        const next = dotAt(cell, t, lens);
        const last = shown[index]!;
        const dot = dots[index]!;
        if (Math.abs(next.scale - last.scale) > 0.004) dot.setAttribute("r", (R * next.scale).toFixed(2));
        if (Math.abs(next.alpha - last.alpha) > 0.004) dot.setAttribute("fill-opacity", next.alpha.toFixed(3));
        if (next.amber !== last.amber) dot.setAttribute("class", next.amber ? "fill-site-agent" : "fill-current");
        shown[index] = next;
      });
    };
    // Dark until the footer is seen, so the entrance starts from nothing.
    paint(0, null);

    let visible = false;
    let frame = 0;
    let start: number | null = null;
    const pointer = { x: 0, y: 0, inside: false, strength: 0 };
    const tick = (now: number) => {
      start ??= now;
      pointer.strength += ((pointer.inside ? 1 : 0) - pointer.strength) * 0.15;
      paint(now - start, pointer.strength > 0.01 ? pointer : null);
      frame = visible ? requestAnimationFrame(tick) : 0;
    };
    const intersection = new IntersectionObserver(
      ([entry]) => {
        visible = entry?.isIntersecting ?? false;
        if (visible && !frame) frame = requestAnimationFrame(tick);
      },
      { threshold: 0.4 },
    );
    intersection.observe(el);

    const move = (event: PointerEvent) => {
      if (event.pointerType !== "mouse") return;
      const box = el.getBoundingClientRect();
      pointer.x = ((event.clientX - box.left) / box.width) * COLS - 0.5;
      pointer.y = ((event.clientY - box.top) / box.height) * ROWS - 0.5;
      pointer.inside = true;
    };
    const leave = () => {
      pointer.inside = false;
    };
    el.addEventListener("pointermove", move);
    el.addEventListener("pointerleave", leave);
    return () => {
      cancelAnimationFrame(frame);
      intersection.disconnect();
      el.removeEventListener("pointermove", move);
      el.removeEventListener("pointerleave", leave);
    };
  }, [reducedMotion]);

  return (
    <svg
      ref={svg}
      viewBox={`0 0 ${COLS * U} ${ROWS * U}`}
      role="img"
      aria-label="Multica"
      className={cn("block h-auto w-full text-site-ink", className)}
    >
      {CELLS.map((cell) => (
        <circle
          key={`${cell.r},${cell.c}`}
          cx={cell.c * U + U / 2}
          cy={cell.r * U + U / 2}
          r={R}
          className={cell.agent ? "fill-site-agent" : "fill-current"}
          fillOpacity={cell.lit ? 1 : FAINT}
        />
      ))}
    </svg>
  );
}
