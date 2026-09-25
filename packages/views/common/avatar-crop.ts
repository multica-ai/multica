// Canvas rendering + encoding for the shared avatar cropper.
//
// react-easy-crop owns the interactive geometry (pan / zoom / rotate) and hands
// back the crop rectangle in source-image pixels via `onCropComplete`. This
// module turns that rectangle into a fixed-size square, compressed avatar file.

/** Square side of the encoded avatar. Avatars never need the original bitmap. */
export const AVATAR_OUTPUT_SIZE = 512;

const AVATAR_QUALITY = 0.85;

/** react-easy-crop's `croppedAreaPixels` shape (source-image px). */
export interface PixelCrop {
  x: number;
  y: number;
  width: number;
  height: number;
}

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

function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.addEventListener("load", () => resolve(image));
    image.addEventListener("error", () => reject(new Error("Could not load image")));
    image.src = src;
  });
}

function toRadians(degrees: number): number {
  return (degrees * Math.PI) / 180;
}

/** Bounding box of an image after rotation, in px. */
function rotatedBoundingBox(width: number, height: number, degrees: number) {
  const rad = toRadians(degrees);
  return {
    width: Math.abs(Math.cos(rad) * width) + Math.abs(Math.sin(rad) * height),
    height: Math.abs(Math.sin(rad) * width) + Math.abs(Math.cos(rad) * height),
  };
}

export interface RenderOptions {
  output: number;
  type: string;
  quality: number;
  /** Fill color drawn behind the image, for opaque formats (JPEG). */
  background?: string;
}

/**
 * Draw the (possibly rotated) crop region into a square canvas and encode it.
 * `pixelCrop` is react-easy-crop's `croppedAreaPixels`, expressed in the
 * rotated-image bounding-box coordinate space — so we first rotate the whole
 * image into a bounding-box canvas, then sample the crop rect from it.
 */
export async function getCroppedAvatarBlob(
  imageSrc: string,
  pixelCrop: PixelCrop,
  rotation: number,
  options: RenderOptions,
): Promise<Blob> {
  const image = await loadImage(imageSrc);
  const bBox = rotatedBoundingBox(image.width, image.height, rotation);

  const rotated = document.createElement("canvas");
  rotated.width = Math.round(bBox.width);
  rotated.height = Math.round(bBox.height);
  const rctx = rotated.getContext("2d");
  if (!rctx) throw new Error("Canvas 2D context unavailable");
  rctx.translate(rotated.width / 2, rotated.height / 2);
  rctx.rotate(toRadians(rotation));
  rctx.drawImage(image, -image.width / 2, -image.height / 2);

  const out = document.createElement("canvas");
  out.width = options.output;
  out.height = options.output;
  const octx = out.getContext("2d");
  if (!octx) throw new Error("Canvas 2D context unavailable");
  octx.imageSmoothingQuality = "high";
  if (options.background) {
    octx.fillStyle = options.background;
    octx.fillRect(0, 0, options.output, options.output);
  }
  octx.drawImage(
    rotated,
    pixelCrop.x,
    pixelCrop.y,
    pixelCrop.width,
    pixelCrop.height,
    0,
    0,
    options.output,
    options.output,
  );
  return canvasToBlob(out, options.type, options.quality);
}
