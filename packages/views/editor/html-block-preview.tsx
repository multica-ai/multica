"use client";

/**
 * HtmlBlockPreview — readonly rendering of fenced ```html code blocks.
 *
 * Default view is "preview" (iframe) per the V2 plan; user can flip to
 * "source" to see the highlighted markup and Copy it. Maximize opens the
 * same iframe in a full-screen Dialog.
 *
 * Mounted by ReadonlyContent's `code` renderer for `lang === "html"`. The
 * `pre` renderer in ReadonlyContent recognizes this component by reference
 * and unwraps it from the default `<pre>` envelope, matching the same
 * two-layer trick already used for MermaidDiagram.
 *
 * NOT used in the editable Tiptap NodeView — that path must keep
 * `<NodeViewContent as="code" />` so the user can continue typing.
 */

import { useState } from "react";
import {
  Check,
  Code as CodeIcon,
  Copy,
  Pencil,
  RotateCcw,
  Eye,
  Maximize2,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import {
  Dialog,
  DialogContent,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../i18n";
import { CodeBlockStatic } from "./code-block-static";
import { HtmlPreviewBody } from "./html-preview-body";

const CODE_BLOCK_IFRAME_HEIGHT = "h-[480px]";

/**
 * Pixel twin of CODE_BLOCK_IFRAME_HEIGHT. The preview iframe is a fixed height,
 * so the near-viewport lazy shell (rich-content/lazy-rich-block.tsx) can
 * reserve exactly the space this component will occupy and mount with zero
 * layout shift. Keep the two in sync.
 */
export const HTML_BLOCK_PREVIEW_HEIGHT_PX = 480;

// Label shown in the code-block header. Not a translatable string — it's a
// language identifier (matches the `lang === "html"` token below).
const HTML_LANGUAGE_LABEL = "html";

interface HtmlBlockPreviewProps {
  html: string;
  className?: string;
}

export function HtmlBlockPreview({ html, className }: HtmlBlockPreviewProps) {
  const { t } = useT("editor");
  const [view, setView] = useState<"preview" | "source">("preview");
  const [copied, setCopied] = useState(false);
  const [dialogMode, setDialogMode] = useState<"preview" | "edit" | null>(null);
  const [draftHtml, setDraftHtml] = useState(html);
  const isDirty = draftHtml !== html;

  const handleCopy = async () => {
    if (!html) return;
    if (await copyText(html)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const toggleView = () =>
    setView((v) => (v === "preview" ? "source" : "preview"));

  const openEditor = () => {
    setDraftHtml(html);
    setDialogMode("edit");
  };

  const handleCopyDraft = async () => {
    if (!draftHtml) return;
    if (await copyText(draftHtml)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div className={cn("code-block-wrapper group/code relative my-3", className)}>
      <div
        className="absolute top-0 right-0 z-10 flex items-center gap-1.5 px-2 py-1.5 opacity-0 transition-opacity group-hover/code:opacity-100 focus-within:opacity-100"
      >
        <span className="text-caption text-muted-foreground select-none">{HTML_LANGUAGE_LABEL}</span>
        <button
          type="button"
          onClick={toggleView}
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          title={
            view === "preview"
              ? t(($) => $.code_block.show_source)
              : t(($) => $.code_block.show_preview)
          }
          aria-label={
            view === "preview"
              ? t(($) => $.code_block.show_source)
              : t(($) => $.code_block.show_preview)
          }
        >
          {view === "preview" ? (
            <CodeIcon className="h-3.5 w-3.5" />
          ) : (
            <Eye className="h-3.5 w-3.5" />
          )}
        </button>
        {view === "preview" && (
          <>
            <button
              type="button"
              onClick={openEditor}
              className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
              title={t(($) => $.code_block.edit_preview)}
              aria-label={t(($) => $.code_block.edit_preview)}
            >
              <Pencil className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              onClick={() => setDialogMode("preview")}
              className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
              title={t(($) => $.code_block.fullscreen)}
              aria-label={t(($) => $.code_block.fullscreen)}
            >
              <Maximize2 className="h-3.5 w-3.5" />
            </button>
          </>
        )}
        <button
          type="button"
          onClick={handleCopy}
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          title={t(($) => $.code_block.copy_code)}
          aria-label={t(($) => $.code_block.copy_code)}
        >
          {copied ? (
            <Check className="h-3.5 w-3.5" />
          ) : (
            <Copy className="h-3.5 w-3.5" />
          )}
        </button>
      </div>
      {view === "preview" ? (
        <HtmlPreviewBody
          source={{ kind: "inline", html }}
          title="HTML preview"
          className={CODE_BLOCK_IFRAME_HEIGHT}
        />
      ) : (
        <CodeBlockStatic language="xml" body={html} />
      )}
      <Dialog
        open={dialogMode !== null}
        onOpenChange={(open) => {
          if (!open) setDialogMode(null);
        }}
      >
        <DialogContent
          className="!max-w-6xl !h-[min(90vh,calc(100vh-2rem))] w-full p-0 gap-0 overflow-hidden"
          aria-label={
            dialogMode === "edit"
              ? t(($) => $.code_block.live_workspace)
              : t(($) => $.code_block.fullscreen)
          }
        >
          {dialogMode === "edit" ? (
            <div className="flex h-full min-h-0 flex-col bg-background">
              <div className="flex min-h-12 items-center gap-3 border-b border-border px-4 pr-12">
                <div className="min-w-0 flex-1">
                  <p className="truncate text-body font-medium text-foreground">
                    {t(($) => $.code_block.live_workspace)}
                  </p>
                  <p className="text-caption text-muted-foreground">
                    {isDirty
                      ? t(($) => $.code_block.local_draft)
                      : t(($) => $.code_block.original)}
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => setDraftHtml(html)}
                  disabled={!isDirty}
                  className="flex h-8 items-center gap-1.5 rounded-md px-2.5 text-caption text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:pointer-events-none disabled:opacity-40"
                  aria-label={t(($) => $.code_block.reset_changes)}
                >
                  <RotateCcw className="h-3.5 w-3.5" />
                  <span className="hidden sm:inline">
                    {t(($) => $.code_block.reset_changes)}
                  </span>
                </button>
                <button
                  type="button"
                  onClick={handleCopyDraft}
                  className="flex h-8 items-center gap-1.5 rounded-md bg-foreground px-2.5 text-caption text-background transition-opacity hover:opacity-85"
                  aria-label={t(($) => $.code_block.copy_changes)}
                >
                  {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                  <span className="hidden sm:inline">
                    {t(($) => $.code_block.copy_changes)}
                  </span>
                </button>
              </div>
              <div className="grid min-h-0 flex-1 grid-rows-[minmax(220px,0.8fr)_minmax(260px,1.2fr)] overflow-y-auto overscroll-contain md:grid-cols-[minmax(320px,0.85fr)_minmax(0,1.15fr)] md:grid-rows-1 md:overflow-hidden">
                <div className="min-h-0 border-b border-border bg-muted/20 md:border-r md:border-b-0">
                  <textarea
                    value={draftHtml}
                    onChange={(event) => setDraftHtml(event.target.value)}
                    aria-label={t(($) => $.code_block.editor_label)}
                    className="h-full min-h-[220px] w-full resize-none bg-transparent p-4 font-mono text-caption leading-relaxed text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
                    spellCheck={false}
                  />
                </div>
                <div className="min-h-0 bg-background">
                  <HtmlPreviewBody
                    source={{ kind: "inline", html: draftHtml }}
                    title={t(($) => $.code_block.live_workspace)}
                    className="h-full min-h-[260px] w-full"
                    iframeClassName="rounded-none border-0"
                  />
                </div>
              </div>
            </div>
          ) : (
            <HtmlPreviewBody
              source={{ kind: "inline", html }}
              title="HTML preview"
              className="h-full w-full"
              iframeClassName="rounded-none border-0"
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
