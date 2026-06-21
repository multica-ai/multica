"use client";

// FIR-1771 — the two self-contained pieces the profile menu mounts:
//
//   CerebroTestAsUserMenuItem  — the bottom-of-menu entry (self-gates on the
//                                feature flag + the tools:test-as-user
//                                permission; renders nothing otherwise).
//   CerebroTestAsUserDialogHost — the dialog, mounted OUTSIDE the dropdown so it
//                                survives the menu closing on click.
//
// They live in separate mount points (the item is inside the dropdown, which
// unmounts on close; the host is at the sidebar root), so they share open state
// through a tiny module store rather than React state.

import { ShieldCheck } from "lucide-react";
import { create } from "zustand";
import { DropdownMenuItem } from "@multica/ui/components/ui/dropdown-menu";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { TestAsUserDialog } from "./test-as-user-dialog";
import { useTestAsUserAccess } from "./use-test-as-user-access";

interface TestAsUserMenuState {
  open: boolean;
  setOpen: (open: boolean) => void;
}

const useTestAsUserMenuStore = create<TestAsUserMenuState>((set) => ({
  open: false,
  setOpen: (open) => set({ open }),
}));

export function CerebroTestAsUserMenuItem({ wsId }: { wsId: string }) {
  const flagOn = useFeatureFlag("cerebro_test_as_user");
  const allowed = useTestAsUserAccess(wsId, flagOn);
  const setOpen = useTestAsUserMenuStore((s) => s.setOpen);
  if (!flagOn || !allowed) return null;
  return (
    <DropdownMenuItem onClick={() => setOpen(true)}>
      <ShieldCheck className="h-3.5 w-3.5" />
      Test as user
    </DropdownMenuItem>
  );
}

export function CerebroTestAsUserDialogHost({ wsId }: { wsId: string }) {
  const flagOn = useFeatureFlag("cerebro_test_as_user");
  const open = useTestAsUserMenuStore((s) => s.open);
  const setOpen = useTestAsUserMenuStore((s) => s.setOpen);
  if (!flagOn) return null;
  return <TestAsUserDialog wsId={wsId} open={open} onOpenChange={setOpen} />;
}
