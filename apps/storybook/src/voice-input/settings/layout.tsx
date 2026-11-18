import type { ReactNode } from "react";
import { Bell, Keyboard, KeyRound, Laptop, Mic, Palette, Search, SlidersHorizontal, User } from "@multica/ui/icons";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { cn } from "@multica/ui/lib/utils";
import type { SettingsCopy } from "./copy";

/*
 * Copies of the settings building blocks in
 * packages/views/settings/components/settings-layout.tsx and the page chrome in
 * settings-page.tsx. They read the app's stores there, so the mock carries the
 * markup and classes only.
 */

export function ScopeBadge({ label }: { label: string }) {
  return (
    <span className="inline-flex h-5 min-w-0 shrink-0 items-center gap-1 rounded-full bg-muted px-2 text-caption font-medium text-muted-foreground">
      <Laptop aria-hidden="true" className="size-3" />
      <span className="truncate">{label}</span>
    </span>
  );
}

export function SettingsSection({ title, description, children }: { title?: ReactNode; description?: ReactNode; children: ReactNode }) {
  return (
    <section className="space-y-3">
      {(title || description) && (
        <div className="px-0.5">
          {title && <h3 className="text-body font-semibold">{title}</h3>}
          {description && <p className="mt-1 text-caption leading-5 text-muted-foreground">{description}</p>}
        </div>
      )}
      {children}
    </section>
  );
}

export function SettingsCard({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <Card className={cn("gap-0 py-0 shadow-none", className)}>
      <CardContent className="divide-y divide-surface-border px-0">{children}</CardContent>
    </Card>
  );
}

const WIDTHS = { text: "sm:w-96", "select-wide": "sm:w-72", select: "sm:w-48", none: "sm:max-w-none" } as const;

export function SettingsRow({
  label,
  description,
  children,
  size,
  align = "center",
  className,
}: {
  label: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  size?: keyof typeof WIDTHS;
  align?: "center" | "start";
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex min-h-16 gap-4 px-4 py-3.5 sm:justify-between sm:gap-8",
        size ? "flex-col sm:flex-row" : "flex-row justify-between",
        align === "center" ? (size ? "sm:items-center" : "items-center") : size ? "sm:items-start" : "items-start",
        className,
      )}
    >
      <div className="min-w-0 flex-1">
        <div className="text-body font-medium">{label}</div>
        {description && <div className="mt-0.5 text-caption leading-5 text-muted-foreground">{description}</div>}
      </div>
      {children !== undefined && (
        <div className={cn("shrink-0 sm:w-auto sm:max-w-[56%]", size ? "w-full" : "w-auto max-w-[56%]", size && WIDTHS[size])}>
          {children}
        </div>
      )}
    </div>
  );
}

/** packages/views/common/segmented-toggle.tsx */
export function SegmentedToggle<T extends string>({
  value,
  options,
  onChange,
  disabled = [],
}: {
  value: T;
  options: ReadonlyArray<readonly [T, ReactNode]>;
  onChange: (value: T) => void;
  disabled?: T[];
}) {
  return (
    <div className="grid auto-cols-fr grid-flow-col gap-1 rounded-md bg-muted p-1">
      {options.map(([key, label]) => (
        <button
          key={key}
          type="button"
          aria-pressed={value === key}
          disabled={disabled.includes(key)}
          onClick={() => key !== value && onChange(key)}
          className={cn(
            "whitespace-nowrap rounded-sm px-2 py-1 text-caption font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40",
            value === key ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
          )}
        >
          {label}
        </button>
      ))}
    </div>
  );
}

/** The settings page: navigation on the left, the open tab on the right. */
export function SettingsShell({ copy, title, scope, children }: { copy: SettingsCopy; title: string; scope: string; children: ReactNode }) {
  const nav = [
    { icon: User, label: copy.nav.profile },
    { icon: SlidersHorizontal, label: copy.nav.preferences },
    { icon: Palette, label: copy.nav.appearance },
    { icon: Bell, label: copy.nav.notifications },
    { icon: Keyboard, label: copy.nav.shortcuts },
    { icon: Mic, label: copy.nav.voice, active: true },
    { icon: KeyRound, label: copy.nav.tokens },
  ];
  return (
    <div className="flex min-h-screen bg-background">
      <nav className="w-60 shrink-0 border-r border-border/60 px-2 pt-4">
        <div className="relative mx-0 mb-3">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <div className="flex h-8 w-full items-center rounded-lg border border-input pl-[37px] text-body text-muted-foreground">
            {copy.nav.search}
          </div>
        </div>
        <div className="flex h-7 items-center px-3 text-caption font-medium text-muted-foreground">{copy.nav.personal}</div>
        <ul className="space-y-px">
          {nav.map((item) => (
            <li key={item.label}>
              <span
                className={cn(
                  "flex min-h-8 items-center gap-2.5 rounded-lg px-3 py-1.5 text-body",
                  item.active ? "bg-surface-selected text-surface-selected-foreground" : "text-muted-foreground",
                )}
              >
                <item.icon className="size-4 shrink-0" />
                <span className="truncate">{item.label}</span>
              </span>
            </li>
          ))}
        </ul>
      </nav>
      <main className="min-w-0 flex-1">
        <div className="flex h-12 items-center gap-2 border-b border-border/60 px-10">
          <h2 className="flex items-center gap-2 text-body font-medium">
            <Mic className="size-4 text-muted-foreground" />
            {title}
          </h2>
          <ScopeBadge label={scope} />
        </div>
        <div className="mx-auto w-full max-w-[56rem] px-10 py-8">{children}</div>
      </main>
    </div>
  );
}
