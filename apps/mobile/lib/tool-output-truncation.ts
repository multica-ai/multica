import type { TaskMessagePayload } from "@multica/core/types";

/**
 * Presentation rules for tool-result truncation, kept out of the component so
 * they can be tested without a React Native renderer.
 *
 * Mobile has no component-test harness, and the defects in this area have all
 * been wiring rather than logic — a correct helper that some surface never
 * called. Extracting the decisions here means the component holds only JSX, so
 * "does it render" and "is the rule right" stop being the same untested
 * question.
 *
 * Mirrors packages/views/common/task-transcript/output-truncation.tsx. Mobile
 * is English-only (see apps/mobile/lib/project-status.ts), so the copy is
 * literal here rather than routed through an i18n layer that does not exist.
 */

/** How much of a stored preview a collapsed row will paint. */
export const DISPLAY_CLIP_CHARS = 4000;

/**
 * Coarse byte size for a truncation label. The reader is judging "a little or
 * a lot", and exact digits add noise to that.
 */
export function formatByteSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/**
 * Badge text for a result whose stored output is only a preview, or null when
 * no badge belongs on the row.
 *
 * Checks `=== true` rather than truthiness: `undefined` means the reporting
 * daemon could not say, which is not the same as confirming the output is
 * complete. Rendering a badge there would assert something unknown; rendering
 * nothing is handled by the transcript-level notice instead.
 *
 * Says "source truncated" rather than "truncated" so it cannot be read as the
 * row being collapsed for layout — a separate thing, labelled separately.
 */
export function sourceTruncatedLabel(item: {
  output_truncated?: boolean;
  output_original_bytes?: number;
}): string | null {
  if (item.output_truncated !== true) return null;
  const size =
    typeof item.output_original_bytes === "number"
      ? formatByteSize(item.output_original_bytes)
      : "";
  return size ? `source truncated · ${size} total` : "source truncated";
}

/** Label for this surface clipping a long stored preview. */
export function displayClippedLabel(output: string): string | null {
  if (output.length <= DISPLAY_CLIP_CHARS) return null;
  return `Showing the first ${DISPLAY_CLIP_CHARS} characters of the stored preview.`;
}

/** The portion of a stored preview this surface will paint. */
export function clipForDisplay(output: string): string {
  return output.length > DISPLAY_CLIP_CHARS ? output.slice(0, DISPLAY_CLIP_CHARS) : output;
}

/**
 * True when any tool result in the timeline has no truncation state.
 *
 * `=== undefined` rather than a falsy check: `false` is a positive assertion
 * that the output is complete and must not be reported as unknown.
 */
export function hasUnknownTruncation(items: Pick<TaskMessagePayload, "type" | "output_truncated">[]): boolean {
  return items.some(
    (item) => item.type === "tool_result" && item.output_truncated === undefined,
  );
}

/** One-time notice shown when a timeline contains results of unknown state. */
export const TRUNCATION_UNKNOWN_NOTICE =
  "Some results here were recorded before truncation was tracked, so whether they are complete is unknown.";
