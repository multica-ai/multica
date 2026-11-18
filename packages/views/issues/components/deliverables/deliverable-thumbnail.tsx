"use client";

import { useEffect, useState } from "react";
import type { Attachment } from "@multica/core/types";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { cn } from "@multica/ui/lib/utils";
import {
  useAttachmentThumbnailURL,
  useResignedInlineMedia,
} from "../../../editor/hooks/use-inline-media-url";
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
  useEffect(() => {
    if (failed) onFail();
  }, [failed, onFail]);

  if (pending || failed || !url) {
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
      onError={onFail}
      className={cn("bg-muted object-cover", className)}
    />
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
