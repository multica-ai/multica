/**
 * The request Settings → Appearance → Ask an agent sends: what to build, the
 * contract a theme has to meet, how to check it, and where to deliver it.
 * It is sent as the chat's first message, in full, so the person asking sees
 * exactly what the agent was told. The format itself is not repeated here:
 * the agent reads docs/theming.md, the same guide people use, and runs the
 * same checker the store and Import theme rely on.
 */

import { findBuiltInTheme } from "./built-in-themes";
import { THEME_ID_PATTERN } from "./theme-package";

const FALLBACK_ID = "my-theme";
const REPO = "https://github.com/multica-ai/multica";

const FILLER = new Set([
  "a", "an", "and", "at", "by", "for", "from", "in", "inspired", "is", "it", "like", "make",
  "me", "my", "of", "on", "please", "style", "that", "the", "theme", "to", "with",
]);

/**
 * An id for the theme a description asks for: up to four of its Latin words,
 * or `my-theme`, with `-2`, `-3` and so on when `taken` or a built-in theme
 * already uses it.
 */
export function themeIdForDescription(description: string, taken: Iterable<string>): string {
  const words = (description.normalize("NFKD").replace(/\p{M}/gu, "").toLowerCase().match(/[a-z0-9]+/g) ?? [])
    .filter((word) => !FILLER.has(word));
  const base = words.slice(0, 4).join("-").slice(0, 32).replace(/-+$/, "") || FALLBACK_ID;
  const used = new Set(taken);
  const free = (id: string) => !used.has(id) && !findBuiltInTheme(id) && THEME_ID_PATTERN.test(id);
  if (free(base)) return base;
  for (let n = 2; ; n += 1) {
    const id = `${base}-${n}`;
    if (free(id)) return id;
  }
}

export interface ThemeAgentRequest {
  /** What the person asked for, in their words. */
  description: string;
  /** The id the theme is to have; follow-up requests keep it. */
  id: string;
  /**
   * Desktop's themes folder. Multica watches it, so an agent on the same
   * machine can write the theme there and see it apply. Null on web.
   */
  folder: string | null;
}

/** The chat message that asks an agent to build a theme. */
export function themeAgentPrompt({ description, id, folder }: ThemeAgentRequest): string {
  const separator = folder && folder.includes("\\") && !folder.includes("/") ? "\\" : "/";
  const target = folder ? `${folder.replace(/[\\/]+$/, "")}${separator}${id}` : null;
  const deliver = target
    ? [
        `- If \`${folder}\` exists on your machine, write the theme into \`${target}\`. Multica on my computer watches that folder: it switches to the theme once it passes the checks, and applies every later save within a second. Make follow-up changes in those files.`,
        `- If that folder is not on your machine, zip the theme as \`${id}.zip\`, with manifest.json at the top of the zip, and attach it to your reply. I install it from the attachment.`,
      ]
    : [
        `- Zip the theme as \`${id}.zip\`, with manifest.json at the top of the zip, and attach it to your reply. I install it from the attachment. Attach a new zip for each change I ask for.`,
      ];
  return [
    "Build me a Multica theme.",
    "",
    description.trim(),
    "",
    "## Contract",
    "",
    `- The theme's id is \`${id}\`. Keep it for every later change, so each version replaces the last.`,
    `- A theme is a folder with manifest.json, theme.css, and optional fonts and images. docs/theming.md in ${REPO} describes the format, every public variable (the backdrop and glass ones included), and what theme CSS may not do. Read it before writing anything, and use only the variables it lists.`,
    "- Make both light and dark mode look intended, and keep text readable over everything it sits on.",
    "- No JavaScript, no network: every font and image ships in the theme folder.",
    "",
    "## Check before you deliver",
    "",
    "The checker needs Node 22.18 or newer and nothing else:",
    "",
    "```sh",
    `git clone --depth 1 --filter=blob:none --sparse ${REPO}.git multica-themes`,
    "git -C multica-themes sparse-checkout set docs scripts packages/core/appearance packages/ui/styles",
    "node multica-themes/scripts/theme-check.mjs <theme folder or zip>",
    "```",
    "",
    "Deliver only when it reports 0 errors, and say why for any warning you keep. If you can run the full repository (`pnpm install`), also run `pnpm theme:preview <theme folder>` and attach light.png and dark.png.",
    "",
    "## Deliver",
    "",
    ...deliver,
    "",
    "Reply in the language I wrote the description in.",
  ].join("\n");
}
