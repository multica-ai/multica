// Geometry and canvas rendering for the shared avatar cropper.
//
// The geometry (coverBaseScale / clampTransform / computeCropRect) is kept
// pure and DOM-free so it can be unit-tested and reused by the preview and the
// output render without drift — the on-screen preview and the canvas output
// are computed from the same rect, which is what makes the crop WYSIWYG.
//
// The canvas/blob helpers at the bottom are the only DOM-bound part.

export interface CropTransform {
  /** Zoom multiplier applied on top of the "cover" base scale. Always >= 1. */
  scale: number;
  /** Pan of the image center away from the viewport center, in viewport px. */
  offsetX: number;
  offsetY: number;
}

export interface CropGeometry extends CropTransform {
  /** Intrinsic (natural) image dimensions, px. */
  imageWidth: number;
  imageHeight: number;
  /** On-screen square viewport side, px. Must match the preview element. */
  viewport: number;
}

export interface SourceRect {
  sx: number;
  sy: number;
  size: number;
}

/** Base scale that makes the image exactly cover the square viewport. */
export function coverBaseScale(
  imageWidth: number,
  imageHeight: number,
  viewport: number,
): number {
  const shortSide = Math.max(1, Math.min(imageWidth, imageHeight));
  return viewport / shortSide;
}

/** How far the image center may pan before a viewport edge shows blank. */
export function maxOffset(drawnSize: number, viewport: number): number {
  return Math.max(0, (drawnSize - viewport) / 2);
}

function clampToRange(value: number, limit: number): number {
  if (limit <= 0) return 0;
  return Math.min(limit, Math.max(-limit, value));
}

/**
 * Clamp a transform so the image always fully covers the viewport (no blank
 * gutter). Re-run whenever scale or offset changes.
 */
export function clampTransform(geometry: CropGeometry): CropTransform {
  const base = coverBaseScale(
    geometry.imageWidth,
    geometry.imageHeight,
    geometry.viewport,
  );
  const drawnW = geometry.imageWidth * base * geometry.scale;
  const drawnH = geometry.imageHeight * base * geometry.scale;
  return {
    scale: geometry.scale,
    offsetX: clampToRange(geometry.offsetX, maxOffset(drawnW, geometry.viewport)),
    offsetY: clampToRange(geometry.offsetY, maxOffset(drawnH, geometry.viewport)),
  };
}

/**
 * Map the square viewport back into source-image pixel coordinates. The
 * returned rect is exactly what `drawImage` samples, and it matches the CSS
 * transform the preview renders — a `+offsetX` pan shifts the image right, so
 * the viewport center reveals a source pixel to the left of the image center.
 */
export function computeCropRect(geometry: CropGeometry): SourceRect {
  const base = coverBaseScale(
    geometry.imageWidth,
    geometry.imageHeight,
    geometry.viewport,
  );
  const effective = base * geometry.scale; // viewport px per source px
  const centerX = geometry.imageWidth / 2 - geometry.offsetX / effective;
  const centerY = geometry.imageHeight / 2 - geometry.offsetY / effective;
  const size = geometry.viewport / effective;
  return { sx: centerX - size / 2, sy: centerY - size / 2, size };
}

// ---------------------------------------------------------------------------
// DOM-bound output rendering
// ---------------------------------------------------------------------------

/** Square side of the encoded avatar. Avatars never need the original bitmap. */
export const AVATAR_OUTPUT_SIZE = 512;

const AVATAR_QUALITY = 0.85;

let webpEncodeSupport: boolean | null = null;

/**
 * Whether the browser can *encode* WebP via canvas. Safari < 17 decodes WebP
 * but cannot encode it, silently emitting PNG from toDataURL — so we probe the
 * data URL's mime rather than assuming.
 */
export function supportsWebpEncode(): boolean {
  if (webpEncodeSupport !== null) return webpEncodeSupport;
  try {
    const canvas = document.createElement("canvas");
    canvas.width = 1;
    canvas.height = 1;
    webpEncodeSupport = canvas
      .toDataURL("image/webp")
      .startsWith("data:image/webp");
  } catch {
    webpEncodeSupport = false;
  }
  return webpEncodeSupport;
}

/** Preferred output type: WebP (keeps alpha, smaller) with a JPEG fallback. */
export function pickOutputType(): { type: string; quality: number } {
  return supportsWebpEncode()
    ? { type: "image/webp", quality: AVATAR_QUALITY }
    : { type: "image/jpeg", quality: AVATAR_QUALITY };
}

function canvasToBlob(
  canvas: HTMLCanvasElement,
  type: string,
  quality: number,
): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => (blob ? resolve(blob) : reject(new Error("Canvas is empty"))),
      type,
      quality,
    );
  });
}

export interface RenderOptions {
  output: number;
  type: string;
  quality: number;
  /** Fill color drawn behind the image, for opaque formats (JPEG). */
  background?: string;
}

/**
 * Draw the cropped region of `image` into a square canvas and encode it. The
 * source rect comes from the same geometry the preview used, so the encoded
 * pixels equal what the user saw.
 */
export async function renderCroppedAvatar(
  image: CanvasImageSource,
  geometry: CropGeometry,
  options: RenderOptions,
): Promise<Blob> {
  const rect = computeCropRect(geometry);
  const canvas = document.createElement("canvas");
  canvas.width = options.output;
  canvas.height = options.output;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("Canvas 2D context unavailable");
  ctx.imageSmoothingQuality = "high";
  if (options.background) {
    ctx.fillStyle = options.background;
    ctx.fillRect(0, 0, options.output, options.output);
  }
  ctx.drawImage(
    image,
    rect.sx,
    rect.sy,
    rect.size,
    rect.size,
    0,
    0,
    options.output,
    options.output,
  );
  return canvasToBlob(canvas, options.type, options.quality);
}

/** Wrap an encoded blob in a File, swapping the source extension for the output's. */
export function blobToAvatarFile(
  blob: Blob,
  sourceName: string,
  type: string,
): File {
  const ext = type === "image/webp" ? "webp" : type === "image/png" ? "png" : "jpg";
  const base = sourceName.replace(/\.[^./\\]+$/, "") || "avatar";
  return new File([blob], `${base}.${ext}`, { type });
}
