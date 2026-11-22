"use client";

import type { ReactNode } from "react";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { cn } from "@multica/ui/lib/utils";

// ---------------------------------------------------------------------------
// CLI brand channel
//
// The @mention picker surfaces which CLI backs an agent through two
// complementary channels:
//
//   1. Brand corner badge on the avatar (glance recognition) — brand
//      background + white glyph + panel-coloured rim.
//   2. Text tag in the row's fixed side column (precise reading + a11y).
//
// Both map from ONE enum so call sites never hand-roll per-CLI styling. The
// brand hexes are design-mandated: Claude #D97757, Codex
// #10A37F, Cursor #26261F. They are brand constants, not theme colours, so
// they are deliberately literal here (same precedent as provider-logo.tsx).
//
// Product terms "Claude Code" / "Codex CLI" / "Cursor CLI" are
// glossary-protected and intentionally untranslated.
// ---------------------------------------------------------------------------

/**
 * Design-mandated clay accent (#D97757, spec): shared by the CLI brand
 * badge, the current-task heading dot and the selected-row indicator bar.
 * Constant across themes, like the other brand hexes above.
 */
export const MENTION_CLAY = "#D97757";

/** Design-mandated CLI identities carried by a mention row. */
export type MentionCli = "claude-code" | "codex-cli" | "cursor-cli" | "other" | "unknown";

/**
 * Map a runtime provider slug ("claude", "codex", "cursor", "kimi", …) onto
 * the design's CLI enum. The three brand-featured families get their mark;
 * every other real provider collapses to "other" (neutral mark + its own
 * display name in the text tag) rather than "unknown", so a Kimi or Qwen
 * agent keeps its identity instead of reading as broken.
 */
export function mentionCliFromProvider(
  provider: string | null | undefined,
): MentionCli {
  const slug = provider?.trim().toLowerCase();
  if (slug === "claude") return "claude-code";
  if (slug === "codex") return "codex-cli";
  if (slug === "cursor") return "cursor-cli";
  if (slug) return "other";
  return "unknown";
}

interface CliBrandVisual {
  /** Corner-badge disc background (brand colour). */
  badgeBg: string;
  /** Side text-tag foreground. */
  tagText: string;
  /** Side text-tag background. */
  tagBg: string;
  /** Glossary-protected brand name for the text tag. */
  label: string;
  /** White line-art glyph for the corner badge. */
  glyph: ReactNode;
}

function ClaudeGlyph() {
  return (
    <svg
      width="9"
      height="9"
      viewBox="0 0 24 24"
      fill="none"
      stroke="#fff"
      strokeWidth="3.2"
      strokeLinecap="round"
      aria-hidden
    >
      <path d="M12 3.5v17M4.6 7.75l14.8 8.5M19.4 7.75l-14.8 8.5" />
    </svg>
  );
}

function CodexGlyph() {
  return (
    <svg
      width="10"
      height="10"
      viewBox="0 0 24 24"
      fill="none"
      stroke="#fff"
      strokeWidth="2.4"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d="M12 3.5l7.4 4.25v8.5L12 20.5l-7.4-4.25v-8.5z" />
      <path d="M12 8.2l3.3 1.9v3.8L12 15.8l-3.3-1.9v-3.8z" />
    </svg>
  );
}

function CursorGlyph() {
  return (
    <svg
      width="10"
      height="10"
      viewBox="0 0 24 24"
      fill="none"
      stroke="#fff"
      strokeWidth="2.2"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d="M12 3l7 4v10l-7 4-7-4V7z" />
      <path d="M5 7l7 4 7-4M12 11v10" />
    </svg>
  );
}

const CLI_BRAND: Record<Exclude<MentionCli, "unknown">, CliBrandVisual> = {
  "claude-code": {
    badgeBg: "#D97757",
    tagText: "#B85C3E",
    tagBg: "#FBEAE2",
    label: "Claude Code",
    glyph: <ClaudeGlyph />,
  },
  "codex-cli": {
    badgeBg: "#10A37F",
    tagText: "#0B7D62",
    tagBg: "#E2F3ED",
    label: "Codex CLI",
    glyph: <CodexGlyph />,
  },
  "cursor-cli": {
    badgeBg: "#26261F",
    tagText: "#44443B",
    tagBg: "#ECEBE4",
    label: "Cursor CLI",
    glyph: <CursorGlyph />,
  },
  other: {
    badgeBg: "#B9B7AC",
    tagText: "#3D3D3A",
    tagBg: "#F0EEE6",
    label: "",
    glyph: null,
  },
};

/** Display label for the side text tag; empty string means "render nothing". */
export function mentionCliTagLabel(
  cli: MentionCli,
  provider: string | null | undefined,
): string {
  if (cli === "unknown") return "";
  if (cli === "other") {
    return providerLabel(provider);
  }
  return CLI_BRAND[cli].label;
}

/**
 * Human display name for a non-featured provider ("kimi" → "Kimi"), using the
 * shared provider display-name map so the tag matches runtime lists.
 */
function providerLabel(provider: string | null | undefined): string {
  const slug = provider?.trim();
  if (!slug) return "";
  const known = PROVIDER_TAG_LABELS[slug];
  if (known) return known;
  return slug.charAt(0).toUpperCase() + slug.slice(1);
}

// Kept local instead of importing @multica/core/runtimes so this module stays
// render-only; the map mirrors PROVIDER_DISPLAY_NAMES for the families a
// mention row can plausibly meet.
const PROVIDER_TAG_LABELS: Record<string, string> = {
  codebuddy: "CodeBuddy",
  opencode: "OpenCode",
  deveco: "DevEco Code",
  codearts: "CodeArts",
  openclaw: "OpenClaw",
  hermes: "Hermes",
  copilot: "Copilot",
  kimi: "Kimi",
  reasonix: "Reasonix",
  kiro: "Kiro",
  antigravity: "Antigravity",
  traecli: "Trae",
  grok: "Grok",
  qwen: "Qwen Code",
  qwenpaw: "QwenPaw",
  mcode: "MiniMax Code",
  dim: "Dim",
  zeroclaw: "ZeroClaw",
  omp: "Oh-My-Pi",
};

/** Lowercase filter tokens a CLI row answers to ("codex" → Codex agents). */
export function mentionCliFilterTokens(
  cli: MentionCli,
  provider: string | null | undefined,
): string[] {
  const tokens = [mentionCliTagLabel(cli, provider)];
  if (provider?.trim()) tokens.push(provider.trim());
  if (cli !== "unknown" && cli !== "other") tokens.push(cli);
  return tokens
    .filter(Boolean)
    .map((token) => token.toLowerCase())
    .filter((token) => token.length > 0);
}

/**
 * Corner badge: brand-coloured disc + white glyph + 2px rim in the panel
 * colour (reads as white in light theme, panel-tinted in dark — dark
 * theme rule). Decorative: the row's aria-label carries the CLI name.
 */
export function MentionCliCornerBadge({
  cli,
  provider,
  sizePx,
  className,
}: {
  cli: MentionCli;
  provider?: string | null;
  /** Badge diameter in px (geometry tiers live in MentionAvatarBadge). */
  sizePx: number;
  className?: string;
}) {
  const visual = cli === "unknown" ? null : CLI_BRAND[cli];
  const style = {
    width: sizePx,
    height: sizePx,
    ...(visual ? { backgroundColor: visual.badgeBg } : {}),
  };
  return (
    <span
      aria-hidden
      style={style}
      className={cn(
        "absolute right-0 top-0 inline-flex -translate-y-[35%] translate-x-[35%] items-center justify-center rounded-full ring-1 ring-popover",
        !visual && "bg-[#B9B7AC]",
        className,
      )}
    >
      {visual ? (
        visual.glyph ?? (
          <ProviderLogo provider={provider ?? ""} className="h-[68%] w-[68%]" />
        )
      ) : null}
    </span>
  );
}

/**
 * Side text tag — the CLI's "precise channel". Rendered only when a label
 * exists (unknown CLIs stay silent instead of showing "Unknown").
 */
export function MentionCliTextTag({
  cli,
  provider,
  className,
}: {
  cli: MentionCli;
  provider?: string | null;
  className?: string;
}) {
  const label = mentionCliTagLabel(cli, provider);
  if (!label || cli === "unknown") return null;
  const visual = CLI_BRAND[cli];
  return (
    <span
      style={{ color: visual.tagText, backgroundColor: visual.tagBg }}
      className={cn(
        "shrink-0 rounded-sm px-[7px] py-[2px] font-mono text-[10.5px] font-medium tracking-[0.01em]",
        className,
      )}
    >
      {label}
    </span>
  );
}
