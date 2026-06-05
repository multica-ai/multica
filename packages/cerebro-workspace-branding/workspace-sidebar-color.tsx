"use client";

import { useEffect } from "react";
import { useCurrentWorkspace } from "@multica/core/paths";

// Each entry is [dark-sidebar, light-sidebar] in oklch
const PALETTE: [string, string][] = [
  ["oklch(0.20 0.030 240)", "oklch(0.95 0.015 240)"], // blue
  ["oklch(0.20 0.030 155)", "oklch(0.95 0.015 155)"], // green
  ["oklch(0.20 0.030 295)", "oklch(0.95 0.015 295)"], // purple
  ["oklch(0.20 0.025 35)", "oklch(0.95 0.015 35)"],   // orange
  ["oklch(0.20 0.025 15)", "oklch(0.95 0.015 15)"],   // red
  ["oklch(0.20 0.030 205)", "oklch(0.95 0.015 205)"], // teal
];

function colorIndexForId(id: string): number {
  let h = 0;
  for (let i = 0; i < id.length; i++) {
    h = ((h << 5) - h + id.charCodeAt(i)) | 0;
  }
  return Math.abs(h) % PALETTE.length;
}

const STYLE_ID = "cerebro-ws-sidebar-color";

export function CerebroWorkspaceSidebarColor() {
  const workspace = useCurrentWorkspace();

  useEffect(() => {
    if (typeof document === "undefined" || !workspace?.id) return;

    const [dark, light] = PALETTE[colorIndexForId(workspace.id)]!;
    let el = document.getElementById(STYLE_ID) as HTMLStyleElement | null;
    if (!el) {
      el = document.createElement("style");
      el.id = STYLE_ID;
      document.head.appendChild(el);
    }
    el.textContent = [
      `:root { --sidebar: ${light}; }`,
      `@media (prefers-color-scheme: dark) { :root { --sidebar: ${dark}; } }`,
    ].join("\n");

    return () => {
      document.getElementById(STYLE_ID)?.remove();
    };
  }, [workspace?.id]);

  return null;
}
