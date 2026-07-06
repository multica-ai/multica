"use client";

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import {
  Maximize2,
  Download,
  Link as LinkIcon,
  Paperclip,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
// CEREBRO-PATCH(zoomable-image-preview): pinch/wheel zoom on the image lightbox (TECH-3695)
// CEREBRO-PATCH(image-chip-thumbnail): FIR-2034 — compact image card behind cerebro_attachment_chips.
import { ZoomableImage, AttachmentChip } from "@multica/cerebro-ui";
import { useFlagValue } from "@multica/cerebro-feature-flags";
// CEREBRO-PATCH(image-view-gallery): FIR-2710 — inline images join the surface image gallery.
import { useGalleryImage } from "@multica/cerebro-attachments/views";
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
      {/* CEREBRO-PATCH(zoomable-image-preview): TECH-3695 — pinch/wheel zoom */}
      <ZoomableImage
        src={src}
        alt={alt}
        viewportClassName="h-[90vh] w-[90vw]"
        className="rounded-lg"
      />
    </div>,
    document.body,
  );
}

// ---------------------------------------------------------------------------
// Image NodeView — renders img with hover toolbar + lightbox
// ---------------------------------------------------------------------------

function ImageView({ node, editor, selected, deleteNode, getPos }: NodeViewProps) {
  const { t } = useT("editor");
  const src = node.attrs.src as string;
  const alt = (node.attrs.alt as string) || "";
  const title = node.attrs.title as string | undefined;
  const uploading = node.attrs.uploading as boolean;
  const { openByUrl } = useAttachmentDownloadResolver();

  const [lightbox, setLightbox] = useState(false);
  const isEditable = editor.isEditable;
  // CEREBRO-PATCH(image-view-gallery): FIR-2710 — inline images open the surface gallery. Registration is harmless in an editable editor: only the Maximize button / non-editor figure click call handleView, never ProseMirror selection.
  const gallery = useGalleryImage(uploading ? null : { src, alt, downloadHref: src });
  const handleView = () => (gallery.enabled ? gallery.open() : setLightbox(true));
  // CEREBRO-PATCH(image-chip-thumbnail): FIR-2034 — compact thumbnail card + existing lightbox instead of the large figure.
  const chipsEnabled = useFlagValue("cerebro_attachment_chips");
  if (chipsEnabled)
    return (
      // CEREBRO-PATCH(image-view-gallery): FIR-2710 — register this image with the surface gallery via ref.
      <NodeViewWrapper as="span" className="image-node" ref={gallery.ref}>
        <AttachmentChip filename={alt || "image"} thumbnailSrc={src} onActivate={uploading ? undefined : handleView} activateLabel={t(($) => $.image.view)} onRemove={isEditable ? () => deleteNode() : undefined} uploading={uploading} />
        {lightbox && <ImageLightbox src={src} alt={alt} onClose={() => setLightbox(false)} />}
      </NodeViewWrapper>
    );

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

  const handleConvertToFileCard = () => {
    const pos = typeof getPos === "function" ? getPos() : undefined;
    if (typeof pos !== "number") return;
    editor
      .chain()
      .focus()
      .insertContentAt(
        { from: pos, to: pos + node.nodeSize },
        {
          type: "fileCard",
          attrs: { href: src, filename: alt || "image", fileSize: 0 },
        },
      )
      .run();
  };

  return (
    // CEREBRO-PATCH(image-view-gallery): FIR-2710 — register this image with the surface gallery via ref.
    <NodeViewWrapper className="image-node" ref={gallery.ref}>
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
                onClick={handleConvertToFileCard}
                title="Convert to attachment"
              >
                <Paperclip className="size-3.5" />
              </button>
            )}
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
