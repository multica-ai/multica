// TECH-3413 — the Classic→Dynamic switch lives inside the classic inbox's ⋯
// settings menu (Jesper's request) instead of as a standalone toolbar button,
// so it sits with the other view options and is reachable the same way on
// desktop and mobile. Renders only when the cerebro_inbox_dynamic flag is on.
"use client";

import { LayoutGrid } from "lucide-react";
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useSetInboxMode } from "../dynamic-mode";

export function CerebroInboxModeMenuItem() {
  const dynamicAvailable = useFeatureFlag("cerebro_inbox_dynamic");
  const setMode = useSetInboxMode();
  if (!dynamicAvailable) return null;
  return (
    <>
      <DropdownMenuSeparator />
      <DropdownMenuItem onClick={() => void setMode("dynamic")}>
        <LayoutGrid className="h-4 w-4" /> Switch to dynamic inbox
      </DropdownMenuItem>
    </>
  );
}
