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
  Download,
  ExternalLink,
  Eye,
  Maximize2,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { HtmlPreviewBody } from "./html-preview-body";
import { CodeBlockStatic } from "./code-block-static";
import {
  HtmlArtifactPreviewModal,
  downloadHtmlArtifact,
  storeHtmlArtifactPreview,
} from "./html-artifact-preview";

const CODE_BLOCK_IFRAME_HEIGHT = "h-[480px]";
const HTML_ARTIFACT_FILENAME = "html-artifact.html";

// Label shown in the code-block header. Not a translatable string — it's a
// language identifier (matches the `lang === "html"` token below).
const HTML_LANGUAGE_LABEL = "html";

interface HtmlBlockPreviewProps {
  html: string;
  className?: string;
}

export function HtmlBlockPreview({ html, className }: HtmlBlockPreviewProps) {
  const { t } = useT("editor");
  const slug = useWorkspaceSlug();
  const navigation = useNavigation();
  const [view, setView] = useState<"preview" | "source">("preview");
  const [previewOpen, setPreviewOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    if (!html) return;
    try {
      await navigator.clipboard.writeText(html);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard failures are user-recoverable (click again, or copy
      // manually from the source view) — no need for a toast here.
    }
  };

  const toggleView = () =>
    setView((v) => (v === "preview" ? "source" : "preview"));

  const handleOpenInNewTab = () => {
    if (!slug || !html) return;
    const key = storeHtmlArtifactPreview(html, HTML_ARTIFACT_FILENAME);
    if (!key) return;
    const path = paths.workspace(slug).htmlArtifactPreview(key);
    if (navigation.openInNewTab) {
      navigation.openInNewTab(path, HTML_ARTIFACT_FILENAME, { activate: true });
      return;
    }
    const url = navigation.getShareableUrl(path);
    window.open(url, "_blank", "noopener,noreferrer");
  };

  const handleDownload = () => {
    downloadHtmlArtifact(html, HTML_ARTIFACT_FILENAME);
  };

  return (
    <div className={cn("code-block-wrapper group/code relative my-2 select-text", className)}>
      <div
        className="code-block-header pointer-events-none absolute top-0 right-0 z-10 flex select-none items-center gap-1.5 px-2 py-1.5 opacity-0 transition-opacity group-hover/code:opacity-100"
      >
        <span className="text-xs text-muted-foreground select-none">{HTML_LANGUAGE_LABEL}</span>
        <button
          type="button"
          onMouseDown={(e) => e.preventDefault()}
          onClick={toggleView}
          className="pointer-events-auto flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
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
          <button
            type="button"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => setPreviewOpen(true)}
            className="pointer-events-auto flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            title={t(($) => $.code_block.fullscreen)}
            aria-label={t(($) => $.code_block.fullscreen)}
          >
            <Maximize2 className="h-3.5 w-3.5" />
          </button>
        )}
        {slug && (
          <button
            type="button"
            onMouseDown={(e) => e.preventDefault()}
            onClick={handleOpenInNewTab}
            className="pointer-events-auto flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            title={t(($) => $.attachment.open_in_new_tab)}
            aria-label={t(($) => $.attachment.open_in_new_tab)}
          >
            <ExternalLink className="h-3.5 w-3.5" />
          </button>
        )}
        <button
          type="button"
          onMouseDown={(e) => e.preventDefault()}
          onClick={handleDownload}
          className="pointer-events-auto flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          title={t(($) => $.image.download)}
          aria-label={t(($) => $.image.download)}
        >
          <Download className="h-3.5 w-3.5" />
        </button>
        <button
          type="button"
          onMouseDown={(e) => e.preventDefault()}
          onClick={handleCopy}
          className="pointer-events-auto flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
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
      <HtmlArtifactPreviewModal
        html={html}
        filename={HTML_ARTIFACT_FILENAME}
        open={previewOpen}
        canOpenInNewTab={!!slug}
        onClose={() => setPreviewOpen(false)}
        onOpenInNewTab={handleOpenInNewTab}
        onDownload={handleDownload}
      />
    </div>
  );
}
