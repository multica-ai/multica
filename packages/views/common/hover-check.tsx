"use client";

import { Check } from "lucide-react";

// shadcn official pattern (PR #6862): a checkbox indicator that appears on
// item hover/focus inside DropdownMenuCheckboxItem rows. Pair it with
// FILTER_ITEM_CLASS on the item (hides the built-in indicator and names the
// hover group). Extracted from issues-header so every filter dropdown shares
// one implementation.

export const FILTER_ITEM_CLASS =
  "group/fitem pr-1.5! [&>[data-slot=dropdown-menu-checkbox-item-indicator]]:hidden";

export function HoverCheck({ checked }: { checked: boolean }) {
  return (
    <div
      className="border-input data-[selected=true]:border-primary data-[selected=true]:bg-primary data-[selected=true]:text-primary-foreground pointer-events-none size-4 shrink-0 rounded-[4px] border transition-all select-none *:[svg]:opacity-0 data-[selected=true]:*:[svg]:opacity-100 opacity-0 group-hover/fitem:opacity-100 group-focus/fitem:opacity-100 data-[selected=true]:opacity-100"
      data-selected={checked}
    >
      <Check className="size-3.5 text-current" />
    </div>
  );
}
