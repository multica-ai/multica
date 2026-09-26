"use client";

import { useId, type ReactNode } from "react";
import {
  AlertCircle,
  Building2,
  Check,
  Eye,
  Laptop,
  Loader2,
  UserRound,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { cn } from "@multica/ui/lib/utils";
import { useCurrentWorkspace } from "@multica/core/paths";
import { memberListOptions } from "@multica/core/workspace/queries";
import { WorkspaceAvatar } from "../../workspace/workspace-avatar";
import { useT } from "../../i18n";

export type SettingsSaveStatus = "idle" | "saving" | "saved" | "error";

/**
 * Where a setting takes effect. Settings mix three storage scopes — the
 * account, the current workspace, and this device — and the page chrome says
 * which one applies so nobody has to guess whether a change follows them.
 */
export type SettingsScope =
  | "account"
  | "device"
  | "device-workspace"
  | "workspace"
  | "workspace-only";

/** Anchor attribute search results and legacy `?section=` links scroll to. */
export const SETTINGS_ANCHOR_ATTR = "data-settings-anchor";

const SCOPE_BADGE_CLASS =
  "inline-flex h-5 min-w-0 shrink-0 items-center gap-1 rounded-full bg-muted px-2 text-caption font-medium text-muted-foreground";

export function SettingsScopeBadge({ scope }: { scope: SettingsScope }) {
  const { t } = useT("settings");
  // Only the workspace-bound scopes read the workspace, so account and device
  // pages render without the workspace query.
  if (scope === "workspace" || scope === "workspace-only" || scope === "device-workspace") {
    return <WorkspaceScopeBadge scope={scope} />;
  }
  const Icon = scope === "account" ? UserRound : Laptop;
  return (
    <span data-slot="settings-scope" className={SCOPE_BADGE_CLASS}>
      <Icon aria-hidden="true" className="size-3" />
      <span className="truncate">
        {scope === "account"
          ? t(($) => $.layout.scope.account)
          : t(($) => $.layout.scope.device)}
      </span>
    </span>
  );
}

function WorkspaceScopeBadge({
  scope,
}: {
  scope: "workspace" | "workspace-only" | "device-workspace";
}) {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const name = workspace?.name ?? t(($) => $.page.workspace_fallback);
  // A letter avatar is illegible at chip size, so only a real logo is shown.
  const mark =
    scope === "device-workspace" ? (
      <Laptop aria-hidden="true" className="size-3" />
    ) : workspace?.avatar_url ? (
      <WorkspaceAvatar
        name={name}
        avatarUrl={workspace.avatar_url}
        className="size-3 rounded-xs border-0"
      />
    ) : (
      <Building2 aria-hidden="true" className="size-3" />
    );
  const label =
    scope === "workspace"
      ? t(($) => $.layout.scope.workspace, { name })
      : scope === "workspace-only"
        ? t(($) => $.layout.scope.workspace_only, { name })
        : t(($) => $.layout.scope.device_workspace, { name });
  return (
    <span data-slot="settings-scope" className={SCOPE_BADGE_CLASS}>
      {mark}
      <span className="truncate">{label}</span>
    </span>
  );
}

export function SettingsTab({
  title,
  description,
  scope,
  actions,
  breadcrumb,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  scope?: SettingsScope;
  /** Primary page-level action, aligned with the title. */
  actions?: ReactNode;
  /** Parent location for detail pages; rendered above the title. */
  breadcrumb?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="space-y-8">
      <header>
        {breadcrumb ? <div className="mb-2">{breadcrumb}</div> : null}
        <div className="flex min-w-0 items-start gap-4">
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1">
              <h2 className="text-title-lg font-semibold tracking-tight">{title}</h2>
              {scope ? <SettingsScopeBadge scope={scope} /> : null}
            </div>
            {description ? (
              <p className="mt-1 max-w-2xl text-body leading-6 text-muted-foreground">
                {description}
              </p>
            ) : null}
          </div>
          {actions ? <div className="shrink-0">{actions}</div> : null}
        </div>
      </header>
      {children}
    </div>
  );
}

export function SettingsSection({
  title,
  description,
  scope,
  action,
  anchor,
  children,
  className,
}: {
  title?: ReactNode;
  description?: ReactNode;
  scope?: SettingsScope;
  action?: ReactNode;
  /** Target for search results and deep links (`?section=<anchor>`). */
  anchor?: string;
  children: ReactNode;
  className?: string;
}) {
  const headingId = useId();
  return (
    <section
      className={cn("scroll-mt-6 space-y-3", className)}
      aria-labelledby={title ? headingId : undefined}
      {...(anchor ? { [SETTINGS_ANCHOR_ATTR]: anchor } : {})}
    >
      {title || description || action ? (
        <div className="flex min-w-0 items-end justify-between gap-4 px-0.5">
          <div className="min-w-0">
            {title ? (
              <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                <h3 id={headingId} className="text-body font-semibold">
                  {title}
                </h3>
                {scope ? <SettingsScopeBadge scope={scope} /> : null}
              </div>
            ) : null}
            {description ? (
              <p className="mt-1 text-caption leading-5 text-muted-foreground">
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
  anchor,
}: {
  label: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  className?: string;
  /** Control column width tier; omit for content-hugging controls (buttons, switches). */
  size?: SettingsControlSize;
  align?: "center" | "start";
  /** Target for search results and deep links (`?section=<anchor>`). */
  anchor?: string;
}) {
  return (
    <div
      {...(anchor ? { [SETTINGS_ANCHOR_ATTR]: anchor } : {})}
      className={cn(
        "flex min-h-16 scroll-mt-6 gap-4 px-4 py-3.5 transition-colors duration-700 data-[flash=true]:bg-surface-selected sm:justify-between sm:gap-8",
        size ? "flex-col sm:flex-row" : "flex-row justify-between",
        align === "center"
          ? size
            ? "sm:items-center"
            : "items-center"
          : size
            ? "sm:items-start"
            : "items-start",
        className,
      )}
    >
      <div className="min-w-0 flex-1">
        <div className="text-body font-medium">{label}</div>
        {description ? (
          <div className="mt-0.5 text-caption leading-5 text-muted-foreground">
            {description}
          </div>
        ) : null}
      </div>
      <div
        className={cn(
          "shrink-0 sm:w-auto sm:max-w-[56%]",
          size ? "w-full" : "w-auto max-w-[56%]",
          size ? SETTINGS_CONTROL_WIDTHS[size] : undefined,
        )}
      >
        {children}
      </div>
    </div>
  );
}

/**
 * The one read-only explanation for workspace pages a member can see but not
 * change. It names who can make the change so the next step is obvious.
 */
export function SettingsReadOnlyNotice({ wsId }: { wsId: string }) {
  const { t } = useT("settings");
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId,
  });
  const managers = members
    .filter((member) => member.role === "owner" || member.role === "admin")
    .map((member) => member.name)
    .filter(Boolean)
    .slice(0, 3);
  return (
    <div
      role="note"
      className="flex items-start gap-3 rounded-xl border border-surface-border bg-surface px-4 py-3 text-body"
    >
      <Eye aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
      <div className="min-w-0">
        <p className="font-medium">{t(($) => $.layout.read_only.title)}</p>
        <p className="mt-0.5 text-caption leading-5 text-muted-foreground">
          {managers.length > 0
            ? t(($) => $.layout.read_only.contact, {
                names: managers.join(t(($) => $.layout.read_only.separator)),
              })
            : t(($) => $.layout.read_only.description)}
        </p>
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
        "inline-flex items-center gap-1.5 text-caption text-muted-foreground",
        status === "error" && "text-destructive",
      )}
    >
      {content}
    </span>
  );
}
