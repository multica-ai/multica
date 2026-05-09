"use client";

import type { CreateMode } from "@multica/core/issues/stores/create-mode-store";
import { cn } from "@multica/ui/lib/utils";

const CREATE_MODES: Array<{ value: CreateMode; label: string }> = [
  { value: "manual", label: "Manual" },
  { value: "agent", label: "Agent" },
  { value: "batch", label: "Batch" },
];

export function CreateModeSelector({
  mode,
  onSelect,
  className,
}: {
  mode: CreateMode;
  onSelect: (mode: CreateMode) => void;
  className?: string;
}) {
  const selectedIndex = Math.max(0, CREATE_MODES.findIndex((item) => item.value === mode));
  const focusMode = (nextMode: CreateMode, currentTarget: HTMLButtonElement) => {
    onSelect(nextMode);
    window.requestAnimationFrame(() => {
      const nextTab = currentTarget
        .closest('[role="tablist"]')
        ?.querySelector<HTMLButtonElement>(`[data-create-mode="${nextMode}"]`);
      nextTab?.focus();
    });
  };

  return (
    <div
      role="tablist"
      aria-label="Issue creation mode"
      className={cn(
        "inline-flex h-7 shrink-0 items-center rounded-md border bg-muted/40 p-0.5",
        className,
      )}
    >
      {CREATE_MODES.map((item) => (
        <button
          key={item.value}
          type="button"
          role="tab"
          data-create-mode={item.value}
          aria-selected={mode === item.value}
          tabIndex={mode === item.value ? 0 : -1}
          onClick={() => onSelect(item.value)}
          onKeyDown={(event) => {
            const keyToIndex: Record<string, number | undefined> = {
              ArrowRight: selectedIndex + 1,
              ArrowDown: selectedIndex + 1,
              ArrowLeft: selectedIndex - 1,
              ArrowUp: selectedIndex - 1,
              Home: 0,
              End: CREATE_MODES.length - 1,
            };
            const targetIndex = keyToIndex[event.key];
            if (targetIndex === undefined) return;
            event.preventDefault();
            const nextIndex = (targetIndex + CREATE_MODES.length) % CREATE_MODES.length;
            focusMode(CREATE_MODES[nextIndex]!.value, event.currentTarget);
          }}
          className={cn(
            "h-6 rounded-[5px] px-2 text-xs font-medium text-muted-foreground transition-colors",
            "hover:bg-background/80 hover:text-foreground",
            "focus:outline-none focus-visible:outline-none",
            mode === item.value && "bg-background text-foreground shadow-xs",
          )}
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}
