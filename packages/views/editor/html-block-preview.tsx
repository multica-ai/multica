"use client";

/**
 * HtmlBlockPreview — a fenced ```html block rendered in place (MUL-7649).
 *
 * The HTML runs in a sandboxed iframe inside the shared DynamicBlock frame and
 * takes its own content's height, reported by the bridge in
 * utils/html-block-document.ts, instead of a fixed 480px that left short
 * charts floating in blank space. An uncaught script error replaces the
 * preview with an explanation; fullscreen still shows whatever did render.
 *
 * Reached through RichFenceBlock (rich-content/rich-code-block.tsx) for closed
 * fences only. NOT used in the editable Tiptap NodeView — that path must keep
 * `<NodeViewContent as="code" />` so the user can continue typing.
 */

import { useEffect, useReducer, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { Dialog, DialogContent } from "@multica/ui/components/ui/dialog";
import { useT } from "../i18n";
import { CodeBlockIframe } from "./code-block-iframe";
import {
  DYNAMIC_BLOCK_CHROME_PX,
  DYNAMIC_BLOCK_COLLAPSE_AT_PX,
  DYNAMIC_BLOCK_MIN_BODY_PX,
  DynamicBlock,
  DynamicBlockSkeleton,
  type DynamicBlockError,
} from "./dynamic-block";
import { useThemeVersion } from "./hooks/use-theme-version";
import {
  INITIAL_HTML_BLOCK_HEIGHT,
  buildHtmlBlockDocument,
  nextHtmlBlockHeight,
  readHtmlBlockMessage,
  readHtmlBlockTheme,
} from "./utils/html-block-document";
import { hashSource } from "./utils/source-hash";

const HEIGHT_CACHE_PREFIX = "multica:html-block:height:";

// A document that never reports (a Content-Security-Policy in the author's
// HTML can block the bridge) is shown at the old fixed height, scrolling
// inside, once it has loaded and stayed silent this long.
const BRIDGE_TIMEOUT_MS = 1000;
const UNMEASURED_HEIGHT_PX = DYNAMIC_BLOCK_COLLAPSE_AT_PX;

function readCachedHeight(html: string): number | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.sessionStorage.getItem(HEIGHT_CACHE_PREFIX + hashSource(html));
    const height = raw == null ? Number.NaN : Number(raw);
    return Number.isFinite(height) && height > 0 ? height : null;
  } catch {
    return null;
  }
}

function writeCachedHeight(html: string, height: number): void {
  if (typeof window === "undefined") return;
  try {
    window.sessionStorage.setItem(HEIGHT_CACHE_PREFIX + hashSource(html), String(height));
  } catch {
    // Quota exceeded or storage disabled: the block still renders, it just
    // reserves the default height next time.
  }
}

function bodyHeightPx(contentHeight: number): number {
  return Math.max(contentHeight, DYNAMIC_BLOCK_MIN_BODY_PX);
}

/** Reserved height before any render: the title bar plus the minimum body. */
export const HTML_BLOCK_DEFAULT_RESERVED_PX = DYNAMIC_BLOCK_CHROME_PX + DYNAMIC_BLOCK_MIN_BODY_PX;

/**
 * Height the near-viewport lazy shell (rich-content/lazy-rich-block.tsx)
 * reserves: the collapsed height this exact HTML had earlier in the session,
 * else the minimum. Unknown blocks reserve the minimum rather than a guess,
 * because the shell keeps its reservation — a guess that is too tall would
 * leave a permanent gap under a short chart.
 *
 * NOT safe to call during render (reads sessionStorage); see
 * useReservedHeightPx in rich-content/rich-code-block.tsx.
 */
export function reservedHtmlBlockHeightPx(html: string): number {
  const cached = readCachedHeight(html);
  if (cached == null) return HTML_BLOCK_DEFAULT_RESERVED_PX;
  return (
    DYNAMIC_BLOCK_CHROME_PX + Math.min(bodyHeightPx(cached), DYNAMIC_BLOCK_COLLAPSE_AT_PX)
  );
}

interface HtmlBlockPreviewProps {
  html: string;
  /** From the fence info string: ```html title="…". */
  title?: string | null;
}

export function HtmlBlockPreview({ html, title }: HtmlBlockPreviewProps) {
  const { t } = useT("editor");
  const hostRef = useRef<HTMLDivElement>(null);
  const themeVersion = useThemeVersion();
  const [srcDoc, setSrcDoc] = useState<string | null>(null);
  const [error, setError] = useState<DynamicBlockError | null>(null);
  const [fullscreen, setFullscreen] = useState(false);
  const frameTitle = t(($) => $.code_block.html_preview);

  // Built after mount: the theme tokens come from the host's computed style.
  // A theme switch rebuilds the document, which reruns its scripts — a chart
  // reads its colors when it draws, so this is the only way it can follow.
  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    setError(null);
    setSrcDoc(buildHtmlBlockDocument(html, readHtmlBlockTheme(getComputedStyle(host))));
  }, [html, themeVersion]);

  return (
    <div ref={hostRef} className="contents">
      <DynamicBlock
        kind="html"
        title={title}
        source={html}
        error={error}
        onFullscreen={() => setFullscreen(true)}
        preview={({ active }) => (
          <HtmlBlockBody
            key={html}
            html={html}
            srcDoc={srcDoc}
            title={frameTitle}
            active={active}
            onError={setError}
          />
        )}
      />
      <Dialog open={fullscreen} onOpenChange={setFullscreen}>
        <DialogContent
          className="!max-w-6xl !h-[min(90vh,calc(100vh-2rem))] w-full p-0 gap-0 overflow-hidden"
          aria-label={t(($) => $.code_block.fullscreen)}
        >
          {srcDoc != null && (
            <CodeBlockIframe
              html={srcDoc}
              title={frameTitle}
              heightClassName="h-full"
              className="rounded-none border-0"
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function HtmlBlockBody({
  html,
  srcDoc,
  title,
  active,
  onError,
}: {
  html: string;
  srcDoc: string | null;
  title: string;
  active: boolean;
  onError: (error: DynamicBlockError) => void;
}) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [measured, reportHeight] = useReducer(nextHtmlBlockHeight, INITIAL_HTML_BLOCK_HEIGHT);
  const [cachedHeight, setCachedHeight] = useState<number | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [timedOut, setTimedOut] = useState(false);

  // The listener is registered once; these refs give it the current values.
  const activeRef = useRef(active);
  const onErrorRef = useRef(onError);
  useEffect(() => {
    activeRef.current = active;
    onErrorRef.current = onError;
  });

  useEffect(() => {
    setCachedHeight(readCachedHeight(html));
  }, [html]);

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      // Only this block's own frame; every block shares the window.
      const frame = iframeRef.current;
      if (!frame || event.source !== frame.contentWindow) return;
      const message = readHtmlBlockMessage(event.data);
      if (!message) return;
      if (message.type === "error") {
        onErrorRef.current({ message: message.message, line: message.line || undefined });
        return;
      }
      // A hidden frame lays out at zero width; what it reports is not its
      // height. It reports again once it is shown and resized back.
      if (activeRef.current) reportHeight(message.height);
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, []);

  const height = measured.height;
  useEffect(() => {
    if (height != null && height > 0) writeCachedHeight(html, height);
  }, [html, height]);

  useEffect(() => {
    if (!loaded || height != null) return;
    const timer = setTimeout(() => setTimedOut(true), BRIDGE_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [loaded, height]);

  const ready = height != null || timedOut;
  const bodyHeight = bodyHeightPx(
    height ?? (timedOut ? UNMEASURED_HEIGHT_PX : (cachedHeight ?? DYNAMIC_BLOCK_MIN_BODY_PX)),
  );

  return (
    <div className="relative" style={{ height: bodyHeight }}>
      {srcDoc != null && (
        <CodeBlockIframe
          ref={iframeRef}
          html={srcDoc}
          title={title}
          heightClassName="h-full"
          // The frame is the block's body, so it draws no border of its own.
          // Transparent lets a document without a background sit on the
          // block's surface.
          className={cn("block rounded-none border-0 bg-transparent", !ready && "invisible")}
          onLoad={() => setLoaded(true)}
        />
      )}
      {!ready && (
        <div className="absolute inset-0">
          <DynamicBlockSkeleton />
        </div>
      )}
    </div>
  );
}
