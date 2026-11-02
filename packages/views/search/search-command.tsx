"use client";

import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ChevronRight, SearchIcon, Sparkles } from "@multica/ui/icons";
import { Command as CommandPrimitive } from "cmdk";
import { selectCommandBarContexts, useCommandBarContextStore } from "@multica/core/command-bar";
import { createShortcutChord } from "@multica/core/shortcuts";
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
import { PageView } from "./pages/page-view";
import { RootView } from "./root-view";
import { useDelegateSubmit } from "./delegate/use-delegate";
import { useRootModel } from "./use-root-model";
import { useSearchStore } from "./search-store";
import type { DelegateActor, PaletteHostDialog, PaletteStackEntry } from "./types";

const ESCAPE = createShortcutChord("Escape");

/**
 * ⌘K. The shell owns the dialog, the input, the page stack and the footer;
 * what the palette offers comes from the pages on screen (registered contexts)
 * and the search over workspace objects.
 */
export function SearchCommand() {
  const { t } = useT("search");
  const open = useSearchStore((s) => s.open);
  const setOpen = useSearchStore((s) => s.setOpen);
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
          {open && <PaletteBody close={close} openHostDialog={openHostDialog} />}
        </DialogContent>
      </Dialog>
      <PaletteHostDialogs dialog={hostDialog} onClose={() => setHostDialog(null)} />
    </>
  );
}

function PaletteBody({
  close,
  openHostDialog,
}: {
  close: () => void;
  openHostDialog: (dialog: PaletteHostDialog) => void;
}) {
  const intentNavigate = useIntentNavigate();
  const [pageQuery, setPageQuery] = useState("");
  const [stack, setStack] = useState<PaletteStackEntry[]>([]);

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
      navigate: (href) => {
        close();
        intentNavigate(href, consumeIntent());
      },
      openHostDialog,
    }),
    [close, consumeIntent, intentNavigate, openHostDialog, push],
  );

  // Close on single ESC — capture phase fires before base-ui Dialog's handlers
  useEffect(() => {
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        close();
      }
    };
    document.addEventListener("keydown", handleEsc, true);
    return () => document.removeEventListener("keydown", handleEsc, true);
  }, [close]);

  return (
    <PaletteContext.Provider value={api}>
      <PaletteFrame
        stack={stack}
        setStack={setStack}
        pageQuery={pageQuery}
        setPageQuery={setPageQuery}
        recordIntent={recordIntent}
      />
    </PaletteContext.Provider>
  );
}

/** Everything that reads the palette API lives under its provider. */
function PaletteFrame({
  stack,
  setStack,
  pageQuery,
  setPageQuery,
  recordIntent,
}: {
  stack: PaletteStackEntry[];
  setStack: React.Dispatch<React.SetStateAction<PaletteStackEntry[]>>;
  pageQuery: string;
  setPageQuery: (value: string) => void;
  recordIntent: (e: Pick<MouseEvent, "button" | "metaKey" | "ctrlKey" | "shiftKey">) => void;
}) {
  const { t } = useT("search");
  const entries = useCommandBarContextStore(selectCommandBarContexts);
  const contexts = useMemo(() => entries.map((entry) => entry.context), [entries]);
  const [rawQuery, setRawQuery] = useState("");
  const [dismissedDelegateFor, setDismissedDelegateFor] = useState<string | null>(null);
  const current = stack[stack.length - 1] ?? null;
  const inputValue = current ? pageQuery : rawQuery;

  const model = useRootModel(rawQuery, contexts, { active: true, dismissedDelegateFor });
  const submitDelegate = useDelegateSubmit();
  const onDelegate = useCallback(
    (actor: DelegateActor, prompt: string) => void submitDelegate(actor, prompt, model.attachments),
    [model.attachments, submitDelegate],
  );

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
      return;
    }
    // ⇧↵ treats an input that read as an instruction as a plain search.
    if (!current && e.key === "Enter" && e.shiftKey && model.delegate) {
      e.preventDefault();
      setDismissedDelegateFor(rawQuery);
    }
  };

  const delegate = !current ? model.delegate : null;

  return (
    <>
      <CommandPrimitive
        key={stack.length}
        label={t(($) => $.title)}
        shouldFilter={false}
        onPointerDownCapture={recordIntent}
        onClickCapture={recordIntent}
        onKeyDownCapture={(e) => {
          if (e.key === "Enter") {
            recordIntent({ button: 0, metaKey: e.metaKey, ctrlKey: e.ctrlKey, shiftKey: e.shiftKey });
          }
        }}
        className="flex size-full flex-col overflow-hidden rounded-xl bg-popover text-popover-foreground"
      >
        <div className="flex items-center gap-2 border-b px-4 py-3">
          {delegate ? (
            <Sparkles className="size-4 shrink-0 text-brand" aria-hidden="true" />
          ) : (
            <SearchIcon className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          )}
          {current && (
            <nav aria-label={t(($) => $.breadcrumb_aria)} className="flex min-w-0 shrink items-center gap-1">
              {current.crumbs.map((crumb, index) => (
                <Fragment key={`${index}:${crumb}`}>
                  {index > 0 && <ChevronRight className="size-3 shrink-0 text-muted-foreground" />}
                  <span className="max-w-40 truncate rounded-md bg-muted px-1.5 py-0.5 text-caption font-medium">
                    {crumb}
                  </span>
                </Fragment>
              ))}
            </nav>
          )}
          <CommandPrimitive.Input
            autoFocus
            placeholder={current ? t(($) => $.page_placeholder) : t(($) => $.placeholder)}
            value={inputValue}
            onValueChange={(value) => (current ? setPageQuery(value) : setRawQuery(value))}
            onKeyDown={handleKeyDown}
            // The input remounts when a page opens or closes; keep the caret
            // after text carried across.
            onFocus={(e) => {
              const end = e.currentTarget.value.length;
              e.currentTarget.setSelectionRange(end, end);
            }}
            className="min-w-0 flex-1 bg-transparent text-body outline-none placeholder:text-muted-foreground"
          />
          {delegate && (
            <span className="hidden shrink-0 items-center gap-1 rounded-md border px-1.5 py-0.5 text-caption text-muted-foreground sm:inline-flex">
              <Sparkles className="size-3" aria-hidden="true" />
              {t(($) => $.delegate.detected)}
            </span>
          )}
          <ShortcutKeycaps shortcut={ESCAPE} className="hidden shrink-0 sm:inline-flex" />
        </div>

        <CommandPrimitive.List className="max-h-[min(440px,55vh)] overflow-y-auto overflow-x-hidden py-1">
          {current ? (
            <PageView page={current.page} query={pageQuery} onDelegate={onDelegate} />
          ) : (
            <RootView model={model} rawQuery={rawQuery} onDelegate={onDelegate} />
          )}
        </CommandPrimitive.List>

        <PaletteFooter
          mode={current ? "page" : delegate ? "delegate" : "root"}
          delegateName={delegate?.actor.name}
        />
      </CommandPrimitive>
    </>
  );
}
