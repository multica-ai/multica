"use client";

import { useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { Dialog, DialogContent } from "@multica/ui/components/ui/dialog";
import {
  useCreateModeStore,
  type CreateMode,
} from "@multica/core/issues/stores/create-mode-store";
import { AgentCreatePanel } from "./quick-create-issue";
import { ManualCreatePanel, manualDialogContentClass } from "./create-issue";

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
          "!top-1/2 !left-1/2 !-translate-x-1/2 !-translate-y-1/2",
          // Width is capped; height is content-driven up to 80vh so a
          // pasted screenshot can't push the dialog past the viewport
          // (the inner editor area scrolls instead).
          isExpanded
            ? "!max-w-4xl !w-full !h-5/6"
            : "!max-w-xl !w-full !max-h-[80vh]",
          // Smooth size transition when switching modes — the manual mode
          // uses the same easing.
          "!transition-all !duration-300 !ease-out",
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
