"use client";

import { type CSSProperties, type ReactNode, type Ref, useEffect, useRef, useState } from "react";

import { cn } from "@multica/ui/lib/utils";

import { AUTOPILOTS_FIGURE, CHAT_FIGURE, SKILLS_FIGURE } from "./figure-art";
import { usePrefersReducedMotion, useStoryLoop } from "./mock-kit";

type Vec = readonly [number, number];

const shift = ([x, y]: Vec, scale = 1) => `${x * scale}px ${y * scale}px`;

function Art({ svg, className, style }: { svg: string; className?: string; style?: CSSProperties }) {
  return <g className={className} style={style} dangerouslySetInnerHTML={{ __html: svg }} />;
}

function Figure({
  figure,
  sceneRef,
  resetting = false,
  children,
}: {
  figure: { width: number; height: number };
  sceneRef?: Ref<HTMLDivElement>;
  resetting?: boolean;
  children: ReactNode;
}) {
  return (
    <div
      ref={sceneRef}
      className={cn("site-iso absolute inset-0 transition-opacity duration-500", resetting && "opacity-0")}
    >
      <svg viewBox={`0 0 ${figure.width} ${figure.height}`} className="iso-svg size-full" aria-hidden>
        {children}
      </svg>
    </div>
  );
}

/** Starts the part at `from`, passes `via`, and settles where it was built. */
function hop(from: Vec, via: Vec, delay: number): CSSProperties {
  return {
    "--site-from": shift(from),
    "--site-via": shift(via),
    animationDelay: `${delay}ms`,
  } as CSSProperties;
}

// --- Chat: a reply becomes a card on the board ----------------------------

// Two messages land, the agent replies, its card hops onto the board, and
// the column's lamp shows the agent at work.
const CHAT_HOLDS = [900, 900, 1100, 1000, 2400, 500];
const CHAT_HOP_MS = 800;

export function ChatFigure() {
  const { ref, step } = useStoryLoop(CHAT_HOLDS, 4);
  const figure = CHAT_FIGURE;
  const resetting = step === CHAT_HOLDS.length - 1;
  return (
    <Figure figure={figure} sceneRef={ref} resetting={resetting}>
      <Art svg={figure.back} />
      {figure.bubbles.map((svg, index) => {
        const shown = step >= index || resetting;
        return (
          <Art
            key={index}
            svg={svg}
            className="transition-[translate,opacity] duration-500 ease-out motion-reduce:transition-none"
            style={{ translate: shown ? "0 0" : shift(figure.drop), opacity: shown ? 1 : 0 }}
          />
        );
      })}
      <Art svg={figure.board} />
      <g data-on={step >= 4 && !resetting ? "agent" : undefined}>
        <Art svg={figure.lamp} />
      </g>
      {step >= 3 && (
        <Art
          svg={figure.card}
          className="animate-site-hop motion-reduce:animate-none"
          style={{ ...hop(figure.from, figure.via, 0), animationDuration: `${CHAT_HOP_MS}ms` }}
        />
      )}
    </Figure>
  );
}

// --- Skills: one edit reaches every agent ---------------------------------

// A line is added to the skill, a token runs to each agent, and each agent's
// lamp comes on as its token arrives.
const SKILL_HOLDS = [900, 1100, 1300, 2400, 500];
const TOKEN_MS = 900;
const TOKEN_STAGGER_MS = 120;

export function SkillsFigure() {
  const { ref, step } = useStoryLoop(SKILL_HOLDS, 3);
  const figure = SKILLS_FIGURE;
  const resetting = step === SKILL_HOLDS.length - 1;
  return (
    <Figure figure={figure} sceneRef={ref} resetting={resetting}>
      <Art svg={figure.back} />
      <Art
        svg={figure.added}
        className="transition-opacity duration-500 motion-reduce:transition-none"
        style={{ opacity: step >= 1 || resetting ? 1 : 0 }}
      />
      {figure.pucks.map((svg, index) => (
        <g key={index}>
          <Art svg={svg} />
          <g data-on={step >= 3 && !resetting ? "agent" : undefined}>
            <Art svg={figure.lamps[index]!} />
          </g>
        </g>
      ))}
      {step === 2 &&
        figure.tokens.map((token, index) => (
          <circle
            key={index}
            cx={token.at[0]}
            cy={token.at[1]}
            r={2.6}
            className="animate-site-hop fill-site-agent stroke-site-surface motion-reduce:hidden"
            style={{
              ...hop(token.from, token.via, index * TOKEN_STAGGER_MS),
              animationDuration: `${TOKEN_MS}ms`,
              strokeWidth: 1.2,
            }}
          />
        ))}
    </Figure>
  );
}

// --- Autopilots: a day dial whose pegs trip the dispenser -----------------

const HOUR_MS = 400;
const DAY_MS = HOUR_MS * 24;
// The frame shown under reduced motion: late in the day, every ticket out.
const STILL_HOUR = 21.2;

// Server and browser trig can differ in the last digit; round so the
// server-rendered frame hydrates.
const num = (value: number) => value.toFixed(3);
const clamp01 = (value: number) => Math.min(1, Math.max(0, value));
const easeOut = (value: number) => 1 - (1 - value) ** 3;

function useInView() {
  const ref = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const intersection = new IntersectionObserver(([entry]) => setVisible(entry?.isIntersecting ?? false));
    intersection.observe(el);
    return () => intersection.disconnect();
  }, []);
  return { ref, visible };
}

/** Screen transforms for every moving part at `hour` of the day. */
function autopilotPose(hour: number) {
  const figure = AUTOPILOTS_FIGURE;
  const [ax, ay, bx, by] = figure.axes;
  const [cx, cy] = figure.centre;
  // The dial turns one way through the day so each peg reaches the lever at
  // its own hour.
  const turn = figure.leverAngle - (hour / 24) * Math.PI * 2;
  const cos = Math.cos(turn);
  const sin = Math.sin(turn);
  // A world rotation about z, seen on screen: axes · rotation · axes⁻¹.
  const det = ax * by - bx * ay;
  const r00 = ax * cos + bx * sin;
  const r01 = -ax * sin + bx * cos;
  const r10 = ay * cos + by * sin;
  const r11 = -ay * sin + by * cos;
  const m00 = (r00 * by - r01 * ay) / det;
  const m01 = (-r00 * bx + r01 * ax) / det;
  const m10 = (r10 * by - r11 * ay) / det;
  const m11 = (-r10 * bx + r11 * ax) / det;
  const face = `matrix(${[m00, m10, m01, m11, cx - m00 * cx - m01 * cy, cy - m10 * cx - m11 * cy].map(num).join(" ")})`;

  const pegs = figure.hours.map((at) => {
    const angle = (at / 24) * Math.PI * 2 + turn;
    const x = figure.dial[0] + Math.sin(angle) * figure.pegRadius;
    const y = figure.dial[1] - Math.cos(angle) * figure.pegRadius;
    return `translate(${num(ax * x + bx * y)} ${num(ay * x + by * y)})`;
  });

  const trip = Math.max(
    ...figure.hours.map((at) => {
      const off = Math.abs(hour - at);
      return off < 0.6 ? (Math.cos((Math.PI * off) / 0.6) + 1) / 2 : 0;
    }),
  );
  const finger = `translate(${num(figure.lift[0] * trip)} ${num(figure.lift[1] * trip)})`;

  // Tickets clear off the tray before midnight so the day can start over.
  const clear = 1 - clamp01((hour - 22) / 1.4);
  const tickets = figure.hours.map((at, index) => {
    const out = easeOut(clamp01((hour - at) / 1.1));
    return {
      transform: `translate(${num(figure.eject[0] * out + figure.stack[0] * index)} ${num(figure.eject[1] * out + figure.stack[1] * index)})`,
      opacity: num(out > 0 ? clear : 0),
    };
  });
  return { face, pegs, finger, tickets };
}

export function AutopilotsFigure() {
  const figure = AUTOPILOTS_FIGURE;
  const { ref, visible } = useInView();
  const reducedMotion = usePrefersReducedMotion();
  const face = useRef<SVGGElement>(null);
  const finger = useRef<SVGGElement>(null);
  const pegs = useRef<(SVGGElement | null)[]>([]);
  const tickets = useRef<(SVGGElement | null)[]>([]);
  const elapsed = useRef(STILL_HOUR * HOUR_MS);
  const still = autopilotPose(STILL_HOUR);

  useEffect(() => {
    if (!visible || reducedMotion) return;
    let frame = 0;
    let last: number | null = null;
    const tick = (now: number) => {
      if (last != null) elapsed.current = (elapsed.current + now - last) % DAY_MS;
      last = now;
      const pose = autopilotPose(elapsed.current / HOUR_MS);
      face.current?.setAttribute("transform", pose.face);
      finger.current?.setAttribute("transform", pose.finger);
      pose.pegs.forEach((transform, index) => pegs.current[index]?.setAttribute("transform", transform));
      pose.tickets.forEach(({ transform, opacity }, index) => {
        tickets.current[index]?.setAttribute("transform", transform);
        tickets.current[index]?.setAttribute("opacity", opacity);
      });
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [visible, reducedMotion]);

  return (
    <Figure figure={figure} sceneRef={ref}>
      <defs>
        {figure.ticketClips.map((d, index) => (
          <clipPath key={index} id={`site-ticket-clip-${index}`}>
            <path d={d} />
          </clipPath>
        ))}
      </defs>
      <Art svg={figure.back} />
      <g ref={face} transform={still.face}>
        <Art svg={figure.face} />
      </g>
      {figure.hours.map((at, index) => (
        <g
          key={at}
          ref={(el) => {
            pegs.current[index] = el;
          }}
          transform={still.pegs[index]}
        >
          <Art svg={figure.peg} />
        </g>
      ))}
      <Art svg={figure.hub} />
      <Art svg={figure.leverBack} />
      <g ref={finger} transform={still.finger}>
        <Art svg={figure.finger} />
      </g>
      <Art svg={figure.box} />
      <Art svg={figure.tray} />
      {figure.hours.map((at, index) => (
        <g key={at} clipPath={`url(#site-ticket-clip-${index})`}>
          <g
            ref={(el) => {
              tickets.current[index] = el;
            }}
            transform={still.tickets[index]!.transform}
            opacity={still.tickets[index]!.opacity}
          >
            <Art svg={figure.ticketLit} />
          </g>
        </g>
      ))}
    </Figure>
  );
}
