"use client";

import { memo, useCallback, useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { AnimatePresence, motion, useIsPresent } from "motion/react";
import { useReducedMotion } from "@multica/core/appearance";
import { ChevronDown, ChevronUp, Maximize2, X } from "@multica/ui/icons";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { cn } from "@multica/ui/lib/utils";
import { UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueRouteRef } from "@multica/core/issues/canonical-id";
import { prefetchIssuePage } from "@multica/core/issues/queries";
import { useIsGuest } from "@multica/core/permissions";
import {
  createShortcutChord,
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";
import { AppLink } from "../../navigation";
import { PAGE_GUTTER } from "../../layout/page-header";
import { WindowTitleBarProvider } from "../../platform/window-title-bar";
import { ShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useT } from "../../i18n";
import { IssueDetail } from "./issue-detail";
import {
  IssuePeekActionsContext,
  IssuePeekColumnsContext,
  IssuePeekIdContext,
  IssuePeekPositionContext,
  PEEK_TARGET_ATTR,
  hasIssueInColumns,
  locateInColumns,
  useIssuePeekActions,
  useIssuePeekColumns,
  useIssuePeekPosition,
  type IssuePeekActions,
  type IssuePeekColumns,
  type IssuePeekPosition,
} from "../surface/peek-context";

const NEXT_KEY = createShortcutChord("J");
const PREV_KEY = createShortcutChord("K");
const CLOSE_KEY = createShortcutChord("Escape");

type Step = "prevId" | "nextId" | "leftId" | "rightId";
// Vim keys work wherever focus is not in a text field; the arrows mirror them
// unless the panel is being read (see IssuePeekFollow).
const LETTER_STEPS: Record<string, Step> = { K: "prevId", J: "nextId", H: "leftId", L: "rightId" };
const ARROW_STEPS: Record<string, Step> = {
  ArrowUp: "prevId",
  ArrowDown: "nextId",
  ArrowLeft: "leftId",
  ArrowRight: "rightId",
};

/**
 * Owns the surface's side peek: which issue is open, the current view's order
 * for keyboard stepping, and the floating panel itself. Wraps the surface
 * content so the panel can position against it — it floats over the view,
 * below the page header and toolbar, which stay usable while it is open.
 *
 * Every view hosts it, so switching views keeps the peeked issue open; the new
 * view publishes its own order.
 *
 * Besides a focused card's own Space (see DraggableBoardCard), Space peeks the
 * issue under the pointer, so mouse users need not Tab to it first.
 */
export function IssuePeekHost({ children }: { children: ReactNode }) {
  const [peekId, setPeekId] = useState<string | null>(null);
  const [columns, setColumns] = useState<IssuePeekColumns | null>(null);

  const actions = useMemo<IssuePeekActions>(
    () => ({
      open: (issueId) => setPeekId(issueId),
      toggle: (issueId) => setPeekId((current) => (current === issueId ? null : issueId)),
      close: () => setPeekId(null),
      publishColumns: setColumns,
    }),
    [],
  );
  const openId = peekId;
  const position = useMemo(() => locateInColumns(columns, openId), [columns, openId]);

  // Keep the latest columns and last known neighbours for the open issue so
  // deleting it can land on the next (or previous) surviving item even when
  // the list/board cache prunes the deleted row before `onDelete` fires.
  const columnsRef = useRef(columns);
  columnsRef.current = columns;
  const lastNeighboursRef = useRef<{
    issueId: string;
    nextId: string | null;
    prevId: string | null;
  } | null>(null);
  if (!openId) {
    lastNeighboursRef.current = null;
  } else if (position) {
    lastNeighboursRef.current = {
      issueId: openId,
      nextId: position.nextId,
      prevId: position.prevId,
    };
  }

  const handleDelete = useCallback((deletedId: string) => {
    setPeekId((current) => {
      if (current !== deletedId) return current;
      const cols = columnsRef.current;
      const pos = locateInColumns(cols, deletedId);
      const fallback =
        lastNeighboursRef.current?.issueId === deletedId
          ? lastNeighboursRef.current
          : null;
      for (const candidate of [
        pos?.nextId,
        pos?.prevId,
        fallback?.nextId,
        fallback?.prevId,
      ]) {
        if (candidate && candidate !== deletedId && hasIssueInColumns(cols, candidate)) {
          return candidate;
        }
      }
      return null;
    });
  }, []);

  // Tracked from pointer events rather than queried with `:hover`, so the
  // card under a stationary pointer is known without a layout read.
  const hoveredCardRef = useRef<string | null>(null);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== " " || event.defaultPrevented || event.repeat || isImeComposing(event)) return;
      if (event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
      // A focused control keeps its own Space: buttons press, inputs type.
      if (isEditableShortcutTarget(event.target) || isControlTarget(event.target)) return;
      if (isPortalLayerShortcutTarget(event.target)) return;
      if (useModalStore.getState().modal) return;
      const hovered = hoveredCardRef.current;
      if (!hovered) return;
      event.preventDefault();
      actions.toggle(hovered);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [actions]);

  return (
    <IssuePeekActionsContext.Provider value={actions}>
      <IssuePeekIdContext.Provider value={openId}>
        <IssuePeekPositionContext.Provider value={position}>
          <IssuePeekColumnsContext.Provider value={columns}>
          <div
            data-peek-open={openId ? "" : undefined}
            className="group/peek relative flex min-h-0 flex-1 flex-col [--issue-peek-width:520px]"
            onPointerOver={(event) => {
              hoveredCardRef.current =
                (event.target as Element).closest(`[${PEEK_TARGET_ATTR}]`)?.getAttribute(PEEK_TARGET_ATTR) ||
                null;
            }}
            onPointerLeave={() => {
              hoveredCardRef.current = null;
            }}
          >
            {children}
            {/* One stable key: switching issues swaps the content in place
                (J / K is keyboard navigation — it must not animate); only
                opening and closing the panel does. */}
            <AnimatePresence initial={false}>
              {openId && (
                <IssuePeekPanel key="issue-peek" issueId={openId} onDelete={handleDelete} />
              )}
            </AnimatePresence>
          </div>
          </IssuePeekColumnsContext.Provider>
        </IssuePeekPositionContext.Provider>
      </IssuePeekIdContext.Provider>
    </IssuePeekActionsContext.Provider>
  );
}

// Memoized, and blind to the column order: the host re-renders on every board
// reorder (a drag publishes columns on each hover), and none of that may reach
// IssueDetail. What does follow the order lives in IssuePeekFollow.
const IssuePeekPanel = memo(function IssuePeekPanel({
  issueId,
  onDelete,
}: {
  issueId: string;
  onDelete: (issueId: string) => void;
}) {
  const { t } = useT("issues");
  const panelRef = useRef<HTMLElement>(null);
  // False while the exit animation plays: the closing panel no longer takes
  // clicks or keys, so the board is usable the moment the peek is dismissed.
  const isPresent = useIsPresent();
  const reduceMotion = useReducedMotion();
  const handleDelete = useCallback(() => onDelete(issueId), [onDelete, issueId]);

  return (
    <motion.aside
      ref={panelRef}
      aria-label={t(($) => $.peek.panel_label)}
      data-issue-peek=""
      className={cn(
        "absolute right-2 top-2 z-30 flex w-[min(var(--issue-peek-width),calc(100%-1rem))] flex-col overflow-hidden",
        "rounded-xl bg-background shadow-[var(--floating-shadow)] ring-1 ring-floating-border",
        // Stops above the chat launcher, which owns the dashboard's
        // bottom-right corner — the panel's composer would sit under it.
        "above-chat-launcher",
        // Floats over the top of the page, which may be the window's drag
        // region (the Desktop issue window); without this, its header's
        // controls would move the window instead of taking the click.
        "[-webkit-app-region:no-drag]",
        !isPresent && "pointer-events-none",
      )}
      // Slides in from the edge it is anchored to; the exit is shorter and
      // travels half as far. Reduced motion keeps only the fade.
      initial={{ opacity: 0, transform: reduceMotion ? "translateX(0)" : "translateX(16px)" }}
      animate={{
        opacity: 1,
        transform: "translateX(0)",
        transition: { duration: UI_MOTION_DURATION.standard, ease: UI_EASE_OUT },
      }}
      exit={{
        opacity: 0,
        transform: reduceMotion ? "translateX(0)" : "translateX(8px)",
        transition: { duration: UI_MOTION_DURATION.fast, ease: UI_EASE_OUT },
      }}
    >
      {isPresent && <IssuePeekFollow issueId={issueId} panelRef={panelRef} />}
      <WindowTitleBarProvider value={null}>
        <ErrorBoundary
          resetKeys={[issueId]}
          // The default fallback is a bare message card; here it would be a
          // panel with no way to close it but Esc.
          fallback={({ error }) => (
            <div className="flex flex-1 min-h-0 flex-col">
              <div className={cn("flex h-12 shrink-0 items-center justify-end gap-1 border-b", PAGE_GUTTER)}>
                <IssuePeekTrailingActions issueId={issueId} />
              </div>
              <div className="flex flex-1 min-h-0 items-center justify-center px-4 text-center text-body text-muted-foreground">
                {error.message}
              </div>
            </div>
          )}
        >
          <IssueDetail
            key={issueId}
            issueId={issueId}
            variant="peek"
            defaultSidebarOpen={false}
            leadingAction={<IssuePeekNav />}
            trailingActions={<IssuePeekTrailingActions issueId={issueId} />}
            onDelete={handleDelete}
          />
        </ErrorBoundary>
      </WindowTitleBarProvider>
    </motion.aside>
  );
});

/** The panel's keyboard (Esc, J / K / H / L, arrows), neighbour prefetch, and keeping the peeked issue in view. */
function IssuePeekFollow({
  issueId,
  panelRef,
}: {
  issueId: string;
  panelRef: RefObject<HTMLElement | null>;
}) {
  const actions = useIssuePeekActions()!;
  const position = useIssuePeekPosition();
  const reduceMotion = useReducedMotion();

  useIssuePeekPrefetch(issueId, position);

  // Read through a ref so the listener is bound once, not on every step.
  const positionRef = useRef(position);
  positionRef.current = position;
  const columns = useIssuePeekColumns();
  const columnCountRef = useRef(0);
  columnCountRef.current = columns?.length ?? 0;

  useEffect(() => {
    // Whether the reader's last click landed in the panel. Clicking panel text
    // moves no focus, so this — not the key's target — is what says the arrows
    // should scroll the panel rather than step through the view.
    let readingPanel = false;
    const onPointerDown = (event: PointerEvent) => {
      readingPanel = !!panelRef.current?.contains(event.target as Node);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.repeat || isImeComposing(event)) return;
      if (event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
      if (isEditableShortcutTarget(event.target)) return;
      if (isPortalLayerShortcutTarget(event.target)) return;
      if (useModalStore.getState().modal) return;

      if (event.key === "Escape") {
        event.preventDefault();
        actions.close();
        return;
      }
      let step = LETTER_STEPS[event.key.toUpperCase()];
      if (!step && event.key in ARROW_STEPS) {
        const inPanel =
          readingPanel || !!panelRef.current?.contains(event.target as Node);
        // Composite widgets (tabs, radios, sliders) own their arrow keys.
        if (inPanel || isArrowWidgetTarget(event.target)) return;
        step = ARROW_STEPS[event.key];
      }
      const target = step ? positionRef.current?.[step] : null;
      if (!target) {
        // Where H / L step across columns, the last column keeps them too:
        // L there is not a cue to edit labels.
        if ((step === "leftId" || step === "rightId") && columnCountRef.current > 1) event.preventDefault();
        return;
      }
      event.preventDefault();
      actions.open(target);
    };
    document.addEventListener("pointerdown", onPointerDown, true);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown, true);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [actions, panelRef]);

  // Keep the peeked issue in sight: scroll it into its column, then scroll the
  // board sideways if the panel covers it (the board reserves room for this
  // while a peek is open — see BoardView).
  //
  // Checked on every order change, but acted on only when the element standing
  // for the issue is a new one: another issue, the same card remounted in
  // another column (an edit made here moved it), or another view. Reorders
  // that leave the element in place never scroll, so a reader who scrolled
  // the peeked card away is not pulled back by an unrelated update.
  const revealedRef = useRef<HTMLElement | null>(null);
  useEffect(() => {
    const panel = panelRef.current;
    const host = panel?.parentElement;
    if (!panel || !host) return;
    const card = host.querySelector<HTMLElement>(
      `[${PEEK_TARGET_ATTR}="${CSS.escape(issueId)}"]`,
    );
    if (!card || card === revealedRef.current) return;
    revealedRef.current = card;
    card.scrollIntoView?.({ block: "nearest", inline: "nearest" });
    const scroller = card.closest<HTMLElement>("[data-board-scroller]");
    // offsetLeft, not the panel's rect: on the first frame the rect is still
    // shifted by the slide-in animation.
    const panelLeft = host.getBoundingClientRect().left + panel.offsetLeft;
    const covered = card.getBoundingClientRect().right - (panelLeft - 16);
    if (scroller && covered > 0) {
      scroller.scrollBy?.({ left: covered, behavior: reduceMotion ? "auto" : "smooth" });
    }
  }, [issueId, position, panelRef, reduceMotion]);

  return null;
}

/**
 * Warms the adjacent issues in the current column (`nextId`, then `prevId`)
 * once the browser is idle, so stepping with J / K (or the prev/next arrows)
 * renders from cache instead of flashing IssueDetailSkeleton.
 */
function useIssuePeekPrefetch(issueId: string, position: IssuePeekPosition | null) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  // A guest's issue page shows no runs, and the server refuses them.
  const guest = useIsGuest(wsId);
  const nextId = position?.nextId ?? null;
  const prevId = position?.prevId ?? null;
  const targets = useMemo(() => {
    const picked: string[] = [];
    for (const candidate of [nextId, prevId]) {
      if (candidate && candidate !== issueId && !picked.includes(candidate)) {
        picked.push(candidate);
      }
    }
    return picked;
  }, [issueId, nextId, prevId]);
  const targetKey = targets.join(",");

  useEffect(() => {
    if (!wsId || !targetKey) return;
    let cancelled = false;
    const run = async () => {
      // One issue at a time: each fans out into up to four requests.
      for (const targetId of targets) {
        if (cancelled) return;
        await prefetchIssuePage(qc, wsId, targetId, undefined, { runs: !guest }).catch(() => {
          // A failed warm-up is retried when the user steps to that issue.
        });
      }
    };
    if (typeof window.requestIdleCallback === "function") {
      const id = window.requestIdleCallback(() => void run(), { timeout: 150 });
      return () => {
        cancelled = true;
        window.cancelIdleCallback(id);
      };
    }
    const timer = window.setTimeout(() => void run(), 100);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed by target ids and viewer role
  }, [qc, wsId, targetKey, guest]);
}

function isArrowWidgetTarget(target: EventTarget | null) {
  return (
    target instanceof Element &&
    target.closest("[role='tab'], [role='radio'], [role='slider'], [role='spinbutton'], select") !== null
  );
}

/** A focused control whose own Space must win over the hovered-card peek. */
function isControlTarget(target: EventTarget | null) {
  return (
    target instanceof Element &&
    target.closest("a, button, select, summary, [role='button'], [role='menuitem'], [role='option'], [role='tab']") !== null
  );
}

/** Previous / next card in the peeked issue's board column. */
function IssuePeekNav() {
  const { t } = useT("issues");
  const actions = useIssuePeekActions()!;
  const position = useIssuePeekPosition();
  const steps = [
    { label: t(($) => $.peek.previous), shortcut: PREV_KEY, icon: ChevronUp, target: position?.prevId },
    { label: t(($) => $.peek.next), shortcut: NEXT_KEY, icon: ChevronDown, target: position?.nextId },
  ];

  return (
    <div className="flex shrink-0 items-center">
      {steps.map(({ label, shortcut, icon: Icon, target }) => (
        <Tooltip key={shortcut.key}>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground"
                aria-label={label}
                disabled={!target}
                onClick={() => target && actions.open(target)}
              >
                <Icon />
              </Button>
            }
          />
          <TooltipContent side="bottom">
            {label}
            <ShortcutKeycaps shortcut={shortcut} decorative className="ml-1.5" />
          </TooltipContent>
        </Tooltip>
      ))}
      {position && (
        <span className="ml-1 text-caption tabular-nums text-muted-foreground">
          {`${position.index} / ${position.total}`}
        </span>
      )}
    </div>
  );
}

function IssuePeekTrailingActions({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const actions = useIssuePeekActions()!;
  const paths = useWorkspacePaths();
  const issueRef = useIssueRouteRef(useWorkspaceId(), issueId);

  return (
    <>
      <Tooltip>
        <TooltipTrigger
          render={
            <AppLink
              href={paths.issueDetail(issueRef)}
              aria-label={t(($) => $.peek.open_full_page)}
              className={cn(buttonVariants({ variant: "ghost", size: "icon-sm" }), "text-muted-foreground")}
            />
          }
        >
          <Maximize2 />
        </TooltipTrigger>
        <TooltipContent side="bottom">{t(($) => $.peek.open_full_page)}</TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              className="text-muted-foreground"
              aria-label={t(($) => $.peek.close)}
              onClick={actions.close}
            />
          }
        >
          <X />
        </TooltipTrigger>
        <TooltipContent side="bottom">
          {t(($) => $.peek.close)}
          <ShortcutKeycaps shortcut={CLOSE_KEY} decorative className="ml-1.5" />
        </TooltipContent>
      </Tooltip>
    </>
  );
}
