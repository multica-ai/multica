"use client";

import { useEffect, useRef } from "react";
import { WORDMARK, WORDMARK_STAR, WORDMARK_STROKE } from "@multica/ui/lib/multica-brand";
import { cn } from "@multica/ui/lib/utils";

/*
 * The footer's closing wordmark, at the full width of the page, drawn as a
 * halftone: square cells in a faint tint of the ink, solid at the top and
 * thinning out towards the bottom edge through an ordered dither.
 *
 * The first time it comes into view the cells develop from the top down.
 * Under the pointer a soft spotlight fills the dither in and deepens it to the
 * ink, trailing the pointer slightly and fading when it leaves.
 *
 * The star on the i can be grabbed and turned. Let go mid-turn and it keeps
 * spinning at the speed of the flick, slows under friction and settles back
 * upright on one of its eight arms. Under reduced motion the wordmark is the
 * finished frame, the spotlight tracks without easing and the star stays
 * where it is let go.
 */

const [VIEW_X, VIEW_Y, VIEW_W, VIEW_H] = WORDMARK.viewBox.split(" ").map(Number) as [number, number, number, number];
const LETTER_PATHS = WORDMARK.letters.map((letter) => letter.d);
const STAR = (() => {
  const i = WORDMARK.letters.find((letter) => letter.star)!;
  // The star is centred on the i's stem, the first move of its path.
  return { d: i.star!, cx: Number(/^M([-\d.]+)/.exec(i.d)![1]), cy: WORDMARK_STAR.cy, r: WORDMARK_STAR.r };
})();

/** Cell edge in CSS pixels. */
const CELL = 3;
const BASE_ALPHA = 0.1;
const SPOT_ALPHA = 0.65;
/** Spotlight radius as a share of the wordmark's width. */
const SPOT_RADIUS = 0.09;
const FOLLOW = 0.2;
const SPOT_FADE = 0.14;
const DEVELOP_STEP = 0.022;

/** How far from the star's centre a press still grabs it, in star radii. */
const STAR_GRAB = 1.8;
/** The release speed is measured over the last stretch of the drag, in ms. */
const FLICK_WINDOW = 90;
/** Spin speed cap, in radians per second (about six turns). */
const MAX_SPIN = 38;
/** Exponential slow-down of a free spin, per second. */
const FRICTION = 1.7;
/** Below this speed (rad/s) the star stops coasting and springs to the nearest arm. */
const SETTLE_SPEED = 2.4;
const SETTLE_STIFFNESS = 90;
const ARM_STEP = Math.PI / 4;

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

/** Marks the cells of `cols` × `rows` whose centres land on what `paint` draws, in CSS pixels. */
function sampleCells(
  width: number,
  height: number,
  cols: number,
  rows: number,
  paint: (ctx: CanvasRenderingContext2D) => void,
): Uint8Array {
  const cells = new Uint8Array(cols * rows);
  const scratch = document.createElement("canvas");
  scratch.width = Math.max(1, Math.ceil(width));
  scratch.height = Math.max(1, Math.ceil(height));
  const ctx = scratch.getContext("2d", { willReadFrequently: true });
  if (!ctx) return cells;
  paint(ctx);
  const alpha = ctx.getImageData(0, 0, scratch.width, scratch.height).data;
  for (let row = 0; row < rows; row++) {
    const y = Math.min(scratch.height - 1, Math.floor(row * CELL + CELL / 2));
    for (let col = 0; col < cols; col++) {
      const x = Math.min(scratch.width - 1, Math.floor(col * CELL + CELL / 2));
      cells[row * cols + col] = alpha[(y * scratch.width + x) * 4 + 3]! >= 128 ? 1 : 0;
    }
  }
  return cells;
}

/** Shortest signed turn from angle `a` to angle `b`. */
function turn(a: number, b: number) {
  return Math.atan2(Math.sin(b - a), Math.cos(b - a));
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
    let scale = 1;
    let letters: Uint8Array = new Uint8Array();
    let inside: Uint8Array = new Uint8Array();
    let developed = still ? 1 : 0;
    let inView = false;
    let frame = 0;
    let lastFrame = 0;
    const spot = { x: 0, y: 0, tx: 0, ty: 0, r: 0, tr: 0 };

    // The star: its centre and grab radius in CSS pixels, the block of cells it
    // can sweep, and its spin.
    const star = { x: 0, y: 0, reach: 0, col0: 0, row0: 0, span: 0, drawnAt: Number.NaN };
    const spin = { angle: 0, speed: 0, free: false, settling: false, target: 0 };
    let drag: { id: number; from: number; samples: { t: number; angle: number }[] } | null = null;

    const pointAngle = (clientX: number, clientY: number) => {
      const rect = canvas.getBoundingClientRect();
      return Math.atan2(clientY - rect.top - star.y, clientX - rect.left - star.x);
    };
    const onStar = (clientX: number, clientY: number) => {
      const rect = canvas.getBoundingClientRect();
      return Math.hypot(clientX - rect.left - star.x, clientY - rect.top - star.y) <= star.reach * STAR_GRAB;
    };

    /** Re-samples the star's cells at its current angle into `inside`. */
    const placeStar = () => {
      if (star.drawnAt === spin.angle || !cols) return;
      star.drawnAt = spin.angle;
      const { col0, row0, span } = star;
      const cells = sampleCells(span * CELL, span * CELL, span, span, (scratch) => {
        scratch.translate(star.x - col0 * CELL, star.y - row0 * CELL);
        scratch.rotate(spin.angle);
        scratch.scale(scale, scale);
        scratch.translate(-STAR.cx, -STAR.cy);
        scratch.fill(new Path2D(STAR.d));
      });
      for (let row = 0; row < span; row++) {
        const r = row0 + row;
        if (r < 0 || r >= rows) continue;
        for (let col = 0; col < span; col++) {
          const c = col0 + col;
          if (c < 0 || c >= cols) continue;
          const i = r * cols + c;
          inside[i] = letters[i]! | cells[row * span + col]!;
        }
      }
    };

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
      scale = width / VIEW_W;
      letters =
        width && height
          ? sampleCells(width, height, cols, rows, (scratch) => {
              scratch.setTransform(scale, 0, 0, scale, -VIEW_X * scale, -VIEW_Y * scale);
              scratch.lineWidth = WORDMARK_STROKE;
              for (const d of LETTER_PATHS) scratch.stroke(new Path2D(d));
            })
          : new Uint8Array();
      inside = letters.slice();
      star.x = (STAR.cx - VIEW_X) * scale;
      star.y = (STAR.cy - VIEW_Y) * scale;
      star.reach = STAR.r * scale;
      star.col0 = Math.floor((star.x - star.reach) / CELL) - 1;
      star.row0 = Math.floor((star.y - star.reach) / CELL) - 1;
      star.span = Math.ceil((2 * star.reach) / CELL) + 3;
      star.drawnAt = Number.NaN;
      placeStar();
    };

    /** Advances the let-go star by `dt` seconds; true while it is still moving. */
    const coast = (dt: number) => {
      if (drag || !spin.free) return false;
      if (!spin.settling && Math.abs(spin.speed) < SETTLE_SPEED) {
        spin.settling = true;
        const ahead = spin.angle + Math.sign(spin.speed) * ARM_STEP * 0.25;
        spin.target = Math.round(ahead / ARM_STEP) * ARM_STEP;
      }
      if (spin.settling) {
        const damping = 2 * Math.sqrt(SETTLE_STIFFNESS);
        spin.speed += (-SETTLE_STIFFNESS * (spin.angle - spin.target) - damping * spin.speed) * dt;
        spin.angle += spin.speed * dt;
        if (Math.abs(spin.angle - spin.target) < 1e-3 && Math.abs(spin.speed) < 1e-2) {
          spin.angle = spin.target % (2 * Math.PI);
          spin.speed = 0;
          spin.free = false;
          spin.settling = false;
          return false;
        }
        return true;
      }
      spin.speed *= Math.exp(-FRICTION * dt);
      spin.angle += spin.speed * dt;
      return true;
    };

    const draw = (now: number) => {
      frame = 0;
      const dt = lastFrame ? Math.min(0.05, (now - lastFrame) / 1000) : 1 / 60;
      lastFrame = now;
      const ease = still ? 1 : FOLLOW;
      spot.x += (spot.tx - spot.x) * ease;
      spot.y += (spot.ty - spot.y) * ease;
      spot.r += (spot.tr - spot.r) * (still ? 1 : SPOT_FADE);
      if (inView && developed < 1) developed = Math.min(1, developed + DEVELOP_STEP);
      const spinning = coast(dt);
      placeStar();

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
      if (moving || spinning || (inView && developed < 1)) frame = requestAnimationFrame(draw);
      else lastFrame = 0;
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
      if (drag && event.pointerId === drag.id) {
        const angle = pointAngle(event.clientX, event.clientY);
        spin.angle += turn(drag.from, angle);
        drag.from = angle;
        const t = event.timeStamp;
        drag.samples.push({ t, angle: spin.angle });
        while (drag.samples.length > 2 && t - drag.samples[0]!.t > FLICK_WINDOW) drag.samples.shift();
      } else if (!drag) {
        canvas.style.cursor = onStar(event.clientX, event.clientY) ? "grab" : "";
      }
      schedule();
    };
    const onLeave = () => {
      spot.tr = 0;
      schedule();
    };
    const onDown = (event: PointerEvent) => {
      if (drag || event.button !== 0 || !onStar(event.clientX, event.clientY)) return;
      event.preventDefault();
      canvas.setPointerCapture(event.pointerId);
      canvas.style.cursor = "grabbing";
      drag = {
        id: event.pointerId,
        from: pointAngle(event.clientX, event.clientY),
        samples: [{ t: event.timeStamp, angle: spin.angle }],
      };
      spin.speed = 0;
      spin.free = false;
      spin.settling = false;
    };
    const onUp = (event: PointerEvent) => {
      if (!drag || event.pointerId !== drag.id) return;
      const { samples } = drag;
      const first = samples[0]!;
      const last = samples[samples.length - 1]!;
      // Held still before letting go: no flick.
      const fresh = event.timeStamp - last.t < FLICK_WINDOW / 2;
      const span = (last.t - first.t) / 1000;
      const speed = fresh && span > 0 ? (last.angle - first.angle) / span : 0;
      drag = null;
      canvas.style.cursor = onStar(event.clientX, event.clientY) ? "grab" : "";
      if (canvas.hasPointerCapture(event.pointerId)) canvas.releasePointerCapture(event.pointerId);
      if (still) return;
      spin.speed = Math.max(-MAX_SPIN, Math.min(MAX_SPIN, speed));
      spin.free = true;
      spin.settling = false;
      lastFrame = 0;
      schedule();
    };
    // Touches that start on the star turn it instead of scrolling the page.
    const onTouchStart = (event: TouchEvent) => {
      const touch = event.touches[0];
      if (event.touches.length === 1 && touch && onStar(touch.clientX, touch.clientY)) event.preventDefault();
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
    canvas.addEventListener("pointerdown", onDown);
    canvas.addEventListener("pointerup", onUp);
    canvas.addEventListener("pointercancel", onUp);
    canvas.addEventListener("touchstart", onTouchStart, { passive: false });
    return () => {
      cancelAnimationFrame(frame);
      view.disconnect();
      resize.disconnect();
      canvas.removeEventListener("pointermove", onMove);
      canvas.removeEventListener("pointerleave", onLeave);
      canvas.removeEventListener("pointerdown", onDown);
      canvas.removeEventListener("pointerup", onUp);
      canvas.removeEventListener("pointercancel", onUp);
      canvas.removeEventListener("touchstart", onTouchStart);
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
