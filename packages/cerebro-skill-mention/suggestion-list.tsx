"use client";

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from "react";
import { isImeComposing } from "@multica/core/utils";
import type { SkillMentionItem } from "./types";

interface SkillMentionListProps {
  items: SkillMentionItem[];
  command: (item: SkillMentionItem) => void;
}

export interface SkillMentionListRef {
  onKeyDown: (props: { event: KeyboardEvent }) => boolean;
}

export const SkillMentionList = forwardRef<
  SkillMentionListRef,
  SkillMentionListProps
>(function SkillMentionList({ items, command }, ref) {
  const [selectedIndex, setSelectedIndex] = useState(0);
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);

  useEffect(() => {
    setSelectedIndex(0);
  }, [items]);

  useEffect(() => {
    itemRefs.current[selectedIndex]?.scrollIntoView({ block: "nearest" });
  }, [selectedIndex]);

  const selectItem = useCallback(
    (index: number) => {
      const item = items[index];
      if (!item) return;
      command(item);
    },
    [items, command],
  );

  useImperativeHandle(ref, () => ({
    onKeyDown: ({ event }) => {
      if (isImeComposing(event)) return false;
      if (event.key === "ArrowUp") {
        if (items.length === 0) return true;
        setSelectedIndex((i) => (i + items.length - 1) % items.length);
        return true;
      }
      if (event.key === "ArrowDown") {
        if (items.length === 0) return true;
        setSelectedIndex((i) => (i + 1) % items.length);
        return true;
      }
      if (event.key === "Enter") {
        if (items.length === 0) return true;
        selectItem(selectedIndex);
        return true;
      }
      return false;
    },
  }));

  if (items.length === 0) {
    return (
      <div className="rounded-md border bg-popover p-2 text-xs text-muted-foreground shadow-md">
        No skills found
      </div>
    );
  }

  return (
    <div className="rounded-md border bg-popover py-1 shadow-md w-72 max-h-[300px] overflow-y-auto">
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
          onClick={() => selectItem(idx)}
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
  );
});
