import type { SVGProps } from "react";

export type OpticalGlyphName =
  | "command"
  | "control"
  | "option"
  | "shift"
  | "enter"
  | "backspace"
  | "forwardDelete"
  | "tab"
  | "arrowUp"
  | "arrowDown"
  | "arrowLeft"
  | "arrowRight"
  | "space";

interface GlyphSpec {
  /** Proportional advance width on a shared 16-unit vertical em-box. */
  width: number;
  /**
   * Stroke width tuned to Inter Medium (500) stem weight (~0.086em):
   * 1.18 / 16 * 1.18em ≈ 0.087em (~0.96px at 11px, ~1.04px at 12px).
   * Closed/dense contours use slightly lighter 1.14 to avoid perceived heaviness.
   */
  strokeWidth?: number;
  /** Inner SVG paths; cap-height optical band is y = 3.2..12.8 (center y = 8). */
  paths: readonly string[];
}

export const OPTICAL_GLYPHS: Record<OpticalGlyphName, GlyphSpec> = {
  command: {
    width: 15.5,
    strokeWidth: 1.15,
    paths: [
      "M9.45 4.75v6.5a1.62 1.62 0 1 0 1.62-1.62H4.43a1.62 1.62 0 1 0 1.62 1.62v-6.5a1.62 1.62 0 1 0-1.62 1.62h6.64a1.62 1.62 0 1 0-1.62-1.62Z",
    ],
  },
  control: {
    width: 13.5,
    strokeWidth: 1.22,
    paths: ["M2.75 10.05 6.75 5.6l4 4.45"],
  },
  option: {
    width: 17,
    strokeWidth: 1.2,
    paths: ["M2.25 4.15h3.85l4.2 7.7h4.45", "M10.25 4.15h4.5"],
  },
  shift: {
    width: 15.5,
    strokeWidth: 1.16,
    paths: [
      "M7.75 3.35 2.75 8.55h2.75v3.35c0 .42.34.75.75.75h3c.41 0 .75-.33.75-.75V8.55h2.75L7.75 3.35Z",
    ],
  },
  enter: {
    width: 16.5,
    strokeWidth: 1.2,
    paths: [
      "M13.6 3.85v3.55c0 1.05-.85 1.9-1.9 1.9H3.45",
      "M6.35 6.5 3.25 9.3l3.1 2.8",
    ],
  },
  backspace: {
    width: 18.5,
    strokeWidth: 1.15,
    paths: [
      "M6.05 4.05h8.9c.9 0 1.6.7 1.6 1.6v4.7c0 .9-.7 1.6-1.6 1.6h-8.9c-.48 0-.93-.22-1.24-.59L2.25 8l2.56-3.36c.31-.37.76-.59 1.24-.59Z",
      "M9.3 6.25 12.8 9.75",
      "M12.8 6.25 9.3 9.75",
    ],
  },
  forwardDelete: {
    width: 18.5,
    strokeWidth: 1.15,
    paths: [
      "M12.45 4.05h-8.9c-.9 0-1.6.7-1.6 1.6v4.7c0 .9.7 1.6 1.6 1.6h8.9c.48 0 .93-.22 1.24-.59L16.25 8l-2.56-3.36c-.31-.37-.76-.59-1.24-.59Z",
      "M5.7 6.25 9.2 9.75",
      "M9.2 6.25 5.7 9.75",
    ],
  },
  tab: {
    width: 17,
    strokeWidth: 1.2,
    paths: [
      "M2.25 8h8.9",
      "M8.1 4.9 11.4 8l-3.3 3.1",
      "M14.5 4.05v7.9",
    ],
  },
  arrowUp: {
    width: 13,
    strokeWidth: 1.22,
    paths: ["M6.5 12.35V3.8", "M3.25 6.95 6.5 3.65l3.25 3.3"],
  },
  arrowDown: {
    width: 13,
    strokeWidth: 1.22,
    paths: ["M6.5 3.65v8.55", "M3.25 9.05 6.5 12.35l3.25-3.3"],
  },
  arrowLeft: {
    width: 15,
    strokeWidth: 1.22,
    paths: ["M12.1 8H3.3", "M6.45 4.75 3.15 8l3.3 3.25"],
  },
  arrowRight: {
    width: 15,
    strokeWidth: 1.22,
    paths: ["M2.9 8h8.8", "M8.55 4.75 11.85 8l-3.3 3.25"],
  },
  space: {
    width: 16.5,
    strokeWidth: 1.2,
    paths: ["M2.6 6.8v2.2c0 .72.58 1.3 1.3 1.3h8.7c.72 0 1.3-.58 1.3-1.3V6.8"],
  },
};

const GLYPH_HEIGHT_EM = 1.18;

export function OpticalKbdGlyph({
  name,
  className,
  strokeWidth,
  style,
  ...rest
}: {
  name: OpticalGlyphName;
} & Omit<SVGProps<SVGSVGElement>, "viewBox" | "width" | "height">) {
  const spec = OPTICAL_GLYPHS[name];
  const widthEm = ((spec.width / 16) * GLYPH_HEIGHT_EM).toFixed(4);
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox={`0 0 ${spec.width} 16`}
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth ?? spec.strokeWidth ?? 1.2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={className}
      style={{
        width: `${widthEm}em`,
        height: `${GLYPH_HEIGHT_EM}em`,
        flexShrink: 0,
        ...style,
      }}
      {...rest}
    >
      {spec.paths.map((d, i) => (
        <path key={i} d={d} />
      ))}
    </svg>
  );
}
