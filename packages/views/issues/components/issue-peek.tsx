"use client";

import { memo, useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from "react";
import { AnimatePresence, motion, useIsPresent, useReducedMotion } from "motion/react";
import { ChevronDown, ChevronUp, Maximize2, X } from "lucide-react";
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
import {
  createShortcutChord,
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";
import { AppLink } from "../../navigation";
import { PAGE_CHROME_GUTTER } from "../../layout/page-header";
import { ShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useT } from "../../i18n";
import { IssueDetail } from "./issue-detail";
import {
  IssuePeekActionsContext,
  IssuePeekIdContext,
  IssuePeekPositionContext,
  PEEK_TARGET_ATTR,
  locateInColumns,
  useIssuePeekActions,
  useIssuePeekPosition,
  type IssuePeekActions,
  type IssuePeekColumns,
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
              {openId && <IssuePeekPanel key="issue-peek" issueId={openId} />}
            </AnimatePresence>
          </div>
        </IssuePeekPositionContext.Provider>
      </IssuePeekIdContext.Provider>
    </IssuePeekActionsContext.Provider>
  );
}

// Memoized, and blind to the column order: the host re-renders on every board
// reorder (a drag publishes columns on each hover), and none of that may reach
// IssueDetail. What does follow the order lives in IssuePeekFollow.
const IssuePeekPanel = memo(function IssuePeekPanel({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const actions = useIssuePeekActions()!;
  const panelRef = useRef<HTMLElement>(null);
  // False while the exit animation plays: the closing panel no longer takes
  // clicks or keys, so the board is usable the moment the peek is dismissed.
  const isPresent = useIsPresent();
  const reduceMotion = useReducedMotion() ?? false;

  return (
    <motion.aside
      ref={panelRef}
      aria-label={t(($) => $.peek.panel_label)}
      data-issue-peek=""
      className={cn(
        "absolute right-2 top-2 z-30 flex w-[min(var(--issue-peek-width),calc(100%-1rem))] flex-col overflow-hidden",
        "rounded-xl bg-background shadow-[var(--floating-shadow)] ring-1 ring-surface-border",
        // Stops above the chat launcher, which owns the dashboard's
        // bottom-right corner — the panel's composer would sit under it.
        "above-chat-launcher",
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
      <ErrorBoundary
        resetKeys={[issueId]}
        // The default fallback is a bare message card; here it would be a
        // panel with no way to close it but Esc.
        fallback={({ error }) => (
          <div className="flex flex-1 min-h-0 flex-col">
            <div className={cn("flex h-12 shrink-0 items-center justify-end gap-1 border-b", PAGE_CHROME_GUTTER)}>
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
          onDelete={actions.close}
        />
      </ErrorBoundary>
    </motion.aside>
  );
});

/** The panel's keyboard (Esc, J / K / H / L, arrows) and keeping the peeked issue in view. */
function IssuePeekFollow({
  issueId,
  panelRef,
}: {
  issueId: string;
  panelRef: RefObject<HTMLElement | null>;
}) {
  const actions = useIssuePeekActions()!;
  const position = useIssuePeekPosition();
  const reduceMotion = useReducedMotion() ?? false;

  // Read through a ref so the listener is bound once, not on every step.
  const positionRef = useRef(position);
  positionRef.current = position;

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
      if (!target) return;
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

  return (
    <>
      <Tooltip>
        <TooltipTrigger
          render={
            <AppLink
              href={paths.issueDetail(issueId)}
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
