"use client";

import type { ReactNode } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { Download, Eye, ImageIcon, Link2 } from "@multica/ui/icons";
import { canCopyImage, copyImage, copyText } from "@multica/ui/lib/clipboard";
import { useT } from "../i18n";
import type { RowMenuState } from "../common/point-menu";
import { RowActionsPointMenu, type RowActionItem } from "../common/row-actions-menu";

/**
 * The right-click menu of an image showing the server's downscaled copy. The
 * browser's image menu, and the desktop's Copy image, would act on the copy on
 * screen, so the image brings this one, acting on the original. The caller
 * opens it from `onContextMenu` and calls `preventDefault`, which keeps either
 * of those from opening.
 */
export function OriginalImageMenu({
  attachmentId,
  link,
  menu,
  onOpenChange,
  onView,
  onDownload,
  trailing,
  finalFocus,
}: {
  attachmentId: string;
  /** What Copy link copies; no item without one. */
  link: string | null | undefined;
  menu: RowMenuState;
  onOpenChange: (open: boolean) => void;
  onView: () => void;
  onDownload: () => void;
  trailing?: ReactNode;
  finalFocus?: () => boolean;
}) {
  const { t } = useT("editor");
  const copyOriginal = async () => {
    if (await copyImage(() => api.getAttachmentOriginalBlob(attachmentId))) {
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
      onSelect: onView,
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
      onSelect: onDownload,
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
    <RowActionsPointMenu
      menu={menu}
      onOpenChange={onOpenChange}
      groups={[actions]}
      trailing={trailing}
      finalFocus={finalFocus}
    />
  );
}
