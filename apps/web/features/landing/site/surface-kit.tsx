"use client";

import type { ComponentType, ReactNode } from "react";
import { Check, type LucideIcon } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Container, Eyebrow, focusRing } from "./primitives";
import { AdvanceLine, useAutoAdvance } from "./auto-advance";

/*
 * Building blocks shared by the product surface pages (Cloud, Desktop, CLI,
 * Chat, Mobile): the hero with its scene, the stepped walkthrough, the
 * two-column comparison, and the check list.
 */

export function SurfaceHero({
  eyebrow,
  title,
  subtitle,
  lead,
  actions,
  frost,
  label,
  children,
}: {
  eyebrow: string;
  title: string;
  subtitle: string;
  lead: ReactNode;
  actions: ReactNode;
  frost: string;
  label: string;
  children: ReactNode;
}) {
  return (
    <section className="pt-site-hero pb-site-section">
      <Container className="max-w-site-wide">
        <Eyebrow>{eyebrow}</Eyebrow>
        <h1 className="mt-4 font-site-display text-site-heading text-balance">
          <span className="block text-site-ink">{title}</span>
          <span className="block text-site-muted">{subtitle}</span>
        </h1>
        <p className="mt-3 max-w-[38rem] text-site-body text-site-muted text-pretty">{lead}</p>
        <div className="mt-6 flex flex-wrap gap-2.5">{actions}</div>
      </Container>
      <Container className="mt-10 max-w-site-wide lg:mt-12">
        <div role="img" aria-label={label} data-frost={frost} className="site-frost overflow-hidden rounded-site-panel">
          {children}
        </div>
      </Container>
    </section>
  );
}

export type Walkthrough = { title: string; body: string; visual: ComponentType };

/**
 * A few steps beside one large visual: the list advances on its own while it
 * is on screen (see `useAutoAdvance`) and the visuals cross-fade in place.
 * `side` puts the list on the left or the right of the visual. Visuals are
 * centered and scaled up with the frame, unless `fill` is set: then each one
 * fills the frame and places itself.
 */
export function WalkthroughTabs({
  steps,
  hold = 5000,
  side = "right",
  numbered = true,
  fill = false,
}: {
  steps: Walkthrough[];
  hold?: number;
  side?: "left" | "right";
  numbered?: boolean;
  fill?: boolean;
}) {
  const cycle = useAutoAdvance(steps.length);
  return (
    <div
      ref={cycle.ref}
      className={cn(
        "grid gap-8 lg:gap-14",
        side === "right" ? "lg:grid-cols-[minmax(0,1fr)_minmax(0,22rem)]" : "lg:grid-cols-[minmax(0,22rem)_minmax(0,1fr)]",
      )}
    >
      <div
        className={cn(
          "site-visual relative h-[23rem] overflow-hidden rounded-site-panel sm:h-[26rem] lg:h-[30rem]",
          side === "left" && "lg:order-2",
        )}
      >
        {steps.map((step, index) => (
          <div
            key={step.title}
            aria-hidden={index !== cycle.active}
            inert={index !== cycle.active}
            className={cn(
              "absolute inset-0 flex items-center justify-center text-foreground transition-[opacity,translate] duration-500 ease-[cubic-bezier(0.32,0.72,0,1)] motion-reduce:transition-none",
              index === cycle.active ? "opacity-100" : "translate-y-2 opacity-0",
            )}
          >
            {fill ? (
              <step.visual />
            ) : (
              <div className="sm:scale-110 lg:scale-[1.2]">
                <step.visual />
              </div>
            )}
          </div>
        ))}
      </div>
      <ol {...cycle.holdProps} className={cn("flex flex-col lg:justify-center", side === "left" && "lg:order-1")}>
        {steps.map((step, index) => {
          const selected = index === cycle.active;
          return (
            <li key={step.title} className="relative border-t border-site-line last:border-b">
              <AdvanceLine
                selected={selected}
                playing={cycle.playing}
                running={cycle.running}
                duration={hold}
                onDone={cycle.advance}
              />
              <button
                type="button"
                aria-pressed={selected}
                onClick={() => cycle.select(index)}
                className={cn("group flex w-full gap-4 py-5 text-left", focusRing)}
              >
                {numbered ? (
                  <span className="pt-1 font-site-mono text-site-label text-site-muted tabular-nums">
                    {String(index + 1).padStart(2, "0")}
                  </span>
                ) : null}
                <span className="min-w-0 flex-1">
                  <span
                    className={cn(
                      "block text-site-card transition-colors",
                      selected ? "text-site-ink" : "text-site-muted group-hover:text-site-ink",
                    )}
                  >
                    {step.title}
                  </span>
                  <span className="mt-site-stack block text-site-body text-site-muted text-pretty">{step.body}</span>
                </span>
              </button>
            </li>
          );
        })}
      </ol>
    </div>
  );
}

export type CompareRow = { label: string; values: [string, string] };

/** Two options compared row by row; on phones the row label moves above its values. */
export function CompareTable({ columns, rows }: { columns: [string, string]; rows: CompareRow[] }) {
  return (
    <div>
      <div className="grid grid-cols-2 gap-x-4 border-b border-site-ink pb-3 text-site-small sm:grid-cols-3">
        <span className="max-sm:hidden" />
        {columns.map((column) => (
          <span key={column} className="text-site-ink">
            {column}
          </span>
        ))}
      </div>
      <dl>
        {rows.map((row) => (
          <div
            key={row.label}
            className="grid grid-cols-2 gap-x-4 gap-y-1.5 border-b border-site-line py-4 text-site-body sm:grid-cols-3"
          >
            <dt className="text-site-muted max-sm:col-span-2 max-sm:text-site-small">{row.label}</dt>
            {row.values.map((value, index) => (
              <dd key={columns[index]} className="text-site-ink">
                {value}
              </dd>
            ))}
          </div>
        ))}
      </dl>
    </div>
  );
}

export type Feature = { icon: LucideIcon; title: string; body: string };

/** Up to four features in a 2×2 grid ruled by hairlines, each led by its icon. */
export function FeatureGrid({ items }: { items: Feature[] }) {
  return (
    <ul className="grid border-y border-site-line sm:grid-cols-2">
      {items.map((item, index) => (
        <li
          key={item.title}
          className={cn(
            "flex gap-5 border-site-line py-8 sm:py-10",
            index > 0 && "max-sm:border-t",
            index > 1 && "sm:border-t",
            index % 2 === 0 ? "sm:pr-8 lg:pr-14" : "sm:border-l sm:pl-8 lg:pl-14",
          )}
        >
          <IconTile>
            <item.icon className="size-5" aria-hidden />
          </IconTile>
          <span className="min-w-0">
            <h3 className="text-site-card text-site-ink">{item.title}</h3>
            <p className="mt-site-stack max-w-[26rem] text-site-body text-site-muted text-pretty">{item.body}</p>
          </span>
        </li>
      ))}
    </ul>
  );
}

export function IconTile({ muted, children }: { muted?: boolean; children: ReactNode }) {
  return (
    <span
      className={cn(
        "flex size-10 shrink-0 items-center justify-center rounded-site-inner border border-site-line bg-site-surface text-site-ink-secondary",
        muted && "opacity-60",
      )}
    >
      {children}
    </span>
  );
}

export function CheckList({ items, className }: { items: ReactNode[]; className?: string }) {
  return (
    <ul className={cn("flex flex-col gap-3", className)}>
      {items.map((item, index) => (
        <li key={index} className="flex gap-2.5 text-site-body text-site-ink-secondary">
          <Check className="mt-1 size-4 shrink-0 text-site-muted" aria-hidden />
          <span className="min-w-0">{item}</span>
        </li>
      ))}
    </ul>
  );
}

/** A command or config file set in the page's own type, for copy a reader may paste. */
export function CodeBlock({ title, children, className }: { title: string; children: ReactNode; className?: string }) {
  return (
    <figure className={cn("overflow-hidden rounded-site-card border border-site-line bg-site-surface", className)}>
      <figcaption className="border-b border-site-line px-4 py-2.5 text-site-small text-site-muted">{title}</figcaption>
      <pre className="overflow-x-auto px-4 py-4 font-site-mono text-[0.8125rem] leading-6 text-site-ink">{children}</pre>
    </figure>
  );
}
