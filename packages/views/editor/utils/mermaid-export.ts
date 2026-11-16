/**
 * Export helpers for Mermaid diagrams (`.mmd` source, `.svg`, `.png`).
 *
 * Export runs in the host document against the SVG markup Mermaid returned —
 * never by reaching into the preview iframe, which is `sandbox=""` and
 * deliberately unreachable. The markup therefore has to be made standalone
 * first: Mermaid renders for embedding, so it emits a transparent background,
 * a `max-width` style tuned to the host container, and `font-family: inherit`,
 * none of which survive being opened as a file on their own.
 */

const SVG_NAMESPACE = "http://www.w3.org/2000/svg";

// Rasterize above CSS pixels so PNGs stay legible when zoomed or pasted into a
// doc. 2 matches a typical HiDPI screen without producing huge files.
const PNG_PIXEL_RATIO = 2;

export interface ExportSvgOptions {
  /** Opaque page color, so an exported file is not transparent-on-transparent. */
  background: string;
  /** Concrete stack to replace Mermaid's `inherit`, which has nothing to inherit from in a file. */
  fontFamily: string;
  width: number;
  height: number;
}

interface ViewBox {
  minX: number;
  minY: number;
  width: number;
  height: number;
}

function readViewBox(root: SVGElement, fallback: Size): ViewBox {
  const parts = (root.getAttribute("viewBox") ?? "")
    .split(/[\s,]+/)
    .map((value) => Number.parseFloat(value));

  if (parts.length === 4 && parts.every((value) => Number.isFinite(value))) {
    const [minX, minY, width, height] = parts as [number, number, number, number];
    if (width > 0 && height > 0) return { minX, minY, width, height };
  }

  return { minX: 0, minY: 0, width: fallback.width, height: fallback.height };
}

interface Size {
  width: number;
  height: number;
}

/**
 * Rewrites a `style` attribute's declarations.
 *
 * Done at the attribute level rather than through the `.style` CSSStyleDeclaration
 * API: an SVG root parsed out of an XML document does not reliably expose that
 * API (jsdom does not implement it at all), so touching `.style` here would
 * throw outside a real browser.
 */
function mergeStyleAttribute(
  existing: string | null,
  overrides: Record<string, string>,
  remove: string[],
): string {
  const declarations = new Map<string, string>();

  for (const part of (existing ?? "").split(";")) {
    const separator = part.indexOf(":");
    if (separator === -1) continue;
    const property = part.slice(0, separator).trim().toLowerCase();
    if (property) declarations.set(property, part.slice(separator + 1).trim());
  }

  for (const property of remove) declarations.delete(property);
  for (const [property, value] of Object.entries(overrides)) {
    declarations.set(property, value);
  }

  return [...declarations]
    .map(([property, value]) => `${property}: ${value}`)
    .join("; ");
}

/**
 * Rewrites embedded Mermaid SVG markup into a standalone document.
 *
 * @returns serialized SVG, or `null` when the markup does not parse.
 */
export function buildExportSvg(
  svgMarkup: string,
  { background, fontFamily, width, height }: ExportSvgOptions,
): string | null {
  const parsed = new DOMParser().parseFromString(svgMarkup, "image/svg+xml");
  if (parsed.querySelector("parsererror")) return null;

  const root = parsed.documentElement as unknown as SVGElement;
  if (root.nodeName.toLowerCase() !== "svg") return null;

  root.setAttribute("xmlns", SVG_NAMESPACE);
  // Concrete pixel dimensions: `width="100%"` (Mermaid's default) has no
  // percentage basis in a standalone file, and an <img> loading it for the PNG
  // path would have no intrinsic size to draw.
  root.setAttribute("width", String(width));
  root.setAttribute("height", String(height));
  root.setAttribute(
    "style",
    mergeStyleAttribute(
      root.getAttribute("style"),
      { "background-color": background, "font-family": fontFamily },
      // Mermaid sizes the embedded SVG to the host container; kept, it would
      // clip the export to whatever width the page happened to have.
      ["max-width"],
    ),
  );

  const viewBox = readViewBox(root, { width, height });
  const backgroundRect = parsed.createElementNS(SVG_NAMESPACE, "rect");
  // Cover the viewBox rather than 0,0 100%x100%: Mermaid emits negative
  // viewBox origins for diagrams with padding, and a 0,0-anchored rect would
  // leave those margins transparent.
  backgroundRect.setAttribute("x", String(viewBox.minX));
  backgroundRect.setAttribute("y", String(viewBox.minY));
  backgroundRect.setAttribute("width", String(viewBox.width));
  backgroundRect.setAttribute("height", String(viewBox.height));
  backgroundRect.setAttribute("fill", background);
  root.insertBefore(backgroundRect, root.firstChild);

  return new XMLSerializer().serializeToString(root);
}

function loadImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error("Failed to load SVG for PNG export"));
    image.src = url;
  });
}

/**
 * Rasterizes standalone SVG markup to a PNG blob.
 *
 * The blob URL is same-origin and the markup carries no external references
 * (fonts are resolved to a concrete stack by `buildExportSvg`), so the canvas
 * stays untainted and `toBlob` is allowed to read it back.
 */
export async function renderSvgToPngBlob(
  standaloneSvg: string,
  { width, height }: Size,
  pixelRatio: number = PNG_PIXEL_RATIO,
): Promise<Blob> {
  const svgBlob = new Blob([standaloneSvg], { type: "image/svg+xml;charset=utf-8" });
  const url = URL.createObjectURL(svgBlob);

  try {
    const image = await loadImage(url);
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(width * pixelRatio));
    canvas.height = Math.max(1, Math.round(height * pixelRatio));

    const context = canvas.getContext("2d");
    if (!context) throw new Error("Canvas 2D context unavailable for PNG export");
    context.drawImage(image, 0, 0, canvas.width, canvas.height);

    const blob = await new Promise<Blob | null>((resolve) =>
      canvas.toBlob(resolve, "image/png"),
    );
    if (!blob) throw new Error("Failed to encode PNG");

    return blob;
  } finally {
    URL.revokeObjectURL(url);
  }
}

export function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.rel = "noopener";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  // Revoke on the next task: Safari aborts the download if the URL dies while
  // the click is still being processed synchronously.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

/**
 * Filename stem derived from the diagram's first meaningful line, so a folder
 * of exports is scannable instead of being `diagram (3).png` all the way down.
 */
export function diagramFilenameStem(chart: string): string {
  const firstLine = chart
    .split("\n")
    .map((line) => line.trim())
    .find((line) => line.length > 0 && !line.startsWith("%%"));

  const slug = (firstLine ?? "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
    .replace(/-+$/g, "");

  return slug || "diagram";
}
