"use client";

import { useEffect, useRef } from "react";
import { WORDMARK, WORDMARK_STROKE } from "@multica/ui/lib/multica-brand";
import { cn } from "@multica/ui/lib/utils";

/*
 * The footer's closing wordmark, at the full width of the page, drawn as a
 * halftone: square cells in a faint tint of the ink, solid at the top and
 * thinning out towards the bottom edge through an ordered dither.
 *
 * The first time it comes into view the cells develop from the top down.
 * Under the pointer a soft spotlight fills the dither in and deepens it to the
 * ink, trailing the pointer slightly and fading when it leaves. Under reduced
 * motion the wordmark is the finished frame and the spotlight tracks without
 * easing.
 */

const [VIEW_X, VIEW_Y, VIEW_W, VIEW_H] = WORDMARK.viewBox.split(" ").map(Number) as [number, number, number, number];
const LETTERS = WORDMARK.letters.map((letter) => ({ d: letter.d, star: letter.star }));

/** Cell edge in CSS pixels. */
const CELL = 3;
const BASE_ALPHA = 0.1;
const SPOT_ALPHA = 0.65;
/** Spotlight radius as a share of the wordmark's width. */
const SPOT_RADIUS = 0.09;
const FOLLOW = 0.2;
const SPOT_FADE = 0.14;
const DEVELOP_STEP = 0.022;

/** 8×8 Bayer thresholds in (0, 1): a cell shows when its density exceeds its threshold. */
const BAYER = (() => {
  const out = new Float32Array(64);
  for (let y = 0; y < 8; y++) {
    for (let x = 0; x < 8; x++) {
      let v = 0;
      for (let bit = 0, xc = x, yc = x ^ y; bit < 3; bit++, xc >>= 1, yc >>= 1) {
        v = (v << 2) | ((yc & 1) << 1) | (xc & 1);
      }
      out[y * 8 + x] = (v + 0.5) / 64;
    }
  }
  return out;
})();

/** Density down the wordmark: solid to halfway down the letters, thinning to a sparse dither at the baseline. */
function rowDensity(y: number, height: number) {
  return Math.min(1, 1.9 - (y / height) * 1.75);
}

/** Which cells fall inside the letters, for a canvas `width` CSS pixels wide. */
function rasterize(width: number, height: number, cols: number, rows: number) {
  const scratch = document.createElement("canvas");
  scratch.width = width;
  scratch.height = height;
  const ctx = scratch.getContext("2d", { willReadFrequently: true });
  const inside = new Uint8Array(cols * rows);
  if (!ctx) return inside;
  const scale = width / VIEW_W;
  ctx.setTransform(scale, 0, 0, scale, -VIEW_X * scale, -VIEW_Y * scale);
  ctx.lineWidth = WORDMARK_STROKE;
  for (const letter of LETTERS) {
    ctx.stroke(new Path2D(letter.d));
    if (letter.star) ctx.fill(new Path2D(letter.star));
  }
  const alpha = ctx.getImageData(0, 0, width, height).data;
  for (let row = 0; row < rows; row++) {
    const y = Math.min(height - 1, Math.floor(row * CELL + CELL / 2));
    for (let col = 0; col < cols; col++) {
      const x = Math.min(width - 1, Math.floor(col * CELL + CELL / 2));
      inside[row * cols + col] = alpha[(y * width + x) * 4 + 3]! >= 128 ? 1 : 0;
    }
  }
  return inside;
}

export function FooterWordmark({ className }: { className?: string }) {
  const box = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const host = box.current;
    const canvas = canvasRef.current;
    const ctx = canvas?.getContext("2d");
    if (!host || !canvas || !ctx) return;

    const still = matchMedia("(prefers-reduced-motion: reduce)").matches;
    let width = 0;
    let height = 0;
    let cols = 0;
    let rows = 0;
    let inside = new Uint8Array();
    let developed = still ? 1 : 0;
    let inView = false;
    let frame = 0;
    const spot = { x: 0, y: 0, tx: 0, ty: 0, r: 0, tr: 0 };

    const layout = () => {
      width = host.clientWidth;
      height = Math.round((width * VIEW_H) / VIEW_W);
      const dpr = Math.min(2, window.devicePixelRatio || 1);
      canvas.width = Math.round(width * dpr);
      canvas.height = Math.round(height * dpr);
      canvas.style.height = `${height}px`;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      cols = Math.ceil(width / CELL);
      rows = Math.ceil(height / CELL);
      inside = width && height ? rasterize(width, height, cols, rows) : new Uint8Array();
    };

    const draw = () => {
      frame = 0;
      const ease = still ? 1 : FOLLOW;
      spot.x += (spot.tx - spot.x) * ease;
      spot.y += (spot.ty - spot.y) * ease;
      spot.r += (spot.tr - spot.r) * (still ? 1 : SPOT_FADE);
      if (inView && developed < 1) developed = Math.min(1, developed + DEVELOP_STEP);

      ctx.clearRect(0, 0, width, height);
      ctx.fillStyle = getComputedStyle(host).color;
      const radius = Math.max(1, width * SPOT_RADIUS * spot.r);
      const lit = spot.r > 0.01;
      for (let row = 0; row < rows; row++) {
        const y = row * CELL + CELL / 2;
        const base = developed * 1.4 - y / height > 0 ? rowDensity(y, height) : 0;
        for (let col = 0; col < cols; col++) {
          if (!inside[row * cols + col]) continue;
          let glow = 0;
          if (lit) {
            const x = col * CELL + CELL / 2;
            const d = Math.hypot(x - spot.x, y - spot.y) / radius;
            glow = Math.max(0, 1 - d * d);
          }
          if (Math.max(base, glow) <= BAYER[(row % 8) * 8 + (col % 8)]!) continue;
          ctx.globalAlpha = BASE_ALPHA + glow * SPOT_ALPHA;
          ctx.fillRect(col * CELL, row * CELL, CELL, CELL);
        }
      }
      ctx.globalAlpha = 1;

      const moving = Math.abs(spot.tx - spot.x) + Math.abs(spot.ty - spot.y) > 0.5 || Math.abs(spot.tr - spot.r) > 0.01;
      if (moving || (inView && developed < 1)) frame = requestAnimationFrame(draw);
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(draw);
    };

    const onMove = (event: PointerEvent) => {
      const rect = canvas.getBoundingClientRect();
      spot.tx = event.clientX - rect.left;
      spot.ty = event.clientY - rect.top;
      if (spot.r < 0.01) {
        spot.x = spot.tx;
        spot.y = spot.ty;
      }
      spot.tr = 1;
      schedule();
    };
    const onLeave = () => {
      spot.tr = 0;
      schedule();
    };

    const view = new IntersectionObserver(
      ([entry]) => {
        if (!entry?.isIntersecting) return;
        inView = true;
        view.disconnect();
        schedule();
      },
      { threshold: 0.3 },
    );
    const resize = new ResizeObserver(() => {
      layout();
      schedule();
    });
    view.observe(canvas);
    resize.observe(host);
    canvas.addEventListener("pointermove", onMove);
    canvas.addEventListener("pointerleave", onLeave);
    return () => {
      cancelAnimationFrame(frame);
      view.disconnect();
      resize.disconnect();
      canvas.removeEventListener("pointermove", onMove);
      canvas.removeEventListener("pointerleave", onLeave);
    };
  }, []);

  return (
    <div ref={box} className={cn("text-site-ink", className)}>
      <canvas
        ref={canvasRef}
        aria-hidden
        className="block w-full select-none"
        style={{ aspectRatio: `${VIEW_W} / ${VIEW_H}` }}
      />
    </div>
  );
}
