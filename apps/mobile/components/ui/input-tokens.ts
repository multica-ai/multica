/**
 * Shared design tokens for text-input surfaces. Mobile-only — these don't
 * mirror the web `<Input>` token set because mobile has different concerns
 * (no hover variants, multiple input scales for hero / body / chat / search).
 */

/** Placeholder text colour. Matches the `muted-foreground` token value
 *  (#71717a is the strict shadcn value, but mobile uses a slightly lighter
 *  #a1a1aa across all existing inputs — match the live UI, not the token). */
export const MOBILE_PLACEHOLDER_COLOR = "#a1a1aa";

/** Default minimum height for a multiline body input (issue description,
 *  agent prompt). Comment composer keeps its own tighter min-h-8 because
 *  it's a chat-style row, not a body block. */
export const MIN_BODY_INPUT_HEIGHT_PX = 120;
