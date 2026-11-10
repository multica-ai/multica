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
 * Over the wordmark the pointer becomes a bulb blower aimed at the star on
 * the i. Each click squeezes the bulb and sends a puff of air, drawn in the
 * same cells, towards the star; every bit of air that reaches it pushes it
 * round like a pinwheel, so closer and faster clicking spins it harder. It
 * slows under friction and settles back upright on one of its eight arms.
 * Under reduced motion the wordmark is the finished frame, the spotlight
 * tracks without easing and a click turns the star one arm with no puff.
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

/** Air per puff, its launch speed (px/s), half-width of its cone (rad) and how quickly it dies down. */
const PUFF_SIZE = 26;
const PUFF_SPEED = 820;
const PUFF_SPREAD = 0.2;
const AIR_DRAG = 1.1;
const AIR_LIFE = 1.4;
const AIR_ALPHA = 0.32;
/** Spin (rad/s) a bit of air adds when it reaches the star at full speed. */
const AIR_PUSH = 0.45;
/** Spin speed cap, in radians per second (about six turns). */
const MAX_SPIN = 38;
/** The blower's nozzle tip within its drawing, which points right. */
const BLOWER_TIP = { x: 54, y: 15 };
/** Exponential slow-down of a free spin, per second. */
const FRICTION = 1;
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

type Air = { x: number; y: number; vx: number; vy: number; age: number; big: boolean; hit: boolean };

export function FooterWordmark({ className }: { className?: string }) {
  const box = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const blowerRef = useRef<SVGSVGElement>(null);
  const bulbRef = useRef<SVGGElement>(null);

  useEffect(() => {
    const host = box.current;
    const canvas = canvasRef.current;
    const blower = blowerRef.current;
    const bulb = bulbRef.current;
    const ctx = canvas?.getContext("2d");
    if (!host || !canvas || !blower || !bulb || !ctx) return;

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

    // The star: its centre and radius in CSS pixels, the block of cells it can
    // sweep, and its spin.
    const star = { x: 0, y: 0, reach: 0, col0: 0, row0: 0, span: 0, drawnAt: Number.NaN };
    const spin = { angle: 0, speed: 0, free: false, settling: false, target: 0 };
    // The blower sits with its nozzle on the pointer, aimed at the star.
    const nozzle = { x: 0, y: 0, aim: 0, shown: false };
    let air: Air[] = [];
    let hideTimer = 0;

    const aimAt = (x: number, y: number) => {
      nozzle.x = x;
      nozzle.y = y;
      // Right on the star the aim is meaningless; keep the last one.
      if (Math.hypot(star.x - x, star.y - y) > star.reach) nozzle.aim = Math.atan2(star.y - y, star.x - x);
      blower.style.transform = `translate(${x - BLOWER_TIP.x}px, ${y - BLOWER_TIP.y}px) rotate(${nozzle.aim}rad)`;
    };
    const showBlower = (shown: boolean) => {
      nozzle.shown = shown;
      blower.style.opacity = shown ? "1" : "0";
    };
    const squeeze = (pressed: boolean) => {
      bulb.style.transition = still ? "none" : pressed ? "transform 70ms ease-in" : "transform 260ms cubic-bezier(.3,1.8,.5,1)";
      bulb.style.transform = pressed ? "scale(1.05, 0.68)" : "";
    };

    const kick = (amount: number) => {
      spin.speed = Math.min(MAX_SPIN, spin.speed + amount);
      spin.free = true;
      spin.settling = false;
    };
    const puff = () => {
      if (still) {
        spin.angle += ARM_STEP;
        return;
      }
      for (let n = 0; n < PUFF_SIZE; n++) {
        const angle = nozzle.aim + (Math.random() + Math.random() - 1) * PUFF_SPREAD;
        const speed = PUFF_SPEED * (0.7 + Math.random() * 0.45);
        air.push({
          x: nozzle.x,
          y: nozzle.y,
          vx: Math.cos(angle) * speed,
          vy: Math.sin(angle) * speed,
          age: Math.random() * -0.05,
          big: Math.random() < 0.3,
          hit: false,
        });
      }
      lastFrame = 0;
    };

    /** Moves the air on by `dt` seconds; what reaches the star pushes it round and swirls off it. */
    const blow = (dt: number) => {
      const slow = Math.exp(-AIR_DRAG * dt);
      air = air.filter((a) => {
        a.age += dt;
        if (a.age < 0) return true;
        a.vx *= slow;
        a.vy *= slow;
        a.x += a.vx * dt;
        a.y += a.vy * dt;
        const dx = a.x - star.x;
        const dy = a.y - star.y;
        const r = Math.hypot(dx, dy);
        if (!a.hit && r < star.reach) {
          a.hit = true;
          const v = Math.hypot(a.vx, a.vy);
          kick((AIR_PUSH * v) / PUFF_SPEED);
          // Off along the star's clockwise turn.
          a.vx = ((-dy / r) * v * 0.55 + a.vx * 0.25) || 0;
          a.vy = ((dx / r) * v * 0.55 + a.vy * 0.25) || 0;
        }
        return a.age < AIR_LIFE && Math.hypot(a.vx, a.vy) > 30;
      });
      return air.length > 0;
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

    /** Advances the blown star by `dt` seconds; true while it is still moving. */
    const coast = (dt: number) => {
      if (!spin.free) return false;
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
      const blowing = blow(dt);
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
      for (const a of air) {
        if (a.age < 0) continue;
        const size = a.big && a.age > 0.12 ? 2 : 1;
        ctx.globalAlpha = AIR_ALPHA * (1 - a.age / AIR_LIFE);
        ctx.fillRect(Math.floor(a.x / CELL) * CELL, Math.floor(a.y / CELL) * CELL, CELL * size, CELL * size);
      }
      ctx.globalAlpha = 1;

      const moving = Math.abs(spot.tx - spot.x) + Math.abs(spot.ty - spot.y) > 0.5 || Math.abs(spot.tr - spot.r) > 0.01;
      if (moving || blowing || spinning || (inView && developed < 1)) frame = requestAnimationFrame(draw);
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
      if (event.pointerType !== "touch") {
        aimAt(spot.tx, spot.ty);
        if (!nozzle.shown) showBlower(true);
      }
      schedule();
    };
    const onLeave = () => {
      spot.tr = 0;
      showBlower(false);
      squeeze(false);
      schedule();
    };
    const onDown = (event: PointerEvent) => {
      if (event.button !== 0) return;
      const rect = canvas.getBoundingClientRect();
      aimAt(event.clientX - rect.left, event.clientY - rect.top);
      squeeze(true);
      puff();
      if (event.pointerType === "touch") {
        // No hover on touch: the blower shows for the tap, then goes.
        showBlower(true);
        clearTimeout(hideTimer);
        hideTimer = window.setTimeout(() => {
          squeeze(false);
          showBlower(false);
        }, 500);
      }
      schedule();
    };
    const onUp = (event: PointerEvent) => {
      if (event.pointerType !== "touch") squeeze(false);
    };
    // Rapid clicks would otherwise select the text around the footer.
    const onMouseDown = (event: MouseEvent) => event.preventDefault();

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
    canvas.addEventListener("mousedown", onMouseDown);
    return () => {
      cancelAnimationFrame(frame);
      clearTimeout(hideTimer);
      view.disconnect();
      resize.disconnect();
      canvas.removeEventListener("pointermove", onMove);
      canvas.removeEventListener("pointerleave", onLeave);
      canvas.removeEventListener("pointerdown", onDown);
      canvas.removeEventListener("pointerup", onUp);
      canvas.removeEventListener("pointercancel", onUp);
      canvas.removeEventListener("mousedown", onMouseDown);
    };
  }, []);

  return (
    <div ref={box} className={cn("relative text-site-ink", className)}>
      <canvas
        ref={canvasRef}
        aria-hidden
        className="block w-full cursor-none select-none"
        style={{ aspectRatio: `${VIEW_W} / ${VIEW_H}` }}
      />
      <svg
        ref={blowerRef}
        aria-hidden
        width="56"
        height="30"
        viewBox="-54 -15 56 30"
        className="pointer-events-none absolute left-0 top-0 overflow-visible opacity-0 transition-opacity duration-150"
        style={{ transformOrigin: `${BLOWER_TIP.x}px ${BLOWER_TIP.y}px` }}
      >
        <path d="M-1 -1.7 L-18 -3.1 L-18 3.1 L-1 1.7 Q0.4 1.7 0.4 0 Q0.4 -1.7 -1 -1.7 Z" fill="currentColor" />
        <rect x="-23" y="-5" width="6" height="10" rx="1.6" fill="currentColor" />
        <g ref={bulbRef} style={{ transformBox: "fill-box", transformOrigin: "100% 50%" }}>
          <ellipse cx="-36" cy="0" rx="14.5" ry="12" fill="currentColor" />
          <ellipse cx="-40" cy="-5" rx="5" ry="2.4" fill="white" opacity="0.3" />
        </g>
      </svg>
    </div>
  );
}
