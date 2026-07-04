"use client";

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from "react";
import type { Editor, Range } from "@tiptap/core";
import { motion } from "motion/react";
import { cn } from "@multica/ui/lib/utils";
import { isPickerAcceptKey } from "./suggestion-popup";
import { isImeComposing } from "@multica/core/utils";
import type { LucideIcon } from "lucide-react";

export interface FormattingCommandItem {
  id: string;
  title: string;
  description: string;
  icon: LucideIcon;
  action: (editor: Editor, range: Range) => void;
}

export interface FormattingSlashCommandListProps {
  items: FormattingCommandItem[];
  command: (item: FormattingCommandItem) => void;
}

export interface FormattingSlashCommandListRef {
  onKeyDown: (props: { event: KeyboardEvent }) => boolean;
}

export const FormattingSlashCommandList = forwardRef<
  FormattingSlashCommandListRef,
  FormattingSlashCommandListProps
>(function FormattingSlashCommandList({ items, command }, ref) {
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
        if (items.length === 0) return false;
        setSelectedIndex((i) => (i + items.length - 1) % items.length);
        return true;
      }
      if (event.key === "ArrowDown") {
        if (items.length === 0) return false;
        setSelectedIndex((i) => (i + 1) % items.length);
        return true;
      }
      if (isPickerAcceptKey(event)) {
        if (items.length === 0) return false;
        selectItem(selectedIndex);
        return true;
      }
      return false;
    },
  }));

  if (items.length === 0) {
    return null;
  }

  return (
    <motion.div
      initial={{ opacity: 0, scale: 0.95 }}
      animate={{ opacity: 1, scale: 1 }}
      exit={{ opacity: 0, scale: 0.95 }}
      transition={{ duration: 0.15, ease: "easeOut" }}
      className="flex max-h-[300px] w-[320px] flex-col overflow-y-auto rounded-xl border bg-popover p-1.5 text-popover-foreground shadow-xl shadow-black/5"
    >
      <div className="px-2 py-1.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/70">
        Basic Blocks
      </div>
      {items.map((item, index) => {
        const Icon = item.icon;

        return (
          <button
            key={item.id}
            ref={(el) => {
              itemRefs.current[index] = el;
            }}
            type="button"
            className={cn(
              "flex items-start gap-2.5 rounded-lg px-2 py-1.5 text-left text-sm cursor-pointer transition-colors outline-none",
              index === selectedIndex
                ? "bg-accent text-accent-foreground"
                : "hover:bg-accent/50",
            )}
            onClick={() => selectItem(index)}
          >
            <div className="mt-0.5 flex size-10 shrink-0 items-center justify-center rounded-md border bg-background text-foreground shadow-sm">
              <Icon className="size-5 text-muted-foreground" />
            </div>
            <div className="flex flex-col justify-center min-h-[40px] overflow-hidden">
              <span className="truncate font-medium">{item.title}</span>
              <span className="truncate text-[11.5px] text-muted-foreground/80 leading-tight mt-0.5">
                {item.description}
              </span>
            </div>
          </button>
        );
      })}
    </motion.div>
  );
});
