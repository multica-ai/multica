"use client";

import { X } from "lucide-react";
import type { IssueLabel, LabelColor } from "@multica/core/types";

/**
 * Tailwind utility classes per label color. Background + text + border are paired for
 * comfortable contrast in both light and dark modes; keep keys aligned with
 * LABEL_COLORS in @multica/core/types/issue.
 */
const COLOR_CLASSES: Record<LabelColor, string> = {
  slate:  "bg-slate-100 text-slate-700 border-slate-200 dark:bg-slate-800/60 dark:text-slate-200 dark:border-slate-700",
  gray:   "bg-gray-100 text-gray-700 border-gray-200 dark:bg-gray-800/60 dark:text-gray-200 dark:border-gray-700",
  red:    "bg-red-100 text-red-700 border-red-200 dark:bg-red-900/40 dark:text-red-200 dark:border-red-800",
  orange: "bg-orange-100 text-orange-700 border-orange-200 dark:bg-orange-900/40 dark:text-orange-200 dark:border-orange-800",
  amber:  "bg-amber-100 text-amber-800 border-amber-200 dark:bg-amber-900/40 dark:text-amber-100 dark:border-amber-800",
  green:  "bg-green-100 text-green-700 border-green-200 dark:bg-green-900/40 dark:text-green-200 dark:border-green-800",
  teal:   "bg-teal-100 text-teal-700 border-teal-200 dark:bg-teal-900/40 dark:text-teal-200 dark:border-teal-800",
  blue:   "bg-blue-100 text-blue-700 border-blue-200 dark:bg-blue-900/40 dark:text-blue-200 dark:border-blue-800",
  indigo: "bg-indigo-100 text-indigo-700 border-indigo-200 dark:bg-indigo-900/40 dark:text-indigo-200 dark:border-indigo-800",
  purple: "bg-purple-100 text-purple-700 border-purple-200 dark:bg-purple-900/40 dark:text-purple-200 dark:border-purple-800",
  pink:   "bg-pink-100 text-pink-700 border-pink-200 dark:bg-pink-900/40 dark:text-pink-200 dark:border-pink-800",
};

const DOT_CLASSES: Record<LabelColor, string> = {
  slate:  "bg-slate-500",
  gray:   "bg-gray-500",
  red:    "bg-red-500",
  orange: "bg-orange-500",
  amber:  "bg-amber-500",
  green:  "bg-green-500",
  teal:   "bg-teal-500",
  blue:   "bg-blue-500",
  indigo: "bg-indigo-500",
  purple: "bg-purple-500",
  pink:   "bg-pink-500",
};

/** Read-only chip used on board cards, list rows and filter dropdowns. */
export function LabelChip({
  label,
  size = "sm",
  className,
}: {
  label: Pick<IssueLabel, "name" | "color">;
  size?: "xs" | "sm";
  className?: string;
}) {
  const padding = size === "xs" ? "px-1.5 py-0.5 text-[10px]" : "px-2 py-0.5 text-xs";
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-full border ${padding} font-medium ${COLOR_CLASSES[label.color] ?? COLOR_CLASSES.gray} ${className ?? ""}`}
      title={label.name}
    >
      <span className="max-w-[140px] truncate">{label.name}</span>
    </span>
  );
}

/** Removable variant used inside the issue detail Labels row. */
export function RemovableLabelChip({
  label,
  onRemove,
  disabled,
}: {
  label: Pick<IssueLabel, "name" | "color">;
  onRemove: () => void;
  disabled?: boolean;
}) {
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium ${COLOR_CLASSES[label.color] ?? COLOR_CLASSES.gray}`}
    >
      <span className="max-w-[160px] truncate">{label.name}</span>
      {!disabled && (
        <button
          type="button"
          onClick={onRemove}
          aria-label={`Remove ${label.name}`}
          className="ml-0.5 rounded-full opacity-70 hover:opacity-100 hover:bg-black/10 dark:hover:bg-white/10"
        >
          <X className="h-3 w-3" />
        </button>
      )}
    </span>
  );
}

/** Small color swatch used in pickers and the settings page. */
export function LabelColorDot({ color, className }: { color: LabelColor; className?: string }) {
  return (
    <span className={`inline-block h-2.5 w-2.5 shrink-0 rounded-full ${DOT_CLASSES[color] ?? DOT_CLASSES.gray} ${className ?? ""}`} />
  );
}
