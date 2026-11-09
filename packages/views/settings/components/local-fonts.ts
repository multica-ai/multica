"use client";

import { create } from "zustand";
import { isLocalFont } from "@multica/core/appearance";

/**
 * Font families installed on this device that can draw Han characters, for
 * the CJK font pickers. Uses the Local Font Access API: available in Desktop
 * and Chromium browsers (which ask for permission), absent elsewhere.
 */

type LocalFontsStatus = "idle" | "loading" | "ready" | "unavailable" | "error";

interface LocalFontsState {
  status: LocalFontsStatus;
  families: string[];
  load: () => Promise<void>;
}

type QueryLocalFonts = () => Promise<readonly { family: string }[]>;

function queryLocalFonts(): QueryLocalFonts | null {
  if (typeof window === "undefined") return null;
  const query = (window as unknown as { queryLocalFonts?: QueryLocalFonts }).queryLocalFonts;
  return typeof query === "function" ? query.bind(window) : null;
}

const PROBE = "永字";

/**
 * Whether `family` has its own Han glyphs: drawn with it first, the probe
 * differs from the generic fallback. Checked against two fallbacks so a
 * family that is itself the default for one of them still counts.
 */
function makeHanProbe(): ((family: string) => boolean) | null {
  const canvas = document.createElement("canvas");
  canvas.width = 48;
  canvas.height = 24;
  const ctx = canvas.getContext("2d", { willReadFrequently: true });
  if (!ctx) return null;
  ctx.textBaseline = "top";
  const draw = (font: string) => {
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.font = `20px ${font}`;
    ctx.fillText(PROBE, 0, 0);
    return ctx.getImageData(0, 0, canvas.width, canvas.height).data;
  };
  const same = (a: Uint8ClampedArray, b: Uint8ClampedArray) => a.every((v, i) => v === b[i]);
  const fallbacks = ["sans-serif", "serif"].map((generic) => [generic, draw(generic)] as const);
  return (family) =>
    fallbacks.some(([generic, base]) => !same(draw(`"${family}", ${generic}`), base));
}

export const useLocalCjkFonts = create<LocalFontsState>()((set, get) => ({
  status: queryLocalFonts() ? "idle" : "unavailable",
  families: [],
  load: async () => {
    const { status } = get();
    if (status === "loading" || status === "ready") return;
    const query = queryLocalFonts();
    if (!query) {
      set({ status: "unavailable" });
      return;
    }
    set({ status: "loading" });
    try {
      const fonts = await query();
      const probe = makeHanProbe();
      const families = [...new Set(fonts.map((font) => font.family))]
        .filter((family) => isLocalFont(`local:${family}`))
        .filter((family) => (probe ? probe(family) : true))
        .sort((a, b) => a.localeCompare(b));
      set({ status: "ready", families });
    } catch {
      set({ status: "error" });
    }
  },
}));
