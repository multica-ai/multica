import { EMBED_TOKENS, isPlainEmbedValue, type EmbedTokenKind } from "@multica/core/appearance";

/** The host's theme as a sandboxed embed receives it (docs/theming.md, "Embedded content"). */
export interface EmbedTheme {
  /** Token name (with `--`) to a resolved, plain value. */
  tokens: Record<string, string>;
  colorScheme: "light" | "dark";
}

export interface ReadEmbedThemeOptions {
  /** The host token `--background` carries, when the embed sits on something else. */
  background?: string;
}

/** The property each kind is resolved through, so the browser computes it. */
const PROBE_PROPERTY: Record<Exclude<EmbedTokenKind, "keyword">, string> = {
  color: "color",
  length: "border-top-left-radius",
  font: "font-family",
};

const PROBE_STYLE =
  "position:absolute;width:0;height:0;overflow:hidden;visibility:hidden;pointer-events:none;contain:strict";

export function colorSchemeOf(scheme: string | undefined): "light" | "dark" {
  const value = scheme ?? "";
  return /\bdark\b/.test(value) && !/\blight\b/.test(value) ? "dark" : "light";
}

/**
 * Resolves the embed token contract where `host` sits, so a theme's scoped
 * overrides apply. Each token is computed on a throwaway probe inside the host
 * and read back as the browser resolved it: a colour, a length in px, a font
 * stack. What is not plain after that (a `var()`, anything that could load a
 * resource) is dropped, and so is a token the host does not define, so an
 * author's own `var(--x, fallback)` still applies.
 */
export function readEmbedTheme(host: Element | null, { background }: ReadEmbedThemeOptions = {}): EmbedTheme {
  if (!host || typeof getComputedStyle !== "function") return { tokens: {}, colorScheme: "light" };
  const hostStyle = getComputedStyle(host);
  const colorScheme = colorSchemeOf(hostStyle.colorScheme);
  const dark = host.closest(".dark") !== null;
  const doc = host.ownerDocument;
  const probe = doc.createElement("div");
  probe.setAttribute("aria-hidden", "true");
  probe.style.cssText = PROBE_STYLE;

  const pending: { name: string; element: HTMLElement; property: string }[] = [];
  for (const token of EMBED_TOKENS) {
    if (token.kind === "keyword") continue;
    const from = token.name === "--background" && background ? background : null;
    let source = from ? null : (dark && token.darkSource) || token.source;
    if (!source) {
      const name = from ?? token.name;
      if (!hostStyle.getPropertyValue(name).trim()) continue;
      source = `var(${name})`;
    }
    const element = doc.createElement("span");
    const property = PROBE_PROPERTY[token.kind];
    element.style.setProperty(property, source);
    probe.appendChild(element);
    pending.push({ name: token.name, element, property });
  }

  const tokens: Record<string, string> = {};
  host.appendChild(probe);
  try {
    for (const { name, element, property } of pending) {
      const value = getComputedStyle(element).getPropertyValue(property).trim();
      if (isPlainEmbedValue(value)) tokens[name] = value;
    }
  } finally {
    probe.remove();
  }
  tokens["--color-scheme"] = colorScheme;
  return { tokens, colorScheme };
}
