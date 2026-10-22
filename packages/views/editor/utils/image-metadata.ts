import type { Attachment } from "@multica/core/types";

// Some attachment lists reach the renderer without a schema pass, so the
// optional image fields are checked here rather than trusted.

function pixelSize(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? Math.round(value)
    : undefined;
}

/** The image's displayed pixel size, when the server recorded it. */
export function imageDimensions(
  record: Pick<Attachment, "width" | "height">,
): { width: number; height: number } | undefined {
  const width = pixelSize(record.width);
  const height = pixelSize(record.height);
  return width && height ? { width, height } : undefined;
}

/** URL of the image's downscaled copy, when it has one. */
export function imagePreviewURL(record: Pick<Attachment, "preview_url">): string | undefined {
  return typeof record.preview_url === "string" && record.preview_url !== ""
    ? record.preview_url
    : undefined;
}
