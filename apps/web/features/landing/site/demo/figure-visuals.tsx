"use client";

import { type CSSProperties, type ReactNode, type Ref, useEffect, useRef, useState } from "react";

import { cn } from "@multica/ui/lib/utils";

import { AUTOPILOTS_FIGURE, RUNTIMES_FIGURE, WORKFLOWS_FIGURE } from "./figure-art";
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

// --- Workflows: the issue rides a conveyor through each step's station ----

// Stations: Todo (a person scopes it), In Progress and In Review (agents),
// Done (a person merges). Review sends it back once before it goes through.
const STATION_OWNERS = ["ink", "agent", "agent", "ink"] as const;
const WORKFLOW_STOPS = [0, 1, 2, 1, 2, 3, 3];
const WORKFLOW_HOLDS = [1300, 1500, 1500, 1500, 1500, 2200, 500];
const SLIDE_MS = 700;

export function WorkflowFigure() {
  const { ref, step } = useStoryLoop(WORKFLOW_HOLDS, 5);
  const figure = WORKFLOWS_FIGURE;
  const resetting = step === WORKFLOW_HOLDS.length - 1;
  const stop = resetting ? 0 : (WORKFLOW_STOPS[step] ?? 0);
  return (
    <Figure figure={figure} sceneRef={ref} resetting={resetting}>
      <Art svg={figure.back} />
      <Art
        svg={figure.sled}
        className="transition-[translate] ease-in-out motion-reduce:transition-none"
        style={{ translate: shift(figure.stations[stop]!), transitionDuration: `${SLIDE_MS}ms` }}
      />
      {figure.stamps.map((svg, index) => (
        <Art
          key={index === stop && !resetting ? `press-${step}` : index}
          svg={svg}
          className={cn(index === stop && !resetting && "animate-site-stamp motion-reduce:animate-none")}
          style={
            {
              "--site-press": shift(figure.press),
              animationDelay: `${step === 0 ? 300 : SLIDE_MS}ms`,
            } as CSSProperties
          }
        />
      ))}
      {figure.beams.map((svg, index) => (
        <g key={index}>
          <Art svg={svg} />
          <g data-on={index === stop && !resetting ? STATION_OWNERS[index] : undefined}>
            <Art svg={figure.lamps[index]!} />
          </g>
        </g>
      ))}
      <Art svg={figure.front} />
    </Figure>
  );
}

// --- Runtimes: a rack whose controller dispatches work to each machine ----

// Bay 1 is already busy. The controller sends work to bays 0 and 2, then a
// fourth machine lowers into the empty bay and gets work too.
const RUNTIME_SENDS = [null, 0, 2, null, 3, null, null];
const RUNTIME_ON: number[][] = [[1], [1], [0, 1], [0, 1, 2], [0, 1, 2], [0, 1, 2, 3], []];
const RUNTIME_HOLDS = [900, 1100, 1300, 1400, 1100, 2400, 500];
const TOKEN_MS = 700;

export function RuntimesFigure() {
  const { ref, step } = useStoryLoop(RUNTIME_HOLDS, 5);
  const figure = RUNTIMES_FIGURE;
  const resetting = step === RUNTIME_HOLDS.length - 1;
  const send = RUNTIME_SENDS[step];
  const on = RUNTIME_ON[step] ?? [];
  const seated = step >= 3 && !resetting;
  const [ox, oy] = figure.origin;
  const from = figure.bus.from;
  return (
    <Figure figure={figure} sceneRef={ref} resetting={resetting}>
      <Art svg={figure.back} />
      {send != null && (
        <circle
          key={step}
          cx={ox + from[0]}
          cy={oy + from[1]}
          r={3.2}
          className="animate-site-token fill-site-agent stroke-site-surface motion-reduce:hidden"
          style={
            {
              strokeWidth: 1.5,
              "--site-to": `${figure.bus.bays[send]![0] - from[0]}px ${figure.bus.bays[send]![1] - from[1]}px`,
            } as CSSProperties
          }
        />
      )}
      {figure.modules.map((svg, index) => {
        const arriving = index === 3;
        return (
          <g
            key={index}
            className={cn(arriving && "transition-[translate,opacity] duration-700 ease-out motion-reduce:transition-none")}
            style={arriving ? { translate: seated ? "0 0" : shift(figure.drop, 0.45), opacity: seated ? 1 : 0 } : undefined}
          >
            <Art svg={svg} />
            <g data-on={on.includes(index) ? "agent" : undefined}>
              <Art svg={figure.lamps[index]!} />
            </g>
          </g>
        );
      })}
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
