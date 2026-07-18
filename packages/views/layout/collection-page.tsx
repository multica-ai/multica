"use client";

import type { ComponentProps, ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { cn } from "@multica/ui/lib/utils";
import { PageHeader } from "./page-header";

interface CollectionPageHeaderProps {
  icon: LucideIcon;
  title: ReactNode;
  count?: number;
  description?: ReactNode;
  learnMore?: {
    href: string;
    label: ReactNode;
  };
  actions?: ReactNode;
  className?: string;
}

/**
 * Shared dashboard collection header: entity icon, title, optional count and
 * supporting copy on the left; page-level actions on the right.
 */
export function CollectionPageHeader({
  icon: Icon,
  title,
  count,
  description,
  learnMore,
  actions,
  className,
}: CollectionPageHeaderProps) {
  return (
    <PageHeader className={cn("justify-between gap-3 px-5", className)}>
      <div className="flex min-w-0 items-center gap-2">
        <Icon
          aria-hidden="true"
          className="size-4 shrink-0 text-muted-foreground"
        />
        <h1 className="truncate text-sm font-medium">{title}</h1>
        {typeof count === "number" && count > 0 ? (
          <span className="shrink-0 font-mono text-xs tabular-nums text-muted-foreground/70">
            {count}
          </span>
        ) : null}
        {description ? (
          <p className="ml-2 hidden min-w-0 truncate text-xs text-muted-foreground md:block">
            {description}
            {learnMore ? (
              <>
                {" "}
                <a
                  href={learnMore.href}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline decoration-muted-foreground/30 underline-offset-4 transition-colors hover:text-foreground"
                >
                  {learnMore.label}
                </a>
              </>
            ) : null}
          </p>
        ) : null}
      </div>
      {actions ? (
        <div className="flex shrink-0 items-center justify-end gap-2">
          {actions}
        </div>
      ) : null}
    </PageHeader>
  );
}

interface CollectionPageHeaderActionProps
  extends Omit<ComponentProps<typeof Button>, "children"> {
  icon: LucideIcon;
  label: string;
}

/** Responsive collection action: icon-only below md, labelled above md. */
export function CollectionPageHeaderAction({
  icon: Icon,
  label,
  className,
  type = "button",
  size = "sm",
  variant = "outline",
  ...props
}: CollectionPageHeaderActionProps) {
  const accessibleLabel = props["aria-label"] ?? label;

  return (
    <Button
      type={type}
      size={size}
      variant={variant}
      className={cn("h-8 w-8 gap-1 px-0 md:w-auto md:px-2.5", className)}
      aria-label={accessibleLabel}
      {...props}
    >
      <Icon aria-hidden="true" className="size-3.5" />
      <span className="hidden md:inline">{label}</span>
    </Button>
  );
}

type PageStateTone = "muted" | "destructive" | "warning";

interface CollectionPageStateProps {
  icon: LucideIcon;
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  tone?: PageStateTone;
  role?: "alert" | "status";
  className?: string;
}

const stateToneClass: Record<PageStateTone, string> = {
  muted: "text-muted-foreground",
  destructive: "text-destructive",
  warning: "text-warning",
};

/** Shared centered state for collection empty, error and not-found views. */
export function CollectionPageState({
  icon: Icon,
  title,
  description,
  actions,
  tone = "muted",
  role,
  className,
}: CollectionPageStateProps) {
  return (
    <Empty
      role={role}
      className={cn("rounded-none border-0 px-6 py-16", className)}
    >
      <EmptyHeader>
        <EmptyMedia
          variant="icon"
          className={cn(
            "size-12 rounded-full [&_svg]:size-6",
            stateToneClass[tone],
          )}
        >
          <Icon aria-hidden="true" />
        </EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        {description ? (
          <EmptyDescription className="max-w-md">{description}</EmptyDescription>
        ) : null}
      </EmptyHeader>
      {actions ? (
        <EmptyContent className="mt-1 flex-row justify-center">
          {actions}
        </EmptyContent>
      ) : null}
    </Empty>
  );
}
