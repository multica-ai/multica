import { useT } from "../../i18n/use-t";
import { cn } from "@multica/ui/lib/utils";

/**
 * Human-readable byte count for truncation notices.
 *
 * Deliberately coarse: the reader is judging "did I lose a little or a lot",
 * and an exact digit count adds noise to that decision.
 */
export function formatByteSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/**
 * Marks a tool result whose stored output is only a preview of what the agent
 * actually produced.
 *
 * This is about the DATA, not about how much of it is currently on screen.
 * Surfaces also clip long text for layout reasons, and conflating the two would
 * tell a reader their output was lost when it is merely collapsed. This badge
 * renders outside the body, next to the label, so the distinction is visible
 * even when the body is scrolled or hidden.
 *
 * Renders nothing unless `truncated === true`. `undefined` — an older daemon,
 * or a row written before the field existed — is not evidence of completeness,
 * but it is also not evidence of loss, so a per-row badge would be a guess;
 * `TruncationUnknownNotice` states it once for the whole transcript instead.
 */
export function OutputTruncatedBadge({
  truncated,
  originalBytes,
  className,
}: {
  truncated?: boolean;
  originalBytes?: number;
  className?: string;
}) {
  const { t } = useT("agents");
  if (truncated !== true) return null;

  const size = typeof originalBytes === "number" ? formatByteSize(originalBytes) : "";
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center rounded border border-warning/40 bg-warning/10 px-1.5 py-0.5 text-micro font-medium text-warning-foreground",
        className,
      )}
      title={t(($) => $.transcript.output_truncated_hint)}
    >
      {size
        ? t(($) => $.transcript.output_truncated_badge, { size })
        : t(($) => $.transcript.patch_truncated)}
    </span>
  );
}

/**
 * One-time notice that some results in this transcript predate truncation
 * tracking.
 *
 * Shown once per transcript rather than per row. Repeating "state unknown" on
 * every historical message would bury the rows that are genuinely truncated,
 * and the fact is a property of when the transcript was recorded, not of any
 * individual result.
 */
export function TruncationUnknownNotice({ show }: { show: boolean }) {
  const { t } = useT("agents");
  if (!show) return null;
  return (
    <p className="px-3 py-1.5 text-micro text-faint-foreground">
      {t(($) => $.transcript.output_truncation_unknown)}
    </p>
  );
}

/**
 * True when any tool result in the transcript has no truncation state.
 *
 * `=== undefined` rather than a falsy check: `false` is a positive assertion
 * that the output is complete and must not be reported as unknown.
 */
export function hasUnknownTruncation(
  items: Array<{ type: string; output_truncated?: boolean }>,
): boolean {
  return items.some(
    (item) => item.type === "tool_result" && item.output_truncated === undefined,
  );
}
