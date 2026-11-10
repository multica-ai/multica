"use client";

import { useEffect, useRef, useState } from "react";
import { WORDMARK, WORDMARK_STAR, WORDMARK_STROKE } from "@multica/ui/lib/multica-brand";
import { cn } from "@multica/ui/lib/utils";
import { Container } from "./primitives";

/*
 * The footer's closing wordmark, at the width of the page frame, drawn as a
 * halftone: square cells in a faint tint of the ink, solid at the top and
 * thinning out towards the bottom edge through an ordered dither.
 *
 * The first time it comes into view the cells develop from the top down.
 * Under the pointer a soft spotlight fills the dither in and deepens it to the
 * ink, trailing the pointer slightly and fading when it leaves.
 *
 * The canvas spans the whole width of the page and the host's top padding, so
 * anywhere around the wordmark the pointer becomes a bulb blower aimed at the
 * star on the i. Each click squeezes the bulb and sends a puff of air, drawn
 * in the same cells, towards the star; every bit of air that reaches it pushes
 * it round like a pinwheel, so closer and faster clicking spins it harder. It
 * slows under friction and settles back upright on one of its eight arms. Air
 * that crosses the letters knocks their cells loose; they drift, then are
 * drawn back home.
 *
 * Every visitor shares the star: the turns each one blows are added to a
 * running total on the server, shown above the wordmark, and turns blown
 * elsewhere since the last look spin this star too. Without the server the
 * total stays hidden and the star is local.
 *
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

/** Cells a bit of air can knock loose, and the chance per cell it passes at full speed. */
const MAX_KNOCKS = 3;
const KNOCK_CHANCE = 0.35;
/** Loose cells drift for this long (s), then a spring pulls them home, ramping up over `DOT_RAMP` (s). */
const DOT_HOLD = 0.7;
const DOT_RAMP = 0.8;
const DOT_DRAG = 2.4;
const DOT_PULL = 26;
/** A loose cell in flight is drawn at least this dark, and is put back after `DOT_MAX_AGE` (s) whatever happens. */
const DOT_ALPHA = 0.3;
const DOT_MAX_AGE = 6;
const MAX_DOTS = 3000;

const STAR_API = "/api/site/star";
const STAR_TURNS_API = "/api/site/star/turns";
/** How often (ms) the page reports its own turns, and the longest it waits (ms) between reads of the total. */
const SYNC_EVERY = 2000;
const READ_EVERY = 6000;
/** The server's cap on turns per report. */
const MAX_REPORT = 30;
/** Spin (rad/s) a turn blown elsewhere adds here, capped so a busy moment still reads as a nudge. */
const REMOTE_PUSH = Math.PI;
const MAX_REMOTE_SPIN = 18;

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

type Air = { x: number; y: number; vx: number; vy: number; age: number; big: boolean; hit: boolean; knocks: number };
type Dot = { i: number; hx: number; hy: number; x: number; y: number; vx: number; vy: number; age: number; alpha: number };

export function FooterWordmark({ className, label, locale }: { className?: string; label: string; locale: string }) {
  const box = useRef<HTMLDivElement>(null);
  const frameRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const blowerRef = useRef<SVGSVGElement>(null);
  const bulbRef = useRef<SVGGElement>(null);
  const [total, setTotal] = useState<number | null>(null);

  useEffect(() => {
    const host = box.current;
    const frameEl = frameRef.current;
    const canvas = canvasRef.current;
    const blower = blowerRef.current;
    const bulb = bulbRef.current;
    const ctx = canvas?.getContext("2d");
    if (!host || !frameEl || !canvas || !blower || !bulb || !ctx) return;

    const still = matchMedia("(prefers-reduced-motion: reduce)").matches;
    // The canvas is `width` × `fullHeight`; the wordmark is `markWidth` ×
    // `height` within it, offset by the page gutter and the host's top padding.
    let width = 0;
    let fullHeight = 0;
    let markWidth = 0;
    let height = 0;
    let ox = 0;
    let oy = 0;
    let cols = 0;
    let rows = 0;
    let scale = 1;
    let letters: Uint8Array = new Uint8Array();
    let inside: Uint8Array = new Uint8Array();
    let loose: Uint8Array = new Uint8Array();
    let developed = still ? 1 : 0;
    let inView = false;
    let frame = 0;
    let lastFrame = 0;
    const spot = { x: 0, y: 0, tx: 0, ty: 0, r: 0, tr: 0 };
    let spotRadius = 1;

    // The star: its centre and radius in CSS pixels, the block of cells it can
    // sweep, and its spin.
    const star = { x: 0, y: 0, reach: 0, col0: 0, row0: 0, span: 0, drawnAt: Number.NaN };
    const spin = { angle: 0, speed: 0, free: false, settling: false, target: 0 };
    // The blower sits with its nozzle on the pointer, aimed at the star.
    const nozzle = { x: 0, y: 0, aim: 0, shown: false };
    let air: Air[] = [];
    let dots: Dot[] = [];
    let hideTimer = 0;

    // The shared total: what the server last reported and has been accounted
    // for here, and this page's own turns not yet reported.
    let known: number | null = null;
    let pending = 0;
    let reporting = 0;
    let shownTotal = -1;
    let syncing = false;
    let lastRead = 0;
    let near = false;
    let syncTimer = 0;

    const showTotal = () => {
      if (known === null) return;
      const next = Math.floor(known + reporting + pending);
      if (next !== shownTotal) {
        shownTotal = next;
        setTotal(next);
      }
    };

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

    /** Adds spin; returns how much was taken, after the cap. */
    const kick = (amount: number, cap = MAX_SPIN) => {
      const before = spin.speed;
      spin.speed = Math.max(before, Math.min(cap, before + amount));
      spin.free = true;
      spin.settling = false;
      return spin.speed - before;
    };
    // Under friction a spin of v rad/s coasts v / FRICTION radians, so the
    // spin this page adds is the turns it will be credited with.
    const credit = (radians: number) => {
      pending += radians / (2 * Math.PI);
      showTotal();
    };
    const puff = () => {
      if (still) {
        spin.angle += ARM_STEP;
        credit(ARM_STEP);
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
          knocks: 0,
        });
      }
      lastFrame = 0;
    };

    /** The alpha a wordmark cell is drawn at right now, or 0 when the dither leaves it out. */
    const shade = (row: number, col: number) => {
      const y = row * CELL + CELL / 2;
      const base = developed * 1.4 - (y - oy) / height > 0 ? rowDensity(y - oy, height) : 0;
      let glow = 0;
      if (spot.r > 0.01) {
        const d = Math.hypot(col * CELL + CELL / 2 - spot.x, y - spot.y) / spotRadius;
        glow = Math.max(0, 1 - d * d);
      }
      if (Math.max(base, glow) <= BAYER[(row % 8) * 8 + (col % 8)]!) return 0;
      return BASE_ALPHA + glow * SPOT_ALPHA;
    };

    /** Knocks loose the drawn letter cells around a bit of air. */
    const knock = (a: Air) => {
      const col = Math.floor(a.x / CELL);
      const row = Math.floor(a.y / CELL);
      const chance = KNOCK_CHANCE * Math.min(1, Math.hypot(a.vx, a.vy) / PUFF_SPEED);
      for (let r = row - 1; r <= row + 1 && a.knocks < MAX_KNOCKS; r++) {
        if (r < 0 || r >= rows) continue;
        for (let c = col - 1; c <= col + 1 && a.knocks < MAX_KNOCKS; c++) {
          if (c < 0 || c >= cols) continue;
          const i = r * cols + c;
          if (!letters[i] || loose[i] || dots.length >= MAX_DOTS || Math.random() > chance) continue;
          const alpha = shade(r, c);
          if (!alpha) continue;
          loose[i] = 1;
          a.knocks++;
          const hx = c * CELL;
          const hy = r * CELL;
          const jitter = 120;
          dots.push({
            i,
            hx,
            hy,
            x: hx,
            y: hy,
            vx: a.vx * (0.35 + Math.random() * 0.35) + (Math.random() - 0.5) * jitter,
            vy: a.vy * (0.35 + Math.random() * 0.35) + (Math.random() - 0.5) * jitter,
            age: 0,
            alpha,
          });
          a.vx *= 0.92;
          a.vy *= 0.92;
        }
      }
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
          credit(kick((AIR_PUSH * v) / PUFF_SPEED) / FRICTION);
          // Off along the star's clockwise turn.
          a.vx = ((-dy / r) * v * 0.55 + a.vx * 0.25) || 0;
          a.vy = ((dx / r) * v * 0.55 + a.vy * 0.25) || 0;
        } else if (a.knocks < MAX_KNOCKS) {
          knock(a);
        }
        return a.age < AIR_LIFE && Math.hypot(a.vx, a.vy) > 30;
      });
      return air.length > 0;
    };

    /** Lets loose cells drift, then springs them home; true while any are away. */
    const scatter = (dt: number) => {
      if (!dots.length) return false;
      const slow = Math.exp(-DOT_DRAG * dt);
      dots = dots.filter((d) => {
        d.age += dt;
        if (d.age > DOT_HOLD) {
          const k = DOT_PULL * Math.min(1, (d.age - DOT_HOLD) / DOT_RAMP);
          const damping = 2 * Math.sqrt(k);
          d.vx += ((d.hx - d.x) * k - damping * d.vx) * dt;
          d.vy += ((d.hy - d.y) * k - damping * d.vy) * dt;
        } else {
          d.vx *= slow;
          d.vy *= slow;
        }
        d.x += d.vx * dt;
        d.y += d.vy * dt;
        const home =
          d.age > DOT_MAX_AGE ||
          (d.age > DOT_HOLD && Math.hypot(d.hx - d.x, d.hy - d.y) < CELL / 2 && Math.hypot(d.vx, d.vy) < 20);
        if (home) loose[d.i] = 0;
        return !home;
      });
      return dots.length > 0;
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
      const hostRect = host.getBoundingClientRect();
      const frameRect = frameEl.getBoundingClientRect();
      width = host.clientWidth;
      fullHeight = host.clientHeight;
      markWidth = frameRect.width;
      height = Math.round((markWidth * VIEW_H) / VIEW_W);
      ox = Math.round(frameRect.left - hostRect.left);
      oy = Math.round(frameRect.top - hostRect.top);
      const dpr = Math.min(2, window.devicePixelRatio || 1);
      canvas.width = Math.round(width * dpr);
      canvas.height = Math.round(fullHeight * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      cols = Math.ceil(width / CELL);
      rows = Math.ceil(fullHeight / CELL);
      scale = markWidth / VIEW_W;
      letters =
        width && height
          ? sampleCells(width, fullHeight, cols, rows, (scratch) => {
              scratch.setTransform(scale, 0, 0, scale, ox - VIEW_X * scale, oy - VIEW_Y * scale);
              scratch.lineWidth = WORDMARK_STROKE;
              for (const d of LETTER_PATHS) scratch.stroke(new Path2D(d));
            })
          : new Uint8Array();
      inside = letters.slice();
      loose = new Uint8Array(letters.length);
      dots = [];
      star.x = ox + (STAR.cx - VIEW_X) * scale;
      star.y = oy + (STAR.cy - VIEW_Y) * scale;
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
      spotRadius = Math.max(1, markWidth * SPOT_RADIUS * spot.r);
      if (inView && developed < 1) developed = Math.min(1, developed + DEVELOP_STEP);
      const blowing = blow(dt);
      const scattered = scatter(dt);
      const spinning = coast(dt);
      placeStar();

      ctx.clearRect(0, 0, width, fullHeight);
      ctx.fillStyle = getComputedStyle(host).color;
      for (let row = 0; row < rows; row++) {
        for (let col = 0; col < cols; col++) {
          const i = row * cols + col;
          if (!inside[i] || loose[i]) continue;
          const alpha = shade(row, col);
          if (!alpha) continue;
          ctx.globalAlpha = alpha;
          ctx.fillRect(col * CELL, row * CELL, CELL, CELL);
        }
      }
      for (const d of dots) {
        const away = Math.min(1, Math.hypot(d.hx - d.x, d.hy - d.y) / 30);
        ctx.globalAlpha = d.alpha + Math.max(0, DOT_ALPHA - d.alpha) * away;
        ctx.fillRect(Math.round(d.x / CELL) * CELL, Math.round(d.y / CELL) * CELL, CELL, CELL);
      }
      for (const a of air) {
        if (a.age < 0) continue;
        const size = a.big && a.age > 0.12 ? 2 : 1;
        ctx.globalAlpha = AIR_ALPHA * (1 - a.age / AIR_LIFE);
        ctx.fillRect(Math.floor(a.x / CELL) * CELL, Math.floor(a.y / CELL) * CELL, CELL * size, CELL * size);
      }
      ctx.globalAlpha = 1;

      const moving = Math.abs(spot.tx - spot.x) + Math.abs(spot.ty - spot.y) > 0.5 || Math.abs(spot.tr - spot.r) > 0.01;
      if (moving || blowing || scattered || spinning || (inView && developed < 1)) frame = requestAnimationFrame(draw);
      else lastFrame = 0;
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(draw);
    };

    /** Takes in a total from the server; turns blown elsewhere since the last one spin this star too. */
    const absorb = (turns: number, reported: number) => {
      if (known !== null) {
        const elsewhere = turns - known - reported;
        if (elsewhere > 0 && !still) {
          kick(elsewhere * REMOTE_PUSH, MAX_REMOTE_SPIN);
          schedule();
        }
      }
      known = turns;
      showTotal();
    };
    const sync = async (final = false) => {
      if (syncing) return;
      syncing = true;
      try {
        if (known !== null && pending >= 1) {
          const turns = Math.min(MAX_REPORT, Math.floor(pending));
          pending -= turns;
          reporting = turns;
          try {
            const res = await fetch(STAR_TURNS_API, {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify({ turns }),
              keepalive: final,
            });
            reporting = 0;
            if (res.ok) {
              absorb(((await res.json()) as { turns: number }).turns, turns);
              lastRead = Date.now();
            } else {
              showTotal();
            }
          } catch {
            reporting = 0;
            pending += turns;
          }
        } else if (!final && Date.now() - lastRead >= READ_EVERY) {
          lastRead = Date.now();
          const res = await fetch(STAR_API, { cache: "no-store" });
          if (res.ok) absorb(((await res.json()) as { turns: number }).turns, 0);
        }
      } catch {
        // The total is a garnish; a failed read leaves it as it was.
      } finally {
        syncing = false;
      }
    };
    const watchTotal = () => {
      const active = near && !document.hidden;
      if (active && !syncTimer) {
        void sync();
        syncTimer = window.setInterval(() => void sync(), SYNC_EVERY);
      } else if (!active && syncTimer) {
        clearInterval(syncTimer);
        syncTimer = 0;
        void sync(true);
      }
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
    const proximity = new IntersectionObserver(
      ([entry]) => {
        near = !!entry?.isIntersecting;
        watchTotal();
      },
      { rootMargin: "200px 0px" },
    );
    const resize = new ResizeObserver(() => {
      layout();
      schedule();
    });
    view.observe(canvas);
    proximity.observe(host);
    resize.observe(host);
    document.addEventListener("visibilitychange", watchTotal);
    canvas.addEventListener("pointermove", onMove);
    canvas.addEventListener("pointerleave", onLeave);
    canvas.addEventListener("pointerdown", onDown);
    canvas.addEventListener("pointerup", onUp);
    canvas.addEventListener("pointercancel", onUp);
    canvas.addEventListener("mousedown", onMouseDown);
    return () => {
      cancelAnimationFrame(frame);
      clearTimeout(hideTimer);
      clearInterval(syncTimer);
      void sync(true);
      view.disconnect();
      proximity.disconnect();
      resize.disconnect();
      document.removeEventListener("visibilitychange", watchTotal);
      canvas.removeEventListener("pointermove", onMove);
      canvas.removeEventListener("pointerleave", onLeave);
      canvas.removeEventListener("pointerdown", onDown);
      canvas.removeEventListener("pointerup", onUp);
      canvas.removeEventListener("pointercancel", onUp);
      canvas.removeEventListener("mousedown", onMouseDown);
    };
  }, []);

  const [before, after = ""] = label.split("{count}");

  return (
    <div ref={box} className={cn("relative text-site-ink", className)}>
      <Container>
        <div ref={frameRef} className="relative" style={{ aspectRatio: `${VIEW_W} / ${VIEW_H}` }}>
          <p
            className={cn(
              "pointer-events-none absolute right-0 bottom-full mb-2 select-none text-site-small text-site-muted transition-opacity duration-500",
              total === null && "opacity-0",
            )}
            aria-live="off"
          >
            {before}
            <span className="tabular-nums text-site-ink">{new Intl.NumberFormat(locale).format(total ?? 0)}</span>
            {after}
          </p>
        </div>
      </Container>
      <canvas ref={canvasRef} aria-hidden className="absolute inset-0 size-full cursor-none select-none" />
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
