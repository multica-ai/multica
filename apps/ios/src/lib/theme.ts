import type { IssueStatus, IssuePriority } from "@multica/core/types";

/**
 * Color tokens for the iOS app.
 * Light mode values derived from packages/ui/styles/tokens.css.
 * oklch values converted to approximate hex.
 */
export const colors = {
  // Base
  background: "#FFFFFF",
  foreground: "#09090B",
  card: "#FFFFFF",
  cardForeground: "#09090B",

  // Primary
  primary: "#18181B",
  primaryForeground: "#FAFAFA",

  // Secondary / Muted
  secondary: "#F4F4F5",
  secondaryForeground: "#18181B",
  muted: "#F4F4F5",
  mutedForeground: "#71717A",

  // Accent
  accent: "#F4F4F5",
  accentForeground: "#18181B",

  // Semantic
  destructive: "#DC2626",
  success: "#16A34A",
  warning: "#D97706",
  info: "#2563EB",
  brand: "#4F46E5",
  priority: "#EA580C",

  // Border
  border: "#E4E4E7",
  input: "#E4E4E7",

  // Transparent helpers
  transparent: "transparent",
  white: "#FFFFFF",
  black: "#000000",
} as const;

export const darkColors = {
  background: "#18181B",
  foreground: "#FAFAFA",
  card: "#1E1E21",
  cardForeground: "#FAFAFA",

  primary: "#E4E4E7",
  primaryForeground: "#18181B",

  secondary: "#27272A",
  secondaryForeground: "#FAFAFA",
  muted: "#27272A",
  mutedForeground: "#A1A1AA",

  accent: "#27272A",
  accentForeground: "#FAFAFA",

  destructive: "#F87171",
  success: "#4ADE80",
  warning: "#FBBF24",
  info: "#60A5FA",
  brand: "#818CF8",
  priority: "#FB923C",

  border: "rgba(255,255,255,0.1)",
  input: "rgba(255,255,255,0.15)",

  transparent: "transparent",
  white: "#FFFFFF",
  black: "#000000",
} as const;

/** Map IssueStatus to a color value */
export const statusColors: Record<IssueStatus, string> = {
  backlog: colors.mutedForeground,
  todo: colors.mutedForeground,
  in_progress: colors.warning,
  in_review: colors.success,
  done: colors.info,
  blocked: colors.destructive,
  cancelled: colors.mutedForeground,
};

/** Map IssuePriority to a color value */
export const priorityColors: Record<IssuePriority, string> = {
  urgent: colors.destructive,
  high: colors.warning,
  medium: colors.warning,
  low: colors.info,
  none: colors.mutedForeground,
};
