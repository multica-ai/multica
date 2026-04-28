"use client";

import { useState, useCallback, useRef, useEffect } from "react";
import { Check } from "lucide-react";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";

const HIGHLIGHT_CLASS = "bg-accent";
const ITEM_SELECTOR = "button[data-picker-item]:not(:disabled)";

// ---------------------------------------------------------------------------
// PropertyPicker — generic Popover shell with optional search
// ---------------------------------------------------------------------------

export function PropertyPicker({
  open,
  onOpenChange,
  trigger,
  triggerRender,
  width = "w-48",
  align = "end",
  searchable = false,
  searchPlaceholder = "Filter...",
  onSearchChange,
  children,
  footer,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  trigger: React.ReactNode;
  triggerRender?: React.ReactElement;
  width?: string;
  align?: "start" | "center" | "end";
  searchable?: boolean;
  searchPlaceholder?: string;
  onSearchChange?: (query: string) => void;
  children: React.ReactNode;
  /**
   * Optional footer rendered below the listbox. Unlike items rendered as
   * children, the footer is *not* included in arrow-key navigation — use it
   * for actions like "Create new…" or "Manage…" that shouldn't be treated as
   * selectable listbox options.
   */
  footer?: React.ReactNode;
}) {
  const [query, setQuery] = useState("");
  const [highlightedIndex, setHighlightedIndex] = useState(-1);
  const listRef = useRef<HTMLDivElement>(null);

  const getItems = useCallback(() => {
    if (!listRef.current) return [];
    return Array.from(
      listRef.current.querySelectorAll<HTMLButtonElement>(ITEM_SELECTOR),
    );
  }, []);

  // Apply/remove highlight class via DOM when index changes
  useEffect(() => {
    const items = getItems();
    for (const item of items) {
      item.classList.remove(HIGHLIGHT_CLASS);
    }
    if (highlightedIndex >= 0 && highlightedIndex < items.length) {
      items[highlightedIndex]?.classList.add(HIGHLIGHT_CLASS);
    }
  }, [highlightedIndex, getItems, children]); // re-run when children change (filtered list updates)

  const handleOpenChange = useCallback(
    (v: boolean) => {
      onOpenChange(v);
      if (!v) {
        setQuery("");
        setHighlightedIndex(-1);
        onSearchChange?.("");
      }
    },
    [onOpenChange, onSearchChange],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      const items = getItems();
      if (items.length === 0) return;

      if (e.key === "ArrowDown") {
        e.preventDefault();
        setHighlightedIndex((prev) => {
          const next = prev < items.length - 1 ? prev + 1 : 0;
          items[next]?.scrollIntoView({ block: "nearest" });
          return next;
        });
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setHighlightedIndex((prev) => {
          const next = prev > 0 ? prev - 1 : items.length - 1;
          items[next]?.scrollIntoView({ block: "nearest" });
          return next;
        });
      } else if (e.key === "Enter") {
        e.preventDefault();
        if (highlightedIndex >= 0 && highlightedIndex < items.length) {
          items[highlightedIndex]?.click();
        } else if (items.length === 1) {
          // Auto-select when only one result
          items[0]?.click();
        }
      }
    },
    [getItems, highlightedIndex],
  );

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger
        className={triggerRender ? undefined : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden"}
        render={triggerRender}
      >
        {trigger}
      </PopoverTrigger>
      <PopoverContent align={align} className={`${width} gap-0 p-0`}>
        {searchable && (
          <div className="px-2 py-1.5 border-b">
            <input
              type="text"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setHighlightedIndex(0);
                onSearchChange?.(e.target.value);
              }}
              onKeyDown={handleKeyDown}
              placeholder={searchPlaceholder}
              aria-label="Filter options"
              className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
            />
          </div>
        )}
        <div ref={listRef} className="p-1 max-h-60 overflow-y-auto">{children}</div>
        {footer && <div className="border-t p-1">{footer}</div>}
      </PopoverContent>
    </Popover>
  );
}

// ---------------------------------------------------------------------------
// PickerItem — single selectable row
// ---------------------------------------------------------------------------

export function PickerItem({
  selected,
  disabled,
  onClick,
  hoverClassName,
  children,
}: {
  selected: boolean;
  disabled?: boolean;
  onClick: () => void;
  hoverClassName?: string;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      data-picker-item
      disabled={disabled}
      onClick={onClick}
      className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm ${disabled ? "opacity-50 cursor-not-allowed" : hoverClassName ?? "hover:bg-accent"} transition-colors`}
    >
      {/* min-w-0 lets long children (like truncated label names) shrink
          inside the flex row instead of pushing the selected checkmark off
          the right edge. shrink-0 on the Check keeps it visible. */}
      <span className="flex min-w-0 flex-1 items-center gap-2">{children}</span>
      {selected && <Check className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
    </button>
  );
}

// ---------------------------------------------------------------------------
// PickerSection — group header
// ---------------------------------------------------------------------------

export function PickerSection({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">
        {label}
      </div>
      {children}
    </div>
  );
}

// ---------------------------------------------------------------------------
// PickerEmpty — no results state
// ---------------------------------------------------------------------------

export function PickerEmpty() {
  return (
    <div className="px-2 py-3 text-center text-sm text-muted-foreground">
      No results
    </div>
  );
}
