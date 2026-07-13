"use client";

import type { SkillFrontmatter } from "@multica/core/skills/frontmatter";

/**
 * FrontmatterCard — renders a skill's YAML frontmatter as a key/value panel.
 *
 * Used by both the full skill file viewer (where it sits above the markdown
 * body, since there it's the natural "metadata header") and the inline
 * SkillProfileCard hover card (where it shows in compact form when the
 * skill's content begins with `--- ... ---` YAML).
 *
 * The key column is fixed-width-ish (`min-w-[80px]`) so labels align across
 * rows when the panel contains several entries. The value column uses
 * `whitespace-pre-wrap break-words` so multi-line values (e.g. long descriptions
 * in `---\ndescription: ...\n---`) preserve their line breaks without
 * horizontally overflowing the card.
 */
export function FrontmatterCard({ data }: { data: SkillFrontmatter }) {
  const entries = Object.entries(data);
  if (entries.length === 0) return null;

  return (
    <div className="rounded-lg border bg-muted/30 px-3 py-2">
      <div className="grid gap-1.5">
        {entries.map(([key, value]) => (
          <div key={key} className="flex gap-2 text-xs">
            <span className="shrink-0 font-medium text-muted-foreground min-w-[80px]">
              {key}
            </span>
            <span className="text-foreground whitespace-pre-wrap break-words">
              {value.trimEnd()}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
