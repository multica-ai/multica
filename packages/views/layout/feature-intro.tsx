"use client";

import {
  useId,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import {
  ArrowLeft,
  ArrowRight,
  BookOpen,
  type IconComponent,
} from "@multica/ui/icons";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { docsLocalePrefix } from "../common/docs-locale";
import { useLocale, useT } from "../i18n";

export type FeatureDocsSlug =
  | "codegraph"
  | "squads"
  | "projects"
  | "skills"
  | "autopilots"
  | "agents"
  | "daemon-runtimes"
  | "issue-wakeups";

export function featureDocsHref(slug: FeatureDocsSlug, language?: string): string {
  return `https://multica.ai/docs${docsLocalePrefix(language)}/${slug}`;
}

interface FeatureIntroProps {
  icon: IconComponent;
  label: string;
  title: string;
  description: string;
  /** Primary call to action, usually the feature's create flow. */
  action: ReactNode;
  docs: FeatureDocsSlug;
  /** Takes the docs link's place when the page has a more useful next step. */
  secondaryAction?: ReactNode;
  /** Extra guidance under the actions, such as a setup checklist. */
  footer?: ReactNode;
  /** Column labels for the faded list behind the intro. */
  columns: string[];
  /** Wide asides fit a grid of choices instead of a single card. */
  aside?: "card" | "wide";
  children: ReactNode;
}

/**
 * First-run page for a collection that has nothing in it yet. Replaces the
 * collection's empty state, so it disappears on its own once the first item
 * exists and needs no "seen" bookkeeping.
 */
export function FeatureIntro({
  icon: Icon,
  label,
  title,
  description,
  action,
  docs,
  secondaryAction,
  footer,
  columns,
  aside = "card",
  children,
}: FeatureIntroProps) {
  const { t } = useT("layout");
  const locale = useLocale();
  const titleId = useId();

  return (
    <section
      aria-labelledby={titleId}
      className="@container/intro relative flex min-h-0 flex-1 flex-col overflow-hidden"
    >
      <FeatureIntroBackdrop columns={columns} />
      <div className="relative flex min-h-0 flex-1 overflow-x-hidden overflow-y-auto">
        <div
          className={cn(
            "m-auto grid w-full max-w-5xl items-center gap-10 px-6 py-12 @3xl/intro:gap-12 @3xl/intro:px-10 @5xl/intro:gap-20",
            aside === "card"
              ? "@3xl/intro:grid-cols-[minmax(0,1fr)_minmax(0,22.5rem)]"
              : "@4xl/intro:grid-cols-[minmax(0,1fr)_minmax(0,30rem)]",
          )}
        >
          <div className="flex max-w-md flex-col items-start">
            <span className="inline-flex items-center gap-1.5 rounded-full bg-brand/10 px-2.5 py-1 text-caption font-medium text-brand">
              <Icon aria-hidden="true" className="size-3.5" />
              {label}
            </span>
            <h2
              id={titleId}
              className="mt-4 text-title-lg font-semibold text-balance [word-break:auto-phrase] @xl/intro:text-display-sm @5xl/intro:text-display"
            >
              {title}
            </h2>
            <p className="mt-3 text-body text-muted-foreground @xl/intro:text-body-lg">
              {description}
            </p>
            <div className="mt-6 flex flex-wrap items-center gap-2">
              {action}
              {secondaryAction ?? (
                <a
                  href={featureDocsHref(docs, locale)}
                  target="_blank"
                  rel="noopener noreferrer"
                  className={buttonVariants({ variant: "outline" })}
                >
                  <BookOpen aria-hidden="true" />
                  {t(($) => $.feature_intro.read_docs)}
                </a>
              )}
            </div>
            {footer && <div className="mt-6 w-full">{footer}</div>}
          </div>
          <div className="min-w-0">{children}</div>
        </div>
      </div>
    </section>
  );
}

const BACKDROP_ROWS = 12;
const BAR_WIDTHS = ["w-40", "w-28", "w-36", "w-24", "w-32", "w-20"];

/** A faded, static copy of the list this page will become. */
function FeatureIntroBackdrop({ columns }: { columns: string[] }) {
  const template = {
    gridTemplateColumns: `minmax(12rem, 2fr) repeat(${Math.max(columns.length - 1, 0)}, minmax(6rem, 1fr))`,
  };
  return (
    <div
      aria-hidden="true"
      data-testid="feature-intro-backdrop"
      className="pointer-events-none absolute inset-0 overflow-hidden px-4 opacity-60 select-none [mask-image:linear-gradient(to_bottom,black,transparent_70%)]"
    >
      <div
        className="grid h-9 items-center gap-4 border-b text-caption text-muted-foreground"
        style={template}
      >
        {columns.map((column) => (
          <span key={column} className="truncate">
            {column}
          </span>
        ))}
      </div>
      {Array.from({ length: BACKDROP_ROWS }, (_, row) => (
        <div key={row} className="grid h-9 items-center gap-4" style={template}>
          {columns.map((column, col) => (
            <span key={column} className="flex items-center gap-2">
              {col === 0 && <span className="size-5 shrink-0 rounded-md bg-muted" />}
              <span
                className={cn(
                  "h-2.5 max-w-full rounded-full bg-muted",
                  col === 0 ? BAR_WIDTHS[row % BAR_WIDTHS.length] : BAR_WIDTHS[(row + col) % BAR_WIDTHS.length],
                  col > 0 && "max-w-16",
                )}
              />
            </span>
          ))}
        </div>
      ))}
    </div>
  );
}

export interface FeatureIntroSlide {
  id: string;
  title: string;
  description: string;
  art: ReactNode;
  interactiveArt?: boolean;
}

/** Manually paged value cards; the next card peeks in from the right. */
export function FeatureIntroCarousel({
  label,
  slides,
}: {
  label: string;
  slides: FeatureIntroSlide[];
}) {
  const { t } = useT("layout");
  const [index, setIndex] = useState(0);
  const last = slides.length - 1;
  const go = (next: number) => setIndex(Math.min(Math.max(next, 0), last));

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      go(index - 1);
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      go(index + 1);
    }
  };

  return (
    <div
      role="group"
      aria-roledescription={t(($) => $.feature_intro.carousel)}
      aria-label={label}
      onKeyDown={onKeyDown}
      className="w-full max-w-[22.5rem]"
    >
      <div
        className="flex gap-6 transition-transform duration-300 ease-out motion-reduce:transition-none"
        style={{ transform: `translateX(calc(${-index} * (100% + 1.5rem)))` }}
      >
        {slides.map((slide, i) => {
          const active = i === index;
          return (
            <div
              key={slide.id}
              role="group"
              aria-roledescription={t(($) => $.feature_intro.slide)}
              aria-label={t(($) => $.feature_intro.slide_position, {
                current: i + 1,
                total: slides.length,
              })}
              aria-hidden={!active}
              inert={!active}
              className={cn(
                "w-full shrink-0 transition-opacity duration-300 motion-reduce:transition-none",
                active ? "opacity-100" : i === index + 1 ? "opacity-40" : "opacity-0",
              )}
            >
              <div className="h-full overflow-hidden rounded-xl bg-surface shadow-[var(--menu-shadow)] ring-1 ring-surface-border">
                <div
                  aria-hidden={slide.interactiveArt ? undefined : true}
                  className="flex h-64 items-center justify-center overflow-hidden border-b bg-brand/5 bg-[radial-gradient(var(--border)_1px,transparent_1px)] [background-size:16px_16px] px-5 py-4"
                >
                  {slide.art}
                </div>
                <div className="px-5 py-4">
                  <h3 className="text-body font-medium">{slide.title}</h3>
                  <p className="mt-1 text-caption text-muted-foreground">
                    {slide.description}
                  </p>
                </div>
              </div>
            </div>
          );
        })}
      </div>
      {slides.length > 1 && (
        <div className="mt-4 flex items-center gap-3">
          <div className="flex items-center gap-1.5">
            {slides.map((slide, i) => (
              <button
                key={slide.id}
                type="button"
                aria-label={t(($) => $.feature_intro.go_to_slide, { index: i + 1 })}
                aria-current={i === index ? "true" : undefined}
                onClick={() => go(i)}
                className={cn(
                  "h-1.5 rounded-full transition-all motion-reduce:transition-none",
                  i === index ? "w-4 bg-foreground" : "w-1.5 bg-muted-foreground/30 hover:bg-muted-foreground/60",
                )}
              />
            ))}
          </div>
          <div className="ml-auto flex items-center gap-1">
            <Button
              variant="outline"
              size="icon-sm"
              aria-label={t(($) => $.feature_intro.previous)}
              disabled={index === 0}
              onClick={() => go(index - 1)}
            >
              <ArrowLeft aria-hidden="true" />
            </Button>
            <Button
              variant="outline"
              size="icon-sm"
              aria-label={t(($) => $.feature_intro.next)}
              disabled={index === last}
              onClick={() => go(index + 1)}
            >
              <ArrowRight aria-hidden="true" />
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

// Illustration pieces. Slides compose these with real product atoms
// (avatars, status icons) so the art reads as the product, not a drawing.

export function IntroArtCard({
  className,
  highlighted = false,
  children,
}: {
  className?: string;
  highlighted?: boolean;
  children: ReactNode;
}) {
  return (
    <div
      className={cn(
        "w-full rounded-lg bg-surface p-2.5 text-caption shadow-[var(--menu-shadow)] ring-1",
        highlighted ? "ring-brand" : "ring-surface-border",
        className,
      )}
    >
      {children}
    </div>
  );
}

const WIRE = "border-dashed border-brand/50";

function IntroArtArrowhead({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        "block size-0 border-x-[4px] border-t-[5px] border-x-transparent border-t-brand/60",
        className,
      )}
    />
  );
}

/** A dashed arrow pointing down to the next card, with an optional label beside it. */
export function IntroArtConnector({ label }: { label?: ReactNode }) {
  return (
    <div className="relative flex h-6 shrink-0 flex-col items-center">
      <span className={cn("w-0 flex-1 border-l-[1.5px]", WIRE)} />
      <IntroArtArrowhead />
      {label && (
        <span className="absolute top-1/2 left-1/2 ml-2.5 -translate-y-1/2 rounded-sm bg-brand/10 px-1.5 py-0.5 text-micro font-medium whitespace-nowrap text-brand">
          {label}
        </span>
      )}
    </div>
  );
}

/** One card handing off to two side-by-side cards below it. */
export function IntroArtFork() {
  return (
    <div className="relative h-6 shrink-0">
      <span className={cn("absolute top-0 left-1/2 h-2.5 border-l-[1.5px]", WIRE)} />
      <span
        className={cn(
          "absolute top-2.5 right-1/4 bottom-[5px] left-1/4 rounded-t-md border-x-[1.5px] border-t-[1.5px]",
          WIRE,
        )}
      />
      <IntroArtArrowhead className="absolute bottom-0 left-1/4 -translate-x-[3.25px]" />
      <IntroArtArrowhead className="absolute right-1/4 bottom-0 translate-x-[3.25px]" />
    </div>
  );
}

export function IntroArtTag({
  className,
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-sm bg-muted px-1.5 py-0.5 text-micro text-muted-foreground",
        className,
      )}
    >
      {children}
    </span>
  );
}
