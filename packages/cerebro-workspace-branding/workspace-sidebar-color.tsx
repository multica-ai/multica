"use client";

import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import type { Workspace } from "@multica/core/types";
import { hexToHue, hueToOklchPair, hueToHex, extractHueFromUrl } from "./color-extract";

// Fallback palette: each entry is [dark-sidebar, light-sidebar] in oklch.
// Used when no accent color is stored in workspace.settings.
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

// Per-session set of workspace IDs we have already attempted logo extraction
// for. Prevents re-triggering the API call on each effect re-run.
const extractionAttempted = new Set<string>();

export function CerebroWorkspaceSidebarColor() {
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();
  // Capture workspace ref for async callbacks — avoids stale closures.
  const workspaceRef = useRef(workspace);
  workspaceRef.current = workspace;

  useEffect(() => {
    const id = workspace?.id;
    if (typeof document === "undefined" || !id) return;

    const settings = workspace.settings ?? {};
    const manualHex = typeof settings.accent_color_manual === "string" ? settings.accent_color_manual : null;
    const autoHex = typeof settings.accent_color_auto === "string" ? settings.accent_color_auto : null;
    const effectiveHex = manualHex ?? autoHex;

    let dark: string, light: string;
    if (effectiveHex) {
      [dark, light] = hueToOklchPair(hexToHue(effectiveHex));
    } else {
      [dark, light] = PALETTE[colorIndexForId(id)]!;
    }

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

    // Auto-extract from logo for workspaces that have a logo but no stored
    // accent color yet. Fires once per workspace per browser session.
    const avatarUrl = workspace.avatar_url;
    if (avatarUrl && !effectiveHex && !extractionAttempted.has(id)) {
      extractionAttempted.add(id);
      const resolved = resolvePublicFileUrl(avatarUrl);
      if (resolved) {
        void extractHueFromUrl(resolved).then((hue) => {
          if (hue == null) return;
          const hex = hueToHex(hue);
          const ws = workspaceRef.current;
          if (!ws || ws.id !== id) return;
          void api.updateWorkspace(id, {
            settings: { ...ws.settings, accent_color_auto: hex },
          }).then((updated) => {
            qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
              old?.map((w) => (w.id === updated.id ? updated : w)),
            );
          });
        });
      }
    }

    return () => {
      document.getElementById(STYLE_ID)?.remove();
    };
  }, [workspace?.id, workspace?.settings?.accent_color_manual, workspace?.settings?.accent_color_auto, workspace?.avatar_url, qc]);

  return null;
}
