"use client";

import { useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { Dialog, DialogContent } from "@multica/ui/components/ui/dialog";
import {
  useCreateModeStore,
  type CreateMode,
} from "@multica/core/issues/stores/create-mode-store";
import { AgentCreatePanel } from "./quick-create-issue";
import { ManualCreatePanel, manualDialogContentClass, mobileFullScreenDialog } from "./create-issue";

/**
 * Shell that owns the single `<Dialog>` AND `<DialogContent>` for the
 * create-issue flow. Mode switching unmounts/mounts only the inner panel
 * body — the Portal, Backdrop, and Popup all stay in the DOM, so Base UI
 * never replays the open animation. That's what makes the switch feel
 * instant; an earlier version put `<DialogContent>` inside each panel and
 * the close→open animation cycle still fired on every toggle.
 *
 * `initialMode` comes from the modal registry (`quick-create-issue` →
 * agent, `create-issue` → manual). Subsequent switches are local state
 * only and never round-trip through the modal store.
 *
 * Carry payload: when a panel switches mode it can hand a payload up via
 * `onSwitchMode`; the shell stores it as the next panel's `data` so seeding
 * works exactly like a fresh open.
 *
 * Manual-mode `isExpanded` / `backlogHintIssueId` are lifted up because they
 * drive `DialogContent`'s className — the className lives here in the shell
 * since the Popup is here, but the toggles for those states live in the
 * manual panel body.
 */
export function CreateIssueDialog({
  onClose,
  initialMode,
  data,
}: {
  onClose: () => void;
  initialMode: CreateMode;
  data?: Record<string, unknown> | null;
}) {
  const setLastMode = useCreateModeStore((s) => s.setLastMode);
  const [mode, setMode] = useState<CreateMode>(initialMode);
  const [panelData, setPanelData] = useState(data ?? null);
  const [isExpanded, setIsExpanded] = useState(false);
  const [backlogHintIssueId, setBacklogHintIssueId] = useState<string | null>(null);

  const switchTo = (next: CreateMode) => (carry?: Record<string, unknown> | null) => {
    setLastMode(next);
    setPanelData(carry ?? null);
    setMode(next);
  };

  const className =
    mode === "agent"
      ? cn(
          "p-0 gap-0 flex flex-col overflow-hidden",
          // Mobile: full-screen modal — centered windows are unusable on a
          // 390px viewport, especially when the keyboard is up. Desktop
          // keeps the centered window via md: overrides.
          mobileFullScreenDialog,
          "md:!top-1/2 md:!left-1/2 md:!-translate-x-1/2 md:!-translate-y-1/2",
          // Smooth size transition when switching modes — the manual mode
          // uses the same easing.
          "md:!transition-all md:!duration-300 md:!ease-out",
          // Expanded matches manual's expanded footprint so toggling expand
          // mid-flow (or after a mode switch) lands the user on the same
          // visual size. Collapsed keeps the slim, content-driven default
          // — pasted screenshots still scroll inside instead of pushing the
          // dialog past the viewport.
          isExpanded
            ? "md:!max-w-4xl md:!w-full md:!h-5/6"
            : "md:!max-w-xl md:!w-full md:!max-h-[80vh]",
        )
      : manualDialogContentClass(isExpanded, backlogHintIssueId);

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent
        finalFocus={false}
        showCloseButton={false}
        className={className}
      >
        {mode === "agent" ? (
          <AgentCreatePanel
            onClose={onClose}
            onSwitchMode={switchTo("manual")}
            data={panelData}
            isExpanded={isExpanded}
            setIsExpanded={setIsExpanded}
          />
        ) : (
          <ManualCreatePanel
            onClose={onClose}
            onSwitchMode={switchTo("agent")}
            data={panelData}
            isExpanded={isExpanded}
            setIsExpanded={setIsExpanded}
            backlogHintIssueId={backlogHintIssueId}
            setBacklogHintIssueId={setBacklogHintIssueId}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}
