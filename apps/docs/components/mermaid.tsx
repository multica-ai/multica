"use client";

import { useEffect, useId, useState } from "react";
import { useTheme } from "next-themes";

/**
 * Client-side Mermaid diagram renderer.
 *
 * Dynamic-imports the mermaid package so it's only loaded on pages that
 * actually use it (~400 KB). Re-renders when the page theme flips.
 *
 * Themed to pick up Multica design tokens at runtime via getComputedStyle,
 * so the diagram tracks both light / dark mode and any future token changes
 * without a rebuild.
 */
export function Mermaid({ chart }: { chart: string }) {
  const reactId = useId();
  const { resolvedTheme } = useTheme();
  const [svg, setSvg] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    void import("mermaid").then(({ default: mermaid }) => {
      const css = getComputedStyle(document.documentElement);
      // Mermaid's khroma parser only understands legacy color syntax (hex /
      // rgb / hsl / named). Our tokens are authored in oklch(), which
      // getComputedStyle preserves verbatim, and a `color-mix(in srgb, ...)`
      // round-trip still serializes as `color(srgb r g b)` per CSS Color 4.
      // Rasterize each token through a 1x1 canvas: fillStyle accepts any CSS
      // <color>, getImageData returns concrete 8-bit sRGB bytes regardless
      // of the input's color space.
      const canvas = document.createElement("canvas");
      canvas.width = 1;
      canvas.height = 1;
      const ctx = canvas.getContext("2d", { willReadFrequently: true });

      const v = (name: string, fallback: string) => {
        const raw = css.getPropertyValue(name).trim();
        if (!raw || !ctx) return fallback;
        // fillStyle silently ignores unparseable input; prime with a known
        // baseline so a parse failure paints black, not whatever was last set.
        ctx.fillStyle = "#000";
        ctx.fillStyle = raw;
        ctx.fillRect(0, 0, 1, 1);
        const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
        return `rgb(${r}, ${g}, ${b})`;
      };

      const brand = v("--brand", "#3b82f6");
      const brandFg = v("--brand-foreground", "#ffffff");
      const background = v("--background", "#ffffff");
      const foreground = v("--foreground", "#111111");
      const muted = v("--muted", "#f5f5f5");
      const mutedFg = v("--muted-foreground", "#6b7280");
      const border = v("--border", "#e5e5e5");
      const accent = v("--accent", muted);

      mermaid.initialize({
        startOnLoad: false,
        theme: "base",
        securityLevel: "strict",
        fontFamily: "inherit",
        themeVariables: {
          // Canvas
          background,
          mainBkg: background,
          // Nodes — soft muted fill with full-contrast text and a subtle border
          primaryColor: muted,
          primaryTextColor: foreground,
          primaryBorderColor: border,
          secondaryColor: accent,
          secondaryTextColor: foreground,
          secondaryBorderColor: border,
          tertiaryColor: background,
          tertiaryTextColor: foreground,
          tertiaryBorderColor: border,
          // Edges + labels
          lineColor: mutedFg,
          textColor: foreground,
          edgeLabelBackground: background,
          labelBackground: background,
          // Clusters (subgraph boxes)
          clusterBkg: accent,
          clusterBorder: border,
          titleColor: foreground,
          // Notes / callouts
          noteBkgColor: muted,
          noteTextColor: foreground,
          noteBorderColor: border,
          // Brand accent — used for active / start states in state diagrams,
          // user-decision diamonds in flowcharts, etc.
          activeTaskBkgColor: brand,
          activeTaskBorderColor: brand,
          altBackground: muted,
          // Sequence / git diagrams (harmless if unused)
          actorBkg: muted,
          actorBorder: border,
          actorTextColor: foreground,
          actorLineColor: mutedFg,
          signalColor: foreground,
          signalTextColor: foreground,
          // Fine print
          errorBkgColor: muted,
          errorTextColor: foreground,
        },
      });

      // mermaid requires a DOM-valid id; useId returns ":r0:" which isn't.
      const domId = `mermaid-${reactId.replace(/:/g, "")}`;

      mermaid
        .render(domId, chart.trim())
        .then((result) => {
          if (!cancelled) {
            setSvg(result.svg);
            setError(null);
          }
        })
        .catch((err: unknown) => {
          if (!cancelled) {
            setError(err instanceof Error ? err.message : String(err));
            setSvg(null);
          }
        });
    });

    return () => {
      cancelled = true;
    };
  }, [chart, reactId, resolvedTheme]);

  if (error) {
    return (
      <pre className="my-4 rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
        Mermaid error: {error}
      </pre>
    );
  }

  if (!svg) {
    return (
      <div className="my-4 text-sm text-muted-foreground">
        Rendering diagram…
      </div>
    );
  }

  return (
    <div
      className="my-6 flex justify-center overflow-x-auto rounded-md border border-border/60 bg-muted/20 p-6 [&_.label_foreignObject>div]:!font-[inherit] [&_.nodeLabel]:!font-[inherit] [&_.edgeLabel]:!font-[inherit] [&_text]:!font-[inherit]"
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  );
}
