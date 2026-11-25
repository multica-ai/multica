"use client";

import { Fragment, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { motion } from "motion/react";
import { useReducedMotion } from "@multica/core/appearance";
import { ChevronRight, SearchIcon } from "@multica/ui/icons";
import { UI_EASE_OUT } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import { Command as CommandPrimitive } from "cmdk";
import {
  selectCommandBarContexts,
  useCommandBarContextStore,
  useSearchStore,
} from "@multica/core/command-bar";
import { createShortcutChord } from "@multica/core/shortcuts";
import { useWorkspaceSlug } from "@multica/core/paths";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { ShortcutKeycaps } from "../common/shortcut-keycaps";
import { resolveClickIntent, useIntentNavigate, type LinkClickIntent } from "../navigation";
import { useT } from "../i18n";
import { PaletteContext, type PaletteApi } from "./palette-context";
import { PaletteFooter } from "./footer";
import { PaletteHostDialogs } from "./host-dialogs";
import { IssueShortcuts } from "./issue-shortcuts";
import { clearPendingPalettePage, usePendingPalettePage } from "./palette-launch";
import { PageView } from "./pages/page-view";
import { RootView } from "./root-view";
import { useRootModel } from "./use-root-model";
import type { PaletteHostDialog, PaletteStackEntry } from "./types";

const ESCAPE = createShortcutChord("Escape");

/**
 * ⌘K. The shell owns the dialog, the input, the page stack and the footer;
 * what the palette offers comes from the pages on screen (registered contexts)
 * and the search over workspace objects.
 */
export function SearchCommand() {
  const { t } = useT("search");
  const workspaceSlug = useWorkspaceSlug();
  const open = useSearchStore((s) => s.open);
  const setOpen = useSearchStore((s) => s.setOpen);
  const launchPage = usePendingPalettePage();
  const [hostDialog, setHostDialog] = useState<PaletteHostDialog | null>(null);
  const close = useCallback(() => setOpen(false), [setOpen]);
  const openHostDialog = useCallback(
    (dialog: PaletteHostDialog) => {
      setOpen(false);
      setHostDialog(dialog);
    },
    [setOpen],
  );

  return (
    <>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent
          finalFocus={false}
          className="top-[20%] translate-y-0 overflow-hidden rounded-xl! p-0 sm:max-w-[640px]!"
          showCloseButton={false}
        >
          <DialogHeader className="sr-only">
            <DialogTitle>{t(($) => $.title)}</DialogTitle>
            <DialogDescription>{t(($) => $.description)}</DialogDescription>
          </DialogHeader>
          {/* The desktop shell survives workspace navigation. Reset its query,
              selection and pending searches together with their workspace.
              Base UI's Portal keeps the body mounted through the exit animation
              and unmounts it once closed, so no `{open && ...}` gate is needed. */}
          <PaletteBody
            key={workspaceSlug}
            active={open}
            launchPage={launchPage}
            close={close}
            openHostDialog={openHostDialog}
          />
        </DialogContent>
      </Dialog>
      <PaletteHostDialogs dialog={hostDialog} onClose={() => setHostDialog(null)} />
      <IssueShortcuts />
    </>
  );
}

function PaletteBody({
  active,
  launchPage,
  close,
  openHostDialog,
}: {
  active: boolean;
  /** The page an issue shortcut opened the palette on (`openPalettePage`). */
  launchPage: PaletteStackEntry | null;
  close: () => void;
  openHostDialog: (dialog: PaletteHostDialog) => void;
}) {
  const intentNavigate = useIntentNavigate();
  const [pageQuery, setPageQuery] = useState("");
  const [stack, setStack] = useState<PaletteStackEntry[]>(() => (launchPage ? [launchPage] : []));

  // The body mounts as the palette opens, so the launch page is its first
  // page; one that arrives while it is still mounted (reopened mid-exit)
  // replaces the stack.
  useEffect(() => {
    if (!launchPage) return;
    clearPendingPalettePage();
    setStack((prev) => (prev.length === 1 && prev[0] === launchPage ? prev : [launchPage]));
    setPageQuery("");
  }, [launchPage]);

  // cmdk's onSelect carries no event, so the activating gesture's modifiers
  // are recorded on the capture phase (click on a result, Enter in the input)
  // and consumed by the select handlers below: cmd+click / cmd+Enter opens a
  // result in a new tab instead of navigating in place.
  const pendingIntentRef = useRef<LinkClickIntent>("push");
  const recordIntent = useCallback(
    (e: Pick<MouseEvent, "button" | "metaKey" | "ctrlKey" | "shiftKey">) => {
      pendingIntentRef.current = resolveClickIntent(e);
    },
    [],
  );
  const consumeIntent = useCallback(() => {
    const intent = pendingIntentRef.current;
    pendingIntentRef.current = "push";
    return intent;
  }, []);

  const push = useCallback((entry: PaletteStackEntry) => {
    setStack((prev) => [...prev, entry]);
    setPageQuery("");
  }, []);

  const api = useMemo<PaletteApi>(
    () => ({
      close,
      push,
      navigate: (href, onNavigateInPlace) => {
        close();
        const intent = consumeIntent();
        if (intent === "push") onNavigateInPlace?.();
        intentNavigate(href, intent);
      },
      openHostDialog,
    }),
    [close, consumeIntent, intentNavigate, openHostDialog, push],
  );

  // Close on single ESC — capture phase fires before base-ui Dialog's handlers
  useEffect(() => {
    if (!active) return;
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        close();
      }
    };
    document.addEventListener("keydown", handleEsc, true);
    return () => document.removeEventListener("keydown", handleEsc, true);
  }, [active, close]);

  return (
    <PaletteContext.Provider value={api}>
      <PaletteFrame
        active={active}
        stack={stack}
        setStack={setStack}
        pageQuery={pageQuery}
        setPageQuery={setPageQuery}
        recordIntent={recordIntent}
      />
    </PaletteContext.Provider>
  );
}

const PALETTE_NAV_DURATION = 0.14;
const ENABLED_ITEM = '[cmdk-item=""]:not([aria-disabled="true"])';

/** Keys cmdk moves the selection on: arrows, and ⌃N/⌃J/⌃P/⌃K. */
function isSelectionKey(e: Pick<KeyboardEvent, "key" | "ctrlKey">) {
  if (e.key === "ArrowDown" || e.key === "ArrowUp") return true;
  return e.ctrlKey && (e.key === "n" || e.key === "j" || e.key === "p" || e.key === "k");
}

/** Everything that reads the palette API lives under its provider. */
function PaletteFrame({
  active,
  stack,
  setStack,
  pageQuery,
  setPageQuery,
  recordIntent,
}: {
  active: boolean;
  stack: PaletteStackEntry[];
  setStack: React.Dispatch<React.SetStateAction<PaletteStackEntry[]>>;
  pageQuery: string;
  setPageQuery: (value: string) => void;
  recordIntent: (e: Pick<MouseEvent, "button" | "metaKey" | "ctrlKey" | "shiftKey">) => void;
}) {
  const { t } = useT("search");
  const reduceMotion = useReducedMotion();
  const entries = useCommandBarContextStore(selectCommandBarContexts);
  const contexts = useMemo(() => entries.map((entry) => entry.context), [entries]);
  const [rawQuery, setRawQuery] = useState("");
  const current = stack[stack.length - 1] ?? null;
  const inputValue = current ? pageQuery : rawQuery;

  // The selected row's value. cmdk picks the first row only when it is empty.
  const [selected, setSelected] = useState("");
  // cmdk picks the first enabled row when the input changes, while the new
  // query's server rows are still loading and disabled, and keeps that pick
  // when they land above it. Until the user moves the selection, the effect
  // below keeps it on the top row.
  const selectionMovedRef = useRef(false);
  const selectionScopeRef = useRef("");

  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const prevDepthRef = useRef(stack.length);
  const navDirectionRef = useRef<-1 | 0 | 1>(0);
  if (stack.length !== prevDepthRef.current) {
    navDirectionRef.current = stack.length > prevDepthRef.current ? 1 : -1;
    prevDepthRef.current = stack.length;
    // It names a row of the page left behind. Cleared before the new rows mount.
    setSelected("");
  }
  const navDirection = navDirectionRef.current;
  const pageKey = current ? `${stack.length}:${current.page.type}` : "root";
  const selectionScope = `${pageKey}\n${inputValue}`;
  if (selectionScope !== selectionScopeRef.current) {
    selectionScopeRef.current = selectionScope;
    selectionMovedRef.current = false;
  }

  const [listHeight, setListHeight] = useState<number | undefined>(undefined);
  const [animateHeight, setAnimateHeight] = useState(false);
  const activeRef = useRef(active);
  activeRef.current = active;

  // Keep list height smooth as filtering or subpage navigation changes the
  // rendered rows, without animating from 0 on initial open or collapsing
  // while the dialog exits.
  useLayoutEffect(() => {
    if (reduceMotion) {
      setListHeight(undefined);
      setAnimateHeight(false);
      return;
    }
    const content = contentRef.current;
    const list = listRef.current;
    if (!content || !list || typeof ResizeObserver === "undefined") return;

    let initialized = false;
    let rafId = 0;
    const syncHeight = () => {
      if (!activeRef.current) return;
      // offsetHeight ignores the dialog's zoom-in transform; a scaled
      // getBoundingClientRect() read would pin the list at ~95% height.
      const contentHeight = content.offsetHeight;
      if (contentHeight <= 0) return;
      const styles = getComputedStyle(list);
      const padY =
        (Number.parseFloat(styles.paddingTop) || 0) +
        (Number.parseFloat(styles.paddingBottom) || 0);
      const maxH = Number.parseFloat(styles.maxHeight);
      const rawNext = contentHeight + padY;
      const clamped = Number.isFinite(maxH) && maxH > 0 ? Math.min(rawNext, maxH) : rawNext;
      setListHeight(clamped);
      if (!initialized) {
        initialized = true;
        rafId = requestAnimationFrame(() => setAnimateHeight(true));
      }
    };

    syncHeight();
    const observer = new ResizeObserver(syncHeight);
    observer.observe(content);
    return () => {
      cancelAnimationFrame(rafId);
      observer.disconnect();
    };
  }, [reduceMotion]);

  // Reset scroll to the top and keep the caret at the end of any carried query
  // when drilling into or returning from a subpage.
  useLayoutEffect(() => {
    if (listRef.current) listRef.current.scrollTop = 0;
    if (navDirection === 0) return;
    const el = inputRef.current;
    if (!el) return;
    el.focus();
    const end = el.value.length;
    el.setSelectionRange(end, end);
  }, [pageKey, navDirection]);

  // Base UI's DialogPortal only mounts PaletteBody while open or exiting, so
  // keeping `active: true` does not fetch while closed and prevents recent
  // items and search results from clearing on frame 1 of the exit animation.
  const model = useRootModel(rawQuery, contexts, { active: true });

  useLayoutEffect(() => {
    const list = listRef.current;
    if (!active || !list || selectionMovedRef.current) return;
    const first = list.querySelector(ENABLED_ITEM);
    const value = first?.getAttribute("data-value");
    if (!first || !value || value === selected) return;
    setSelected(value);
    // cmdk scrolls only selections it makes itself, and this one lands first.
    list.scrollTop = 0;
    first.scrollIntoView({ block: "nearest" });
  }, [active, selected, model, current, pageQuery]);

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    // cmdk's root handler intercepts Home/End for list navigation; stop
    // propagation so the browser moves the text caret instead.
    if (e.key === "Home" || e.key === "End") {
      e.stopPropagation();
      return;
    }
    if (e.key === "Backspace" && current && pageQuery === "") {
      e.preventDefault();
      setStack((prev) => prev.slice(0, -1));
    }
  };

  return (
    <>
      <CommandPrimitive
        data-mc="command-palette"
        label={t(($) => $.title)}
        shouldFilter={false}
        value={selected}
        onValueChange={setSelected}
        onPointerDownCapture={recordIntent}
        onClickCapture={recordIntent}
        onKeyDownCapture={(e) => {
          if (e.key === "Enter") {
            recordIntent({ button: 0, metaKey: e.metaKey, ctrlKey: e.ctrlKey, shiftKey: e.shiftKey });
          }
          if (isSelectionKey(e)) selectionMovedRef.current = true;
        }}
        className="flex size-full flex-col overflow-hidden rounded-xl bg-popover text-popover-foreground"
      >
        <div className="flex items-center gap-2 border-b px-4 py-3">
          <SearchIcon className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          {current && (
            <motion.nav
              key={pageKey}
              aria-label={t(($) => $.breadcrumb_aria)}
              initial={reduceMotion ? false : { opacity: 0, x: -6 }}
              animate={{
                opacity: 1,
                x: 0,
                transition: { duration: reduceMotion ? 0 : PALETTE_NAV_DURATION, ease: UI_EASE_OUT },
              }}
              className="flex min-w-0 shrink items-center gap-1"
            >
              {current.crumbs.map((crumb, index) => (
                <Fragment key={`${index}:${crumb}`}>
                  {index > 0 && <ChevronRight className="size-3 shrink-0 text-muted-foreground" />}
                  <span className="max-w-40 truncate rounded-md bg-muted px-1.5 py-0.5 text-caption font-medium">
                    {crumb}
                  </span>
                </Fragment>
              ))}
            </motion.nav>
          )}
          <CommandPrimitive.Input
            ref={inputRef}
            autoFocus
            placeholder={current ? t(($) => $.page_placeholder) : t(($) => $.placeholder)}
            value={inputValue}
            onValueChange={(value) => (current ? setPageQuery(value) : setRawQuery(value))}
            onKeyDown={handleKeyDown}
            onFocus={(e) => {
              const end = e.currentTarget.value.length;
              e.currentTarget.setSelectionRange(end, end);
            }}
            className="min-w-0 flex-1 bg-transparent text-body outline-none placeholder:text-muted-foreground"
          />
          <ShortcutKeycaps shortcut={ESCAPE} className="hidden shrink-0 sm:inline-flex" />
        </div>

        <CommandPrimitive.List
          ref={listRef}
          // cmdk selects the enabled row under a moving pointer, and nothing else.
          onPointerMove={(e) => {
            if ((e.target as Element).closest(ENABLED_ITEM)) selectionMovedRef.current = true;
          }}
          style={listHeight !== undefined ? { height: listHeight } : undefined}
          className={cn(
            "max-h-[min(440px,55vh)] overflow-y-auto overflow-x-hidden py-1",
            animateHeight && "transition-[height] duration-[140ms] ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:transition-none",
          )}
        >
          <div ref={contentRef}>
            <motion.div
              key={pageKey}
              initial={reduceMotion || navDirection === 0 ? false : { opacity: 0, x: navDirection * 10 }}
              animate={{ opacity: 1, x: 0 }}
              transition={{
                duration: reduceMotion || navDirection === 0 ? 0 : PALETTE_NAV_DURATION,
                ease: UI_EASE_OUT,
              }}
            >
              {current ? (
                <PageView page={current.page} query={pageQuery} />
              ) : (
                <RootView model={model} rawQuery={rawQuery} />
              )}
            </motion.div>
          </div>
        </CommandPrimitive.List>

        <PaletteFooter mode={current ? "page" : "root"} />
      </CommandPrimitive>
    </>
  );
}
