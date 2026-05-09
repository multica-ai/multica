"use client";

import { Cloud, GitBranch, Globe, Plane, Rocket, Server } from "lucide-react";
import type { LucideIcon } from "lucide-react";

// Phase 6 — adapter glyph + label key resolution. Centralized so the
// dialog dropdown and the swimlane pill render the same icon for the
// same adapter. New adapters fall through to the generic `Globe` icon
// and a free-form label, so the UI never breaks when the server adds
// a new kind.

const ADAPTER_ICONS: Record<string, LucideIcon> = {
  github_actions: GitBranch,
  vercel: Rocket,
  cloudflare: Cloud,
  fly: Plane,
  render: Server,
  generic_webhook: Globe,
};

export function adapterIcon(kind: string | undefined): LucideIcon {
  if (!kind) return Globe;
  return ADAPTER_ICONS[kind] ?? Globe;
}

// Map a server-emitted adapter kind to the corresponding translation
// key inside the `configure_dialog` namespace. Returns `null` for
// unknown kinds so the caller can render the raw string instead — this
// keeps the UI robust to a future server-side adapter the frontend
// hasn't been rebuilt for.
export type AdapterLabelKey =
  | "adapter_github_actions"
  | "adapter_vercel"
  | "adapter_cloudflare"
  | "adapter_fly"
  | "adapter_render"
  | "adapter_generic_webhook";

export function adapterLabelKey(kind: string): AdapterLabelKey | null {
  switch (kind) {
    case "github_actions":
      return "adapter_github_actions";
    case "vercel":
      return "adapter_vercel";
    case "cloudflare":
      return "adapter_cloudflare";
    case "fly":
      return "adapter_fly";
    case "render":
      return "adapter_render";
    case "generic_webhook":
      return "adapter_generic_webhook";
    default:
      return null;
  }
}
