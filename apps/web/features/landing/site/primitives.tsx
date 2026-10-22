import type { ComponentProps, CSSProperties, ReactNode } from "react";
import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";

export const focusRing =
  "outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-site-accent";

export function Container({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      className={cn("mx-auto w-full max-w-[1240px] px-5 sm:px-8", className)}
      {...props}
    />
  );
}

export function Section({ className, ...props }: ComponentProps<"section">) {
  return <section className={cn("py-20 sm:py-28", className)} {...props} />;
}

export function SectionHeader({
  eyebrow,
  title,
  lead,
  className,
}: {
  eyebrow?: string;
  title: ReactNode;
  lead?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("max-w-[44rem]", className)}>
      {eyebrow ? <Eyebrow>{eyebrow}</Eyebrow> : null}
      <h2 className={cn("font-site-display text-site-section text-site-ink text-balance", eyebrow && "mt-3")}>
        {title}
      </h2>
      {lead ? (
        <p className="mt-4 text-site-lead text-site-muted text-pretty">{lead}</p>
      ) : null}
    </div>
  );
}

export function Eyebrow({ className, ...props }: ComponentProps<"p">) {
  return (
    <p
      className={cn("text-site-eyebrow font-medium text-site-muted", className)}
      {...props}
    />
  );
}

const actionTone = {
  primary: "bg-site-inverse text-site-inverse-ink hover:bg-site-ink-secondary",
  secondary:
    "border border-site-line-strong bg-site-surface text-site-ink hover:bg-site-hover",
  ghost: "text-site-ink hover:bg-site-hover",
} as const;

const actionSize = {
  md: "h-9 px-3.5 text-site-nav",
  lg: "h-12 px-5 text-site-body",
} as const;

export function actionClassName(
  tone: keyof typeof actionTone = "primary",
  size: keyof typeof actionSize = "md",
) {
  return cn(
    "inline-flex shrink-0 items-center justify-center gap-1.5 rounded-site-control font-medium whitespace-nowrap transition-colors",
    focusRing,
    actionTone[tone],
    actionSize[size],
  );
}

export function isExternalHref(href: string) {
  return /^https?:\/\//.test(href);
}

export function ActionLink({
  href,
  tone,
  size,
  className,
  children,
  ...props
}: Omit<ComponentProps<typeof Link>, "href"> & {
  href: string;
  tone?: keyof typeof actionTone;
  size?: keyof typeof actionSize;
}) {
  return (
    <Link
      href={href}
      className={cn(actionClassName(tone, size), className)}
      {...(isExternalHref(href) ? { target: "_blank", rel: "noreferrer" } : {})}
      {...props}
    >
      {children}
    </Link>
  );
}

/** Text link used in menus and the footer; external targets get an arrow. */
export function SiteTextLink({
  href,
  external,
  className,
  children,
  ...props
}: Omit<ComponentProps<typeof Link>, "href"> & { href: string; external?: boolean }) {
  return (
    <Link
      href={href}
      className={cn("inline-flex items-center gap-1 rounded-sm transition-colors", focusRing, className)}
      {...(external ? { target: "_blank", rel: "noreferrer" } : {})}
      {...props}
    >
      {children}
      {external ? (
        <ArrowUpRight className="size-3.5 text-site-muted" aria-hidden />
      ) : null}
    </Link>
  );
}

export type VisualTone = "sky" | "dusk" | "sage" | "stone";

/**
 * Frame for product imagery. Pass a screenshot or a token-built product mockup
 * as children; without children it renders a labelled placeholder so layouts
 * can be reviewed before the art exists.
 */
export function ProductFrame({
  tone = "sky",
  label,
  aspectRatio,
  className,
  children,
}: {
  tone?: VisualTone;
  label: string;
  aspectRatio?: CSSProperties["aspectRatio"];
  className?: string;
  children?: ReactNode;
}) {
  return (
    <div
      role={children ? undefined : "img"}
      aria-label={children ? undefined : label}
      data-tone={tone}
      className={cn("site-visual rounded-site-panel", className)}
      style={{ aspectRatio }}
    >
      {children ?? (
        <div className="absolute inset-[7%] flex items-end rounded-site-card border border-dashed border-site-line-strong bg-site-surface/60 p-4">
          <span className="rounded-md bg-site-surface px-2 py-1 text-site-eyebrow text-site-muted">
            {label}
          </span>
        </div>
      )}
    </div>
  );
}
