"use client";

import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { getCurrentWsId } from "@multica/core/platform";
import { skillDetailOptions } from "@multica/core/workspace/queries";
import { skillPreviewText } from "./skill-preview";
import type { SkillMentionItem } from "./types";

interface SkillMentionListProps {
  items: SkillMentionItem[];
  selectedIndex: number;
  onSelect: (index: number) => void;
  onHover: (index: number) => void;
}

/**
 * Pure presentational list. Keyboard navigation (ArrowUp/Down/Enter) is
 * owned by the extension closure in extension.ts so it works the instant
 * the popup opens — before React has mounted this component and before any
 * imperative ref has been bound (FIR-2299 follow-up).
 */
export function SkillMentionList({
  items,
  selectedIndex,
  onSelect,
  onHover,
}: SkillMentionListProps) {
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);

  useEffect(() => {
    itemRefs.current[selectedIndex]?.scrollIntoView({ block: "nearest" });
  }, [selectedIndex]);

  if (items.length === 0) {
    return (
      <div className="rounded-md border bg-popover p-2 text-xs text-muted-foreground shadow-md">
        No skills found
      </div>
    );
  }

  const activeItem = items[selectedIndex];

  return (
    <div className="flex items-start gap-2">
      <div className="w-72 max-h-[300px] overflow-y-auto rounded-md border bg-popover py-1 shadow-md">
        <div className="px-3 py-1.5 text-xs font-medium text-muted-foreground">
          Skills
        </div>
        {items.map((item, idx) => (
          <button
            key={item.id}
            ref={(el) => {
              itemRefs.current[idx] = el;
            }}
            className={`flex w-full flex-col items-start gap-0.5 px-3 py-1.5 text-left text-xs transition-colors ${
              idx === selectedIndex ? "bg-accent" : "hover:bg-accent/50"
            }`}
            onClick={() => onSelect(idx)}
            onMouseEnter={() => onHover(idx)}
          >
            <span className="font-medium">{item.name}</span>
            {item.description && (
              <span className="line-clamp-1 text-[11px] text-muted-foreground">
                {item.description}
              </span>
            )}
          </button>
        ))}
      </div>
      {activeItem && <SkillPreviewPanel item={activeItem} />}
    </div>
  );
}

/**
 * Side panel that previews what the highlighted skill does. Shows the skill
 * name and description immediately (already in the list cache) and lazily
 * fetches the full SKILL.md body for the instructions preview — react-query
 * caches per skill, so arrow-keying or hovering back over a skill is instant.
 */
function SkillPreviewPanel({ item }: { item: SkillMentionItem }) {
  const wsId = getCurrentWsId();
  const { data: skill, isLoading } = useQuery({
    ...skillDetailOptions(wsId ?? "", item.id),
    enabled: !!wsId && !!item.id,
  });

  const preview = skillPreviewText(skill?.content);

  return (
    <div className="w-80 max-h-[300px] overflow-y-auto rounded-md border bg-popover p-3 shadow-md">
      <div className="text-sm font-medium text-foreground">{item.name}</div>
      {item.description && (
        <p className="mt-1 text-xs text-muted-foreground">{item.description}</p>
      )}
      <div className="mt-2 border-t pt-2">
        {isLoading ? (
          <span className="text-[11px] text-muted-foreground">
            Loading preview…
          </span>
        ) : preview ? (
          <pre className="whitespace-pre-wrap break-words font-sans text-[11px] leading-relaxed text-muted-foreground">
            {preview}
          </pre>
        ) : (
          <span className="text-[11px] text-muted-foreground">
            No preview available
          </span>
        )}
      </div>
    </div>
  );
}
