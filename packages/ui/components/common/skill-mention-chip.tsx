"use client";

import { BookOpenText } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";

/**
 * SkillMentionChip — the inline pill for skill mentions in rich-text.
 *
 * Follows the `ActorMentionChip` anatomy: `rounded-full` pill with `text-xs`
 * typography, icon + label, caller-controlled focusability. Skills are
 * documents/instructions, not actors — the violet/purple tint and
 * `BookOpenText` icon reflect that visual language.
 *
 * Color note: violet is used here as an entity-specific tint that distinguishes
 * skills from members/agents/issues in the mention surface. There is no
 * `skill` semantic token in `packages/ui/styles/tokens.css` today; the design
 * tokens were authored without entity-tinted chips. If a broader refactor
 * introduces entity-specific tokens, this should use them. Until then the
 * hardcoded palette is intentional and visible-by-design, not an oversight.
 *
 * This is a pure presentational component with zero business logic.
 * It lives in `packages/ui/` and must NOT import from `@multica/core`.
 */
export interface SkillMentionChipProps {
  /** The skill's display name (e.g. "code-review"). */
  name: string;
  /** Optional description shown as a title tooltip on hover. */
  description?: string;
  /** Extra classes for caller-specific overrides. */
  className?: string;
  /** When true the chip is keyboard-focusable with a focus-visible ring
   *  (editor use). Readonly consumers leave it false (R14). */
  focusable?: boolean;
}

const BASE_CLASS =
  "skill-mention-chip inline-flex align-middle min-w-0 items-center gap-1 rounded-full border px-1.5 py-0.5 text-xs font-medium bg-violet-100 text-violet-700 border-violet-200 hover:bg-violet-200 transition-colors dark:bg-violet-900/30 dark:text-violet-300 dark:border-violet-700 dark:hover:bg-violet-900/40";

const ICON_SIZE = 12;

export function SkillMentionChip({
  name,
  description,
  className,
  focusable = false,
}: SkillMentionChipProps) {
  return (
    <span
      className={cn(
        BASE_CLASS,
        focusable &&
          "focus-visible:outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50",
        className,
      )}
      tabIndex={focusable ? 0 : undefined}
      aria-label={`Skill: ${name}`}
      title={description}
    >
      <BookOpenText
        aria-hidden
        style={{ width: ICON_SIZE, height: ICON_SIZE }}
        className="shrink-0"
      />
      <span data-slot="label" className="min-w-0 shrink truncate max-w-[8rem]">
        {name}
      </span>
    </span>
  );
}
