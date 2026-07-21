import type { ReactNode } from "react";
import { AlertCircle, Check, Loader2 } from "lucide-react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { cn } from "@multica/ui/lib/utils";

export type SettingsSaveStatus = "idle" | "saving" | "saved" | "error";

export function SettingsTab({
  title,
  description,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="space-y-8">
      <header>
        <h2 className="text-xl font-semibold tracking-tight">{title}</h2>
        {description ? (
          <p className="mt-1 max-w-2xl text-sm leading-6 text-muted-foreground">
            {description}
          </p>
        ) : null}
      </header>
      {children}
    </div>
  );
}

export function SettingsSection({
  title,
  description,
  action,
  children,
  className,
}: {
  title?: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("space-y-3", className)}>
      {title || description || action ? (
        <div className="flex min-w-0 items-end justify-between gap-4 px-0.5">
          <div className="min-w-0">
            {title ? <h3 className="text-sm font-semibold">{title}</h3> : null}
            {description ? (
              <p className="mt-1 text-xs leading-5 text-muted-foreground">
                {description}
              </p>
            ) : null}
          </div>
          {action ? <div className="shrink-0">{action}</div> : null}
        </div>
      ) : null}
      {children}
    </section>
  );
}

export function SettingsCard({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <Card className={cn("gap-0 py-0 shadow-none", className)}>
      <CardContent className="divide-y divide-surface-border px-0">
        {children}
      </CardContent>
    </Card>
  );
}

/**
 * Width tiers for the control column. Within a card, every text-entry
 * control shares the `text` tier so their edges align; a row may only
 * drop to a smaller tier when the field is deliberately short (a code,
 * an enum select) — the difference must read as intentional. Pick a
 * tier instead of adding per-row ad-hoc widths.
 */
const SETTINGS_CONTROL_WIDTHS = {
  /** Text inputs and textareas — the standard control column. */
  text: "sm:w-96",
  /** Selects/pickers with long option labels (timezone, model). */
  "select-wide": "sm:w-72",
  /** Compact enum selects (theme, language). */
  select: "sm:w-48",
  /** Short fixed-format codes (issue prefix). */
  code: "sm:w-40",
  /** Unconstrained — non-input content like avatar uploads. */
  none: "sm:max-w-none",
} as const;

export type SettingsControlSize = keyof typeof SETTINGS_CONTROL_WIDTHS;

export function SettingsRow({
  label,
  description,
  children,
  className,
  size,
  align = "center",
}: {
  label: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  className?: string;
  /** Control column width tier; omit for content-hugging controls (buttons, switches). */
  size?: SettingsControlSize;
  align?: "center" | "start";
}) {
  return (
    <div
      className={cn(
        "flex min-h-16 flex-col gap-3 px-4 py-3.5 sm:flex-row sm:justify-between sm:gap-8",
        align === "center" ? "sm:items-center" : "sm:items-start",
        className,
      )}
    >
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">{label}</div>
        {description ? (
          <div className="mt-0.5 text-xs leading-5 text-muted-foreground">
            {description}
          </div>
        ) : null}
      </div>
      <div
        className={cn(
          "w-full shrink-0 sm:w-auto sm:max-w-[56%]",
          size ? SETTINGS_CONTROL_WIDTHS[size] : undefined,
        )}
      >
        {children}
      </div>
    </div>
  );
}

export function SettingsSaveState({
  status,
  savingLabel,
  savedLabel,
  errorLabel,
}: {
  status: SettingsSaveStatus;
  savingLabel: string;
  savedLabel: string;
  errorLabel: string;
}) {
  if (status === "idle") return null;

  const content =
    status === "saving" ? (
      <>
        <Loader2 className="size-3 animate-spin" />
        {savingLabel}
      </>
    ) : status === "saved" ? (
      <>
        <Check className="size-3 text-success" />
        {savedLabel}
      </>
    ) : (
      <>
        <AlertCircle className="size-3 text-destructive" />
        {errorLabel}
      </>
    );

  return (
    <span
      role="status"
      className={cn(
        "inline-flex items-center gap-1.5 text-xs text-muted-foreground",
        status === "error" && "text-destructive",
      )}
    >
      {content}
    </span>
  );
}
