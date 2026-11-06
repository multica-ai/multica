import type { Attachment } from "@multica/core/types";

// Some attachment lists reach the renderer without a schema pass, so the
// optional image fields are checked here rather than trusted.

function pixelSize(value: unknown): number | undefined {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0
    ? value
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
