"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { clampRatio, justifyRows } from "./justified-layout";

const GAP_PX = 8;
const MIN_ROW_HEIGHT = 100;
const MAX_ROW_HEIGHT = 240;
// Until an image has loaded its shape is unknown; most are screenshots.
const DEFAULT_RATIO = 16 / 10;

/**
 * Several images under one comment, laid out as justified rows (MUL-7649):
 * one shared height per row, each full row edge to edge. Widths come from
 * each image's real aspect ratio once it has loaded, so the row settles into
 * place instead of cropping.
 */
export function JustifiedImageRow<T extends { id: string }>({
  items,
  renderTile,
}: {
  items: ReadonlyArray<T>;
  /** Renders one image filling the box it is given. */
  renderTile: (item: T) => ReactNode;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [ratios, setRatios] = useState<ReadonlyMap<string, number>>(() => new Map());

  useLayoutEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const measure = () => setWidth(el.clientWidth);
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // `load` does not bubble, but it does travel the capture phase — one
  // listener here hears every tile's image, including ones that swap URL
  // after a re-sign. Images already decoded before this ran are read once.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const record = (img: HTMLImageElement) => {
      const key = img.closest<HTMLElement>("[data-ratio-key]")?.dataset.ratioKey;
      if (!key || img.naturalWidth <= 0 || img.naturalHeight <= 0) return;
      const ratio = img.naturalWidth / img.naturalHeight;
      setRatios((prev) => {
        if (Math.abs((prev.get(key) ?? 0) - ratio) < 0.001) return prev;
        return new Map(prev).set(key, ratio);
      });
    };
    el.querySelectorAll("img").forEach((img) => {
      if (img.complete) record(img);
    });
    const onLoad = (e: Event) => {
      if (e.target instanceof HTMLImageElement) record(e.target);
    };
    el.addEventListener("load", onLoad, true);
    return () => el.removeEventListener("load", onLoad, true);
  }, [items]);

  const rows = useMemo(
    () =>
      justifyRows(
        items.map((item) => ratios.get(item.id) ?? DEFAULT_RATIO),
        width,
        { gap: GAP_PX, minHeight: MIN_ROW_HEIGHT, maxHeight: MAX_ROW_HEIGHT },
      ),
    [items, ratios, width],
  );

  return (
    <div ref={containerRef} className="flex flex-col" style={{ gap: GAP_PX }}>
      {rows.map((row) => (
        <div key={items[row.items[0]!]!.id} className="flex" style={{ gap: GAP_PX }}>
          {row.items.map((index) => {
            const item = items[index]!;
            const ratio = clampRatio(ratios.get(item.id) ?? DEFAULT_RATIO);
            return (
              <div
                key={item.id}
                data-ratio-key={item.id}
                className="shrink-0"
                style={{ width: ratio * row.height, height: row.height }}
              >
                {renderTile(item)}
              </div>
            );
          })}
        </div>
      ))}
    </div>
  );
}
