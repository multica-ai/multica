import type { ArchiveFile } from "./theme-archive";
import { THEME_ID_PATTERN } from "./theme-package";
import { THEME_API_VERSION, themeTokensCss, type BuiltInTheme, type Theme, type ThemeManifest } from "./themes";

/**
 * Writes a theme back out as a theme folder, as a starting point for a new
 * one: `manifest.json` under a new id and name, `theme.css`, and the fonts
 * and images it ships. A built-in theme's token tables become readable
 * `.theme-light` / `.theme-dark` rules, so importing the folder renders the
 * same as the original with its default options.
 */

const isBuiltIn = (theme: Theme): theme is BuiltInTheme => "tokens" in theme;

function themeCssSource(theme: Theme): string {
  if (!isBuiltIn(theme)) return theme.css;
  const compact = themeTokensCss(theme);
  const rest = theme.css.startsWith(compact) ? theme.css.slice(compact.length).trim() : theme.css;
  return [themeTokensCss(theme, { readable: true }), rest].filter(Boolean).join("\n\n");
}

function dataUrlBytes(url: string): Uint8Array {
  const binary = atob(url.slice(url.indexOf(",") + 1));
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

export interface ThemeExportTarget {
  id: string;
  name: string;
}

/** The files of a copy of `theme` named `target`. */
export function exportTheme(theme: Theme, target: ThemeExportTarget): ArchiveFile[] {
  const encoder = new TextEncoder();
  const manifest: ThemeManifest = {
    apiVersion: THEME_API_VERSION,
    id: target.id,
    name: target.name,
    version: "0.1.0",
    description: `Started from ${theme.name}.`,
    ...(theme.options ? { options: theme.options } : {}),
  };
  const header = [
    `/* ${target.name}, started from ${theme.name}.`,
    " * Variables, anchors and the manifest are in beta and may change without notice:",
    " * https://github.com/multica-ai/multica/blob/main/docs/theming.md */",
  ].join("\n");
  const css = [header, themeCssSource(theme)].filter(Boolean).join("\n\n");
  return [
    { path: "manifest.json", data: encoder.encode(`${JSON.stringify(manifest, null, 2)}\n`) },
    { path: "theme.css", data: encoder.encode(`${css}\n`) },
    ...Object.entries(theme.assets ?? {}).map(([path, url]) => ({ path, data: dataUrlBytes(url) })),
  ];
}

/**
 * An id and name for a copy that no theme in `taken` uses yet:
 * `github-copy`, then `github-copy-2` and so on.
 */
export function themeCopyTarget(theme: Theme, taken: Iterable<string>): ThemeExportTarget {
  const used = new Set(taken);
  const base = `${theme.id.slice(0, 30)}-copy`;
  let id = base;
  for (let n = 2; used.has(id); n += 1) id = `${base}-${n}`;
  const suffix = id === base ? "" : ` ${id.slice(base.length + 1)}`;
  return { id: THEME_ID_PATTERN.test(id) ? id : "my-theme", name: `${theme.name} copy${suffix}`.slice(0, 40) };
}
