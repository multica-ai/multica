"use client";

/**
 * DynamicBlock — the frame shared by every fence that renders in place
 * (MUL-7649): ```html previews and ```mermaid diagrams.
 *
 * A dynamic block is part of the message body. That is what separates it from
 * an attachment, which is a file and shows as a card that opens the viewer. So
 * the frame reads as content, not as a file: an always-visible title bar (kind
 * icon, title, kind chip; the Preview | Source, fullscreen and copy actions
 * fade in on hover or focus, MUL-7733) over a body that takes its content's
 * height, collapsed past DYNAMIC_BLOCK_COLLAPSE_AT_PX behind a fade and
 * "Show all". Loading and error states live inside the same frame, so a block
 * that fails never breaks the comment around it.
 *
 * The kinds supply only the preview; the frame owns the chrome, the source
 * view and the error panel.
 */

import {
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
  type Ref,
} from "react";
import {
  AppWindow,
  Check,
  ChevronDown,
  CodeXml,
  Copy,
  Maximize2,
  TriangleAlert,
  Workflow,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { CodeBlockStatic } from "./code-block-static";

export type DynamicBlockKind = "html" | "mermaid";

/** Content taller than this is collapsed until the reader asks for all of it. */
export const DYNAMIC_BLOCK_COLLAPSE_AT_PX = 480;
/** Floor for a preview body, so a loading or near-empty block keeps a shape. */
export const DYNAMIC_BLOCK_MIN_BODY_PX = 120;
/** Title bar (36px) plus the frame's top and bottom borders. */
export const DYNAMIC_BLOCK_CHROME_PX = 38;

// Kind names are identifiers (the fence language), not translatable copy.
const KIND_LABEL: Record<DynamicBlockKind, string> = {
  html: "HTML",
  mermaid: "Mermaid",
};

const KIND_ICON: Record<DynamicBlockKind, typeof AppWindow> = {
  html: AppWindow,
  mermaid: Workflow,
};

// Highlighting language for the source view. Mermaid has no lowlight grammar.
const SOURCE_LANGUAGE: Record<DynamicBlockKind, string | undefined> = {
  html: "xml",
  mermaid: undefined,
};

export interface DynamicBlockError {
  message: string;
  /** 1-based source line, when the error carries one. */
  line?: number;
}

type DynamicBlockView = "preview" | "source";

interface DynamicBlockProps {
  kind: DynamicBlockKind;
  /** From the fence info string (```html title="…"). Falls back to the kind name. */
  title?: string | null;
  /** The fence body: shown in the source view and copied by the copy button. */
  source: string;
  error?: DynamicBlockError | null;
  /** Shows the fullscreen button when set. */
  onFullscreen?: () => void;
  /** Focus returns here when the kind's fullscreen surface closes. */
  fullscreenButtonRef?: Ref<HTMLButtonElement>;
  /**
   * The rendered content. It stays mounted while the source view is showing,
   * so switching back is instant; `active` says whether it is on screen.
   */
  preview: (options: { active: boolean }) => ReactNode;
  className?: string;
}

export function DynamicBlock({
  kind,
  title,
  source,
  error,
  onFullscreen,
  fullscreenButtonRef,
  preview,
  className,
}: DynamicBlockProps) {
  const { t } = useT("editor");
  const [view, setView] = useState<DynamicBlockView>("preview");
  const [copied, setCopied] = useState(false);
  const Icon = KIND_ICON[kind];
  const kindLabel = KIND_LABEL[kind];
  const trimmedTitle = title?.trim();
  // The actions stay up while they carry state the reader has to see: the
  // source view (Preview is how to get back) and the copy confirmation.
  const actionsPinned = view === "source" || copied;

  const handleCopy = async () => {
    if (await copyText(source)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <Tabs
      value={view}
      onValueChange={(value) => setView(value as DynamicBlockView)}
      data-dynamic-block={kind}
      // No outer margin: the lazy shell around the block owns that spacing.
      className={cn(
        "group/dynamic-block gap-0 overflow-hidden rounded-lg border bg-surface shadow-[var(--surface-shadow)]",
        error && view === "preview" && "border-destructive/35",
        className,
      )}
    >
      <div className="flex h-9 items-center gap-2 border-b pr-1.5 pl-3">
        <Icon aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
        {/* With no title, the kind name stands in and the chip would repeat it. */}
        <span
          className={cn(
            "min-w-0 truncate text-label font-medium",
            !trimmedTitle && "text-muted-foreground",
          )}
        >
          {trimmedTitle || kindLabel}
        </span>
        {trimmedTitle && (
          <span className="inline-flex h-[18px] shrink-0 items-center rounded-sm bg-secondary px-1.5 text-micro text-muted-foreground">
            {kindLabel}
          </span>
        )}
        <span className="flex-1" />
        {/* The title identifies the block and always shows; the actions fade
            in while the block is hovered or holds focus (MUL-7733). Opacity,
            not display, so they keep their place in the bar and in the tab
            order. Touch has no hover to reveal them, so there they always
            show. */}
        <div
          data-dynamic-block-actions=""
          className={cn(
            "flex shrink-0 items-center gap-2 transition-opacity duration-150 ease-out",
            !actionsPinned &&
              "group-focus-within/dynamic-block:opacity-100 group-hover/dynamic-block:opacity-100 [@media(hover:hover)]:opacity-0",
          )}
        >
          <TabsList
            aria-label={t(($) => $.dynamic_block.view)}
            className="mr-1 shrink-0 gap-0.5 rounded-md p-0.5 group-data-horizontal/tabs:h-[26px]"
          >
            <TabsTrigger value="preview" className="rounded-sm px-2 text-caption">
              {t(($) => $.dynamic_block.preview)}
            </TabsTrigger>
            <TabsTrigger value="source" className="rounded-sm px-2 text-caption">
              {t(($) => $.dynamic_block.source)}
            </TabsTrigger>
          </TabsList>
          {onFullscreen && (
            <Button
              ref={fullscreenButtonRef}
              type="button"
              variant="ghost"
              size="icon-xs"
              className="text-muted-foreground"
              onClick={onFullscreen}
              title={t(($) => $.code_block.fullscreen)}
              aria-label={t(($) => $.code_block.fullscreen)}
            >
              <Maximize2 />
            </Button>
          )}
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            className="text-muted-foreground"
            onClick={handleCopy}
            title={t(($) => $.dynamic_block.copy_source)}
            aria-label={t(($) => $.dynamic_block.copy_source)}
          >
            {copied ? <Check /> : <Copy />}
          </Button>
        </div>
      </div>
      <TabsContent value="preview" keepMounted>
        {error && (
          <DynamicBlockErrorPanel
            kind={kind}
            error={error}
            onViewSource={() => setView("source")}
          />
        )}
        <div hidden={!!error}>
          <CollapsibleBody>{preview({ active: view === "preview" && !error })}</CollapsibleBody>
        </div>
      </TabsContent>
      <TabsContent value="source">
        <CollapsibleBody>
          <CodeBlockStatic
            language={SOURCE_LANGUAGE[kind]}
            body={source}
            className="!m-0 !rounded-none !bg-muted"
          />
        </CollapsibleBody>
      </TabsContent>
    </Tabs>
  );
}

/**
 * Takes its content's height, collapsed past DYNAMIC_BLOCK_COLLAPSE_AT_PX
 * with a fade and "Show all". The content keeps laying out at full height
 * underneath, so expanding only lifts the clip — an HTML preview or diagram
 * is not rebuilt.
 */
function CollapsibleBody({ children }: { children: ReactNode }) {
  const { t } = useT("editor");
  const contentRef = useRef<HTMLDivElement>(null);
  const [contentHeight, setContentHeight] = useState(0);
  const [expanded, setExpanded] = useState(false);

  const measure = useCallback(() => {
    const el = contentRef.current;
    if (el) setContentHeight(el.offsetHeight);
  }, []);

  useLayoutEffect(() => {
    const el = contentRef.current;
    if (!el) return;
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [measure]);

  const collapsed = !expanded && contentHeight > DYNAMIC_BLOCK_COLLAPSE_AT_PX;

  return (
    <div
      className="relative overflow-hidden"
      data-collapsed={collapsed ? "" : undefined}
      style={collapsed ? { maxHeight: DYNAMIC_BLOCK_COLLAPSE_AT_PX } : undefined}
    >
      <div ref={contentRef}>{children}</div>
      {collapsed && (
        <>
          <div className="pointer-events-none absolute inset-x-0 bottom-0 h-24 bg-gradient-to-b from-transparent to-surface" />
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full bg-surface shadow-[var(--surface-shadow)]"
            onClick={() => setExpanded(true)}
          >
            <ChevronDown />
            {t(($) => $.dynamic_block.show_all)}
          </Button>
        </>
      )}
    </div>
  );
}

function DynamicBlockErrorPanel({
  kind,
  error,
  onViewSource,
}: {
  kind: DynamicBlockKind;
  error: DynamicBlockError;
  onViewSource: () => void;
}) {
  const { t } = useT("editor");
  const [copied, setCopied] = useState(false);
  const detail = error.line
    ? t(($) => $.dynamic_block.error_at_line, { message: error.message, line: error.line })
    : error.message;

  const handleCopyError = async () => {
    if (await copyText(detail)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div role="alert" className="flex gap-3 p-5 text-label">
      <TriangleAlert aria-hidden className="mt-0.5 size-4 shrink-0 text-destructive" />
      <div className="min-w-0 flex-1">
        <div className="font-semibold">
          {kind === "html"
            ? t(($) => $.dynamic_block.error_html)
            : t(($) => $.dynamic_block.error_mermaid)}
        </div>
        {/* The parser or script message is the only clue about what to fix. */}
        {detail && (
          <div className="mt-0.5 font-mono text-caption break-words whitespace-pre-wrap text-muted-foreground">
            {detail}
          </div>
        )}
        <div className="mt-2.5 flex flex-wrap gap-2">
          <Button type="button" variant="outline" size="sm" onClick={onViewSource}>
            <CodeXml />
            {t(($) => $.dynamic_block.view_source)}
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={handleCopyError}>
            {copied ? <Check /> : <Copy />}
            {t(($) => $.dynamic_block.copy_error)}
          </Button>
        </div>
      </div>
    </div>
  );
}

/**
 * Loading state for a preview body: a quiet page outline at the height the
 * block will most likely take, so the finished render does not move the page.
 */
export function DynamicBlockSkeleton({ className }: { className?: string }) {
  const { t } = useT("editor");
  return (
    <div className={cn("flex h-full flex-col gap-3 px-5 py-[18px]", className)}>
      <div className="h-3 w-[38%] rounded-sm bg-muted" />
      <div className="min-h-3 flex-1 rounded-sm bg-muted" />
      <div className="flex items-center gap-1.5 text-caption text-muted-foreground">
        <Spinner aria-hidden className="size-3.5" />
        {t(($) => $.dynamic_block.rendering)}
      </div>
    </div>
  );
}
