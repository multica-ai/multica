"use client";

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import {
  Maximize2,
  Download,
  Link as LinkIcon,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { useAttachmentDownloadResolver } from "../attachment-download-context";

// ---------------------------------------------------------------------------
// Lightbox — full-screen image preview (ESC or click backdrop to close)
// ---------------------------------------------------------------------------

function ImageLightbox({
  src,
  alt,
  onClose,
}: {
  src: string;
  alt: string;
  onClose: () => void;
}) {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [onClose]);

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 cursor-zoom-out"
      onClick={onClose}
    >
      <img
        src={src}
        alt={alt}
        className="max-h-[90vh] max-w-[90vw] rounded-lg object-contain"
        onClick={(e) => e.stopPropagation()}
      />
    </div>,
    document.body,
  );
}

// ---------------------------------------------------------------------------
// Image NodeView — renders img with hover toolbar + lightbox
// ---------------------------------------------------------------------------

function ImageView({ node, editor, selected, deleteNode }: NodeViewProps) {
  const { t } = useT("editor");
  const src = node.attrs.src as string;
  const alt = (node.attrs.alt as string) || "";
  const title = node.attrs.title as string | undefined;
  const uploading = node.attrs.uploading as boolean;
  const { openByUrl } = useAttachmentDownloadResolver();

  const [lightbox, setLightbox] = useState(false);
  const isEditable = editor.isEditable;

  const handleView = () => setLightbox(true);

  const handleDownload = () => {
    // Cross-origin CDN images can't be fetched as blob (CORS),
    // and <a download> is ignored for cross-origin URLs.
    // Re-sign through the provider when the src maps to a known
    // attachment; otherwise just open externally.
    openByUrl(src);
  };

  const handleCopyLink = async () => {
    try {
      await navigator.clipboard.writeText(src);
      toast.success(t(($) => $.image.link_copied));
    } catch {
      toast.error(t(($) => $.image.copy_link_failed));
    }
  };

  return (
    <NodeViewWrapper className="image-node">
      <figure
        className={cn(
          "image-figure",
          selected && isEditable && "image-selected",
        )}
        contentEditable={false}
        onClick={!isEditable && !uploading ? handleView : undefined}
      >
        <img
          src={src}
          alt={alt}
          title={title || undefined}
          className={cn("image-content", uploading && "image-uploading")}
          draggable={false}
        />
        {!uploading && (
          <div
            className="image-toolbar"
            onMouseDown={(e) => e.stopPropagation()}
            onClick={(e) => e.stopPropagation()}
          >
            <button type="button" onClick={handleView} title={t(($) => $.image.view)}>
              <Maximize2 className="size-3.5" />
            </button>
            <button type="button" onClick={handleDownload} title={t(($) => $.image.download)}>
              <Download className="size-3.5" />
            </button>
            <button
              type="button"
              onClick={handleCopyLink}
              title={t(($) => $.image.copy_link)}
            >
              <LinkIcon className="size-3.5" />
            </button>
            {isEditable && (
              <button
                type="button"
                onClick={() => deleteNode()}
                title={t(($) => $.image.delete)}
              >
                <Trash2 className="size-3.5" />
              </button>
            )}
          </div>
        )}
      </figure>
      {lightbox && (
        <ImageLightbox
          src={src}
          alt={alt}
          onClose={() => setLightbox(false)}
        />
      )}
    </NodeViewWrapper>
  );
}

export { ImageView, ImageLightbox };
