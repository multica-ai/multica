"use client";

import { useLayoutEffect, useState } from "react";
import { motion } from "motion/react";
import { useQuery } from "@tanstack/react-query";
import { useReducedMotion } from "@multica/core/appearance";
import { UI_EASE_OUT } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import { Dialog, DialogContent } from "@multica/ui/components/ui/dialog";
import {
  useCreateModeStore,
  type CreateMode,
} from "@multica/core/issues/stores/create-mode-store";
import { AgentCreatePanel } from "./quick-create-issue";
import { ManualCreatePanel, manualDialogContentClass } from "./create-issue";
import { sourceContextPreviewOptions } from "@multica/core/issues/queries";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import { useWorkspaceId } from "@multica/core/hooks";

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
 * `isExpanded` is lifted up because it drives `DialogContent`'s className —
 * the className lives here in the shell since the Popup is here, but the
 * toggle for that state lives in the panel bodies. It is persisted in the
 * create-mode store, so the dialog reopens at the size it was last left at.
 */
export function CreateIssueDialog({
  open = true,
  onClose,
  onOpenChangeComplete,
  initialMode,
  data,
}: {
  open?: boolean;
  onClose: () => void;
  onOpenChangeComplete?: (open: boolean) => void;
  initialMode: CreateMode;
  data?: Record<string, unknown> | null;
}) {
  const anchorCommentId = typeof data?.anchor_comment_id === "string"
    ? data.anchor_comment_id
    : null;
  if (anchorCommentId) {
    return (
      <SourceContextCreateIssueDialog
        open={open}
        onClose={onClose}
        onOpenChangeComplete={onOpenChangeComplete}
        initialMode={initialMode}
        data={data}
        anchorCommentId={anchorCommentId}
      />
    );
  }
  return (
    <CreateIssueDialogBody
      open={open}
      onClose={onClose}
      onOpenChangeComplete={onOpenChangeComplete}
      initialMode={initialMode}
      data={data}
    />
  );
}

function SourceContextCreateIssueDialog({
  open,
  onClose,
  onOpenChangeComplete,
  initialMode,
  data,
  anchorCommentId,
}: {
  open: boolean;
  onClose: () => void;
  onOpenChangeComplete?: (open: boolean) => void;
  initialMode: CreateMode;
  data?: Record<string, unknown> | null;
  anchorCommentId: string;
}) {
  const wsId = useWorkspaceId();
  const [draftReady, setDraftReady] = useState(false);
  const [sourceContextExpanded, setSourceContextExpanded] = useState(false);
  const sourceQuery = useQuery(sourceContextPreviewOptions(wsId, anchorCommentId));

  useLayoutEffect(() => {
    useIssueDraftStore.getState().beginIsolatedDraft();
    setDraftReady(true);
    return () => useIssueDraftStore.getState().endIsolatedDraft();
  }, []);

  if (!draftReady) return null;

  return (
    <CreateIssueDialogBody
      open={open}
      onClose={onClose}
      onOpenChangeComplete={onOpenChangeComplete}
      initialMode={initialMode}
      data={data}
      sourceContextExpanded={sourceContextExpanded}
      sourceContextData={{
        anchor_comment_id: anchorCommentId,
        source_context_preview: sourceQuery.isError || sourceQuery.isFetching
          ? undefined
          : sourceQuery.data,
        source_context_loading: sourceQuery.isLoading || sourceQuery.isFetching,
        source_context_failed: sourceQuery.isError,
        source_context_error: sourceQuery.error,
        source_context_refetch: sourceQuery.refetch,
        source_context_expanded: sourceContextExpanded,
        source_context_on_expanded_change: setSourceContextExpanded,
      }}
    />
  );
}

function CreateIssueDialogBody({
  open,
  onClose,
  onOpenChangeComplete,
  initialMode,
  data,
  sourceContextData,
  sourceContextExpanded = false,
}: {
  open: boolean;
  onClose: () => void;
  onOpenChangeComplete?: (open: boolean) => void;
  initialMode: CreateMode;
  data?: Record<string, unknown> | null;
  sourceContextData?: Record<string, unknown>;
  sourceContextExpanded?: boolean;
}) {
  const prefersReducedMotion = useReducedMotion();
  const setLastMode = useCreateModeStore((s) => s.setLastMode);
  const [mode, setMode] = useState<CreateMode>(initialMode);
  const [modeSwitchCount, setModeSwitchCount] = useState(0);
  const [manualOpened, setManualOpened] = useState(initialMode === "manual");
  const [panelData, setPanelData] = useState(data ?? null);
  // Remembered across opens: the dialog reopens at the size it was left at.
  const isExpanded = useCreateModeStore((s) => s.expanded);
  const setIsExpanded = useCreateModeStore((s) => s.setExpanded);
  const effectiveData = {
    ...(panelData ?? {}),
    ...sourceContextData,
  };

  const switchTo = (next: CreateMode) => (carry?: Record<string, unknown> | null) => {
    setLastMode(next);
    // Apply contextual owner defaults only on the first visit to manual mode.
    // Later visits must preserve the user's edits, including an explicit clear.
    const seedParentOwner = next === "manual" && !manualOpened
      && typeof data?.parent_issue_id === "string"
      && carry?.parent_issue_id === data?.parent_issue_id
      && data?.assignee_type && data?.assignee_id;
    setPanelData(seedParentOwner ? {
      ...carry,
      assignee_type: data.assignee_type,
      assignee_id: data.assignee_id,
    } : carry ?? null);
    if (next === "manual") setManualOpened(true);
    setModeSwitchCount((count) => count + 1);
    setMode(next);
  };

  const className =
    mode === "agent"
      ? cn(
          "p-0 gap-0 flex flex-col overflow-hidden",
          "!top-1/2 !left-1/2 !-translate-x-1/2 !-translate-y-1/2",
          // Geometry-only transition so DialogContent's enter/exit animation
          // keeps its own duration & easing without being overridden.
          "[interpolate-size:allow-keywords] [transition-property:width,max-width,height] [transition-duration:200ms] [transition-timing-function:cubic-bezier(0.23,1,0.32,1)] motion-reduce:[transition-duration:0ms]",
          // Phone gutter. The widths below are `!important` so they beat
          // DialogContent's own sizing — which also made them beat its
          // `max-w-[calc(100%-2rem)]` safety margin, leaving the card flush
          // against both screen edges on a 430px viewport (MUL-6236). Restore
          // the margin here and let the `sm:` widths take over above 640px.
          "!w-full !max-w-[calc(100%-1.5rem)]",
          // Source-context create needs numeric collapsed/expanded endpoints
          // so its preview transition can interpolate the height. Ordinary
          // quick create has no expanding preview and keeps its original
          // content-driven height, capped for mobile browser chrome.
          isExpanded
            ? "!h-5/6 sm:!max-w-4xl"
            : sourceContextData
              ? sourceContextExpanded
                ? "!h-5/6 sm:!max-w-2xl"
                : "!h-96 sm:!max-w-xl"
              : "!max-h-[80%] sm:!max-w-xl",
        )
      : cn(
          manualDialogContentClass(isExpanded),
          sourceContextExpanded && "!h-5/6",
        );

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => { if (!v) onClose(); }}
      onOpenChangeComplete={onOpenChangeComplete}
    >
      <DialogContent
        finalFocus={false}
        showCloseButton={false}
        className={className}
      >
        <motion.div
          key={mode}
          initial={modeSwitchCount > 0 && !prefersReducedMotion ? { opacity: 0 } : false}
          animate={{ opacity: 1 }}
          transition={{ duration: prefersReducedMotion ? 0 : 0.13, ease: UI_EASE_OUT }}
          className="flex min-h-0 flex-1 flex-col"
        >
          {mode === "agent" ? (
            <AgentCreatePanel
              onClose={onClose}
              onSwitchMode={switchTo("manual")}
              data={effectiveData}
              isExpanded={isExpanded}
              setIsExpanded={setIsExpanded}
            />
          ) : (
            <ManualCreatePanel
              onClose={onClose}
              onSwitchMode={switchTo("agent")}
              data={effectiveData}
              isExpanded={isExpanded}
              setIsExpanded={setIsExpanded}
            />
          )}
        </motion.div>
      </DialogContent>
    </Dialog>
  );
}
