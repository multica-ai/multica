import { useEffect, useState } from "react";
import { HTML_BLOCK_THEME_TOKENS } from "../utils/html-block-document";

/**
 * Everything that decides what a sandboxed block copies out of the theme: the
 * attributes that pick a theme, and what it resolves to on the root. Tokens
 * are read resolved so a theme written straight into `style` still counts.
 */
function readThemeSignature(): string {
  const root = document.documentElement;
  const styles = getComputedStyle(root);
  return [
    root.className,
    root.getAttribute("data-theme"),
    document.body?.className,
    document.body?.getAttribute("data-theme"),
    styles.colorScheme,
    styles.fontFamily,
    ...HTML_BLOCK_THEME_TOKENS.map((name) => styles.getPropertyValue(name)),
  ].join("|");
}

/**
 * A counter that bumps whenever the app theme changes.
 *
 * Sandboxed blocks copy resolved theme colors into their iframe document, so
 * they cannot follow a theme switch through CSS; they rebuild when this bumps.
 *
 * A class, style or data-theme change on <html>/<body>, or an OS color-scheme
 * change, only prompts a check: it bumps when the signature actually moved. A
 * dialog's scroll lock writes `overflow` into that `style` on every open and
 * close, and taking it for a theme switch re-rendered every diagram on the
 * page — on close, a long task that stalled the dialog's exit and made the
 * page flash (MUL-7760).
 */
export function useThemeVersion(): number {
  const [themeVersion, setThemeVersion] = useState(0);

  useEffect(() => {
    let signature = readThemeSignature();
    const bumpIfThemeChanged = () => {
      const next = readThemeSignature();
      if (next === signature) return;
      signature = next;
      setThemeVersion((version) => version + 1);
    };
    const observer = new MutationObserver(bumpIfThemeChanged);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "style", "data-theme"],
    });
    if (document.body) {
      observer.observe(document.body, {
        attributes: true,
        attributeFilter: ["class", "style", "data-theme"],
      });
    }

    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    mediaQuery.addEventListener("change", bumpIfThemeChanged);

    return () => {
      observer.disconnect();
      mediaQuery.removeEventListener("change", bumpIfThemeChanged);
    };
  }, []);

  return themeVersion;
}
