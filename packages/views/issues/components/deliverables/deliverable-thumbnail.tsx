"use client";

import { useEffect, useState, type MouseEvent } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { Attachment } from "@multica/core/types";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { Download, Eye, ImageIcon, Link2 } from "@multica/ui/icons";
import { canCopyImage, copyImage, copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../../i18n";
import { CLOSED_ROW_MENU, type RowMenuState } from "../../../common/point-menu";
import { RowActionsPointMenu, type RowActionItem } from "../../../common/row-actions-menu";
import { useDownloadAttachment } from "../../../editor";
import {
  useAttachmentThumbnailURL,
  useResignedInlineMedia,
} from "../../../editor/hooks/use-inline-media-url";
import { useOpenAttachment } from "./use-open-attachment";
import { fileTypeLabel, getPreviewKind } from "../../../editor/utils/preview";
import { fileIcon } from "../../../editor/utils/file-icon";

/**
 * A file's face in a grid or strip: the image itself for images, otherwise
 * its type icon and extension. Fills its box — the caller sizes it.
 */
export function DeliverableThumbnail({
  attachment,
  showTypeLabel = true,
  className,
}: {
  attachment: Attachment;
  /** The extension under the icon; off where the tile is too small for it. */
  showTypeLabel?: boolean;
  className?: string;
}) {
  if (getPreviewKind(attachment.content_type, attachment.filename) === "image") {
    return <ImageThumbnail attachment={attachment} className={className} />;
  }
  return (
    <FileFace
      attachment={attachment}
      showTypeLabel={showTypeLabel}
      className={className}
    />
  );
}

function FileFace({
  attachment,
  showTypeLabel,
  className,
}: {
  attachment: Attachment;
  showTypeLabel: boolean;
  className?: string;
}) {
  const Icon = fileIcon(attachment.content_type, attachment.filename);
  const label = fileTypeLabel(attachment.filename);
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-1.5 bg-muted text-muted-foreground",
        className,
      )}
    >
      <Icon className="size-5" />
      {showTypeLabel && label && (
        <span className="text-micro font-medium tracking-wide">{label}</span>
      )}
    </div>
  );
}

// A large image shows the server's downscaled copy: decoding dozens of
// multi-megapixel originals into small tiles is what made the overview stutter.
// Small images, other formats, older servers and a failed thumbnail load the
// original.
function ImageThumbnail({
  attachment,
  className,
}: {
  attachment: Attachment;
  className?: string;
}) {
  const [failedThumbnail, setFailedThumbnail] = useState<string | null>(null);
  const thumbnailPath = attachment.thumbnail_url;
  if (thumbnailPath && failedThumbnail !== thumbnailPath) {
    return (
      <ResizedImageThumbnail
        attachment={attachment}
        thumbnailPath={thumbnailPath}
        onFail={() => setFailedThumbnail(thumbnailPath)}
        className={className}
      />
    );
  }
  return <OriginalImageThumbnail attachment={attachment} className={className} />;
}

// The browser's image menu, and the desktop's Copy image, would act on the
// thumbnail the tile shows, so the tile brings its own menu, acting on the
// original. Handling the right-click keeps either from opening.
function ResizedImageThumbnail({
  attachment,
  thumbnailPath,
  onFail,
  className,
}: {
  attachment: Attachment;
  thumbnailPath: string;
  onFail: () => void;
  className?: string;
}) {
  const { url, pending, failed } = useAttachmentThumbnailURL(attachment.id, thumbnailPath);
  const [menu, setMenu] = useState<RowMenuState>(CLOSED_ROW_MENU);
  useEffect(() => {
    if (failed) onFail();
  }, [failed, onFail]);

  const openMenu = (event: MouseEvent) => {
    event.preventDefault();
    setMenu({ open: true, point: { x: event.clientX, y: event.clientY } });
  };

  return (
    <>
      <span className="contents" onContextMenu={openMenu}>
        {pending || failed || !url ? (
          <FileFace attachment={attachment} showTypeLabel={false} className={className} />
        ) : (
          <img
            src={url}
            alt=""
            loading="lazy"
            decoding="async"
            draggable={false}
            onError={onFail}
            className={cn("bg-muted object-cover", className)}
          />
        )}
      </span>
      {menu.point && (
        <OriginalImageMenu
          attachment={attachment}
          menu={menu}
          onOpenChange={(open) => setMenu((current) => ({ ...current, open }))}
        />
      )}
    </>
  );
}

function OriginalImageMenu({
  attachment,
  menu,
  onOpenChange,
}: {
  attachment: Attachment;
  menu: RowMenuState;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("editor");
  const { open, modal } = useOpenAttachment();
  const download = useDownloadAttachment();
  const link = resolvePublicFileUrl(attachment.markdown_url);
  const copyOriginal = async () => {
    if (await copyImage(() => api.getAttachmentOriginalBlob(attachment.id))) {
      toast.success(t(($) => $.image.image_copied));
    } else {
      toast.error(t(($) => $.image.copy_image_failed));
    }
  };
  const copyLink = async (url: string) => {
    // Same-origin web resolves the durable URL to a site-relative path.
    if (await copyText(new URL(url, window.location.href).href)) {
      toast.success(t(($) => $.image.link_copied));
    } else {
      toast.error(t(($) => $.image.copy_link_failed));
    }
  };
  const actions: RowActionItem[] = [
    // The viewer shows the original, where the browser's own Copy image works
    // even without the async Clipboard API that Copy image here needs.
    {
      key: "view",
      icon: <Eye />,
      label: t(($) => $.image.view),
      onSelect: () => open(attachment),
    },
    ...(canCopyImage()
      ? [
          {
            key: "copy-image",
            icon: <ImageIcon />,
            label: t(($) => $.image.copy_image),
            onSelect: () => void copyOriginal(),
          },
        ]
      : []),
    {
      key: "download",
      icon: <Download />,
      label: t(($) => $.image.download),
      onSelect: () => void download(attachment.id),
    },
    ...(link
      ? [
          {
            key: "copy-link",
            icon: <Link2 />,
            label: t(($) => $.image.copy_link),
            onSelect: () => void copyLink(link),
          },
        ]
      : []),
  ];
  return (
    <>
      <RowActionsPointMenu menu={menu} onOpenChange={onOpenChange} groups={[actions]} />
      {modal}
    </>
  );
}

// Loads the original, so callers keep these few (the sidebar shows three) or
// lazy (the overview's grid).
function OriginalImageThumbnail({
  attachment,
  className,
}: {
  attachment: Attachment;
  className?: string;
}) {
  const picked =
    attachment.download_url || attachment.markdown_url || attachment.url;
  const { url, pending } = useResignedInlineMedia(
    attachment.id,
    resolvePublicFileUrl(picked) ?? picked,
    true,
  );
  const [failedUrl, setFailedUrl] = useState<string | null>(null);

  if (pending || !url || failedUrl === url) {
    return (
      <FileFace attachment={attachment} showTypeLabel={false} className={className} />
    );
  }
  return (
    <img
      src={url}
      alt=""
      loading="lazy"
      decoding="async"
      draggable={false}
      onError={() => setFailedUrl(url)}
      className={cn("bg-muted object-cover", className)}
    />
  );
}
