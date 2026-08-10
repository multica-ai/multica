"use client";

// CEREBRO-PATCH(image-resize-controls): FIR-4699 Phase 5 — useRef for the figure.
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import {
  Maximize2,
  Download,
  Link as LinkIcon,
  Paperclip,
  Trash2,
  AlignLeft,
  AlignCenter,
  AlignRight,
} from "lucide-react";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { useIsMobile } from "@multica/ui/hooks/use-mobile"; // CEREBRO-PATCH(image-phone-controls): FIR-4699 — presets replace drag handles on phones.
// CEREBRO-PATCH(zoomable-image-preview): pinch/wheel zoom on the image lightbox (TECH-3695)
// CEREBRO-PATCH(image-chip-thumbnail): FIR-2034 — compact image card behind cerebro_attachment_chips.
import { ZoomableImage, AttachmentChip } from "@multica/cerebro-ui";
import { useFlagValue } from "@multica/cerebro-feature-flags";
// CEREBRO-PATCH(image-view-gallery): FIR-2710 — inline images join the surface image gallery.
import { useGalleryImage } from "@multica/cerebro-attachments/views";
// CEREBRO-PATCH(image-resize-controls): FIR-4699 Phase 5 — corner resize handles.
// CEREBRO-PATCH(image-placement-toggle): FIR-4699 — tray placement actions.
import {
  ImageResizeControls,
  deleteInlineImageAndCaption,
  moveInlineImageToTray,
} from "@multica/cerebro-editor-image";
import { useT } from "../../i18n";
import { useAttachmentDownloadResolver } from "../attachment-download-context";
// CEREBRO-PATCH(image-phone-controls): FIR-4699 — shared desktop right-click / phone long-press image menu.
import {
  CerebroImageContextMenu,
  inlineImageFigureStyle,
} from "./cerebro-image-context-menu";

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

// CEREBRO-PATCH(image-placement-toggle): FIR-4699 — caption-aware node actions.
function ImageView({ node, editor, selected, getPos }: NodeViewProps) {
  const { t } = useT("editor");
  const src = node.attrs.src as string;
  const alt = (node.attrs.alt as string) || "";
  const title = node.attrs.title as string | undefined;
  const uploading = node.attrs.uploading as boolean;
  // CEREBRO-PATCH(image-resize-controls): FIR-4699 Phase 5 — inline size/align.
  const widthPct = node.attrs.widthPct as number | null;
  const align = node.attrs.align as "left" | "center" | "right" | null;
  const placement = node.attrs.placement as string | null; // CEREBRO-PATCH(image-placement-toggle): explicit inline state.
  const figureRef = useRef<HTMLElement>(null);
  const { openByUrl } = useAttachmentDownloadResolver();

  const [lightbox, setLightbox] = useState(false);
  const isEditable = editor.isEditable;
  const isMobile = useIsMobile(); // CEREBRO-PATCH(image-phone-controls): phone uses presets, not handles.
  // CEREBRO-PATCH(image-view-gallery): FIR-2710 — inline images open the surface gallery. Registration is harmless in an editable editor: only the Maximize button / non-editor figure click call handleView, never ProseMirror selection.
  const gallery = useGalleryImage(uploading ? null : { src, alt, downloadHref: src });
  const handleView = () => (gallery.enabled ? gallery.open() : setLightbox(true));
  // CEREBRO-PATCH(image-chip-thumbnail): FIR-2034 — compact thumbnail card + existing lightbox instead of the large figure.
  const chipsEnabled = useFlagValue("cerebro_attachment_chips");
  // CEREBRO-PATCH(image-placement-toggle): explicit inline images escape the chip view.
  const inlinePlaced = placement === "inline" || widthPct != null || align != null;
  if (chipsEnabled && !inlinePlaced)
    return (
      // CEREBRO-PATCH(image-view-gallery): FIR-2710 — register this image with the surface gallery via ref.
      <NodeViewWrapper as="span" className="image-node" ref={gallery.ref}>
        <AttachmentChip filename={alt || "image"} thumbnailSrc={src} onActivate={uploading ? undefined : handleView} activateLabel={t(($) => $.image.view)} onRemove={isEditable ? () => deleteInlineImageAndCaption(editor, node, getPos) : undefined} uploading={uploading} />
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

  const handleMoveToBottom = () => {
    if (figureRef.current) {
      moveInlineImageToTray(figureRef.current, editor, node, getPos);
    }
  };
  const canMoveToBottom = Boolean(
    (editor.view?.dom as HTMLElement | undefined)?.closest?.("[data-editor-image-tray]"),
  );

  // CEREBRO-PATCH(image-placement-toggle): the toolbar action below replaces Convert to attachment.
  return (
    // CEREBRO-PATCH(image-view-gallery): FIR-2710 — register this image with the surface gallery via ref.
    <NodeViewWrapper className="image-node" ref={gallery.ref}>
      {/* CEREBRO-PATCH(image-phone-controls): the native ContextMenu supplies desktop right-click and 500 ms touch long-press. */}
      <CerebroImageContextMenu
        widthPct={widthPct}
        align={align}
        editable={isEditable && !uploading}
        disabled={uploading}
        labels={{
          view: t(($) => $.image.view),
          download: t(($) => $.image.download),
          copyLink: t(($) => $.image.copy_link),
          size: t(($) => $.image.size),
          small: t(($) => $.image.small),
          medium: t(($) => $.image.medium),
          fullWidth: t(($) => $.image.full_width),
          alignLeft: t(($) => $.image.align_left),
          alignCenter: t(($) => $.image.align_center),
          alignRight: t(($) => $.image.align_right),
          moveToBottom: t(($) => $.image.move_to_bottom),
          delete: t(($) => $.image.delete),
        }}
        canMoveToBottom={canMoveToBottom}
        onWidthChange={(value) => {
          const pos = getPos();
          if (typeof pos === "number") {
            editor.chain().setNodeSelection(pos).setImageWidthPct(value).run();
          }
        }}
        onAlignChange={(value) => {
          const pos = getPos();
          if (typeof pos === "number") {
            editor.chain().setNodeSelection(pos).setImageAlign(value).run();
          }
        }}
        onView={handleView}
        onDownload={handleDownload}
        onCopyLink={handleCopyLink}
        onMoveToBottom={handleMoveToBottom}
        onDelete={() => deleteInlineImageAndCaption(editor, node, getPos)}
        trigger={
          <figure
            ref={figureRef}
            data-align={align || undefined}
            style={inlineImageFigureStyle(widthPct)}
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
            {isEditable && selected && !uploading && !isMobile && (
              <ImageResizeControls editor={editor} figureRef={figureRef} />
            )}
            {!uploading && !isMobile && (
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
                {isEditable &&
                  (
                    [
                      ["left", AlignLeft, "Align left"],
                      ["center", AlignCenter, "Align center"],
                      ["right", AlignRight, "Align right"],
                    ] as const
                  ).map(([value, Icon, label]) => (
                    <button
                      key={value}
                      type="button"
                      className={cn(align === value && "image-toolbar-active")}
                      onClick={() =>
                        editor
                          .chain()
                          .setImageAlign(align === value ? null : value)
                          .run()
                      }
                      title={label}
                    >
                      <Icon className="size-3.5" />
                    </button>
                  ))}
                {isEditable && (
                  <button
                    type="button"
                    className="image-move-to-tray"
                    onClick={handleMoveToBottom}
                    title="Move to bottom"
                  >
                    <Paperclip className="size-3.5" />
                  </button>
                )}
                {isEditable && (
                  <button
                    type="button"
                    onClick={() => deleteInlineImageAndCaption(editor, node, getPos)}
                    title={t(($) => $.image.delete)}
                  >
                    <Trash2 className="size-3.5" />
                  </button>
                )}
              </div>
            )}
          </figure>
        }
      />
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
