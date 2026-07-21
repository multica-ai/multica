import type { IssueStatus, StatusColor, StatusDetail } from "../../types";

export const STATUS_ORDER: IssueStatus[] = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
  "cancelled",
];

export const ALL_STATUSES: IssueStatus[] = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
  "cancelled",
];

export const STATUS_CONFIG: Record<
  IssueStatus,
  {
    label: string;
    iconColor: string;
    hoverBg: string;
    dividerColor: string;
    columnBg: string;
  }
> = {
  backlog: { label: "Backlog", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
  todo: { label: "Todo", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
  in_progress: { label: "In Progress", iconColor: "text-warning", hoverBg: "hover:bg-warning/10", dividerColor: "bg-warning", columnBg: "bg-warning/5" },
  in_review: { label: "In Review", iconColor: "text-success", hoverBg: "hover:bg-success/10", dividerColor: "bg-success", columnBg: "bg-success/5" },
  done: { label: "Done", iconColor: "text-info", hoverBg: "hover:bg-info/10", dividerColor: "bg-info", columnBg: "bg-info/5" },
  blocked: { label: "Blocked", iconColor: "text-destructive", hoverBg: "hover:bg-destructive/10", dividerColor: "bg-destructive", columnBg: "bg-destructive/5" },
  cancelled: { label: "Cancelled", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
};

export interface StatusTheme {
  iconColor: string;
  hoverBg: string;
  dividerColor: string;
  columnBg: string;
}

/**
 * Theme classes keyed by the semantic COLOR token a catalog status carries
 * (MUL-4809). STATUS_CONFIG above is keyed by the 7 legacy status tokens, which
 * cannot express a custom status; this map is the color-driven equivalent and is
 * what every custom-status-aware surface should use.
 *
 * The class strings are written out in full on purpose: Tailwind scans source
 * statically, so a template-built class name (`text-${color}`) would be purged.
 */
export const STATUS_COLOR_CONFIG: Record<StatusColor, StatusTheme> = {
  "muted-foreground": { iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
  warning: { iconColor: "text-warning", hoverBg: "hover:bg-warning/10", dividerColor: "bg-warning", columnBg: "bg-warning/5" },
  success: { iconColor: "text-success", hoverBg: "hover:bg-success/10", dividerColor: "bg-success", columnBg: "bg-success/5" },
  info: { iconColor: "text-info", hoverBg: "hover:bg-info/10", dividerColor: "bg-info", columnBg: "bg-info/5" },
  destructive: { iconColor: "text-destructive", hoverBg: "hover:bg-destructive/10", dividerColor: "bg-destructive", columnBg: "bg-destructive/5" },
};

const FALLBACK_STATUS_THEME: StatusTheme = STATUS_COLOR_CONFIG["muted-foreground"];

/**
 * Theme classes for a color token. An unknown token (a newer server shipping a
 * color this client predates) degrades to the neutral theme rather than
 * rendering unstyled — the same tolerance the schemas apply.
 */
export function statusThemeForColor(color: string | null | undefined): StatusTheme {
  if (!color) return FALLBACK_STATUS_THEME;
  return STATUS_COLOR_CONFIG[color as StatusColor] ?? FALLBACK_STATUS_THEME;
}

/**
 * Theme classes for an issue's status. Prefers the resolved catalog entry
 * (`status_detail`, which is what a custom status arrives as) and falls back to
 * the legacy token config when the issue has no `status_id` yet or the server
 * predates the catalog.
 */
export function statusTheme(
  statusDetail: StatusDetail | null | undefined,
  fallbackStatus?: IssueStatus | string | null,
): StatusTheme {
  if (statusDetail?.color) return statusThemeForColor(statusDetail.color);
  const legacy = fallbackStatus ? STATUS_CONFIG[fallbackStatus as IssueStatus] : undefined;
  return legacy ?? FALLBACK_STATUS_THEME;
}
