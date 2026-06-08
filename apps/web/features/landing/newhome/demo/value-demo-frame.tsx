"use client";

// Shared scale frame for the value-section micro-demos (#2–#4). Each demo lays
// out at a fixed natural size, then this frame scales it down by the shared
// DEMO_ZOOM so every value card's demo matches the hero board's on-screen scale
// and the cards line up at one height. Value #1 (the board) carries its own
// frame because it also needs providers; the natural size is kept identical
// here so all four cards are the same height.

import { useEffect, useRef, useState } from "react";
import { DEMO_ZOOM } from "./zoom";

// Default natural width for the content-light demos (#2–#4). Sized so that,
// scaled by DEMO_ZOOM, the panel fits the demo half of the card at the design
// widths (≥1200px container) without bleeding/clipping. Height is per-demo
// (sized to its content) so panels stay snug.
export const VALUE_DEMO_W = 720;

export function ValueDemoFrame({
  width = VALUE_DEMO_W,
  height,
  children,
}: {
  width?: number;
  height: number;
  children: React.ReactNode;
}) {
  const frameRef = useRef<HTMLDivElement>(null);
  const [renderedWidth, setRenderedWidth] = useState(width * DEMO_ZOOM);

  useEffect(() => {
    const frame = frameRef.current;
    if (!frame) return;

    const measure = () => {
      const next = frame.clientWidth || width * DEMO_ZOOM;
      setRenderedWidth(Math.min(width * DEMO_ZOOM, next));
    };

    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(frame);
    return () => observer.disconnect();
  }, [width]);

  const scale = Math.min(DEMO_ZOOM, renderedWidth / width);

  return (
    <div
      ref={frameRef}
      className="w-full overflow-hidden"
      style={{
        width: width * DEMO_ZOOM,
        maxWidth: "100%",
        height: height * scale,
      }}
    >
      <div
        className="origin-top-left"
        style={{ width, height, transform: `scale(${scale})` }}
      >
        {children}
      </div>
    </div>
  );
}
