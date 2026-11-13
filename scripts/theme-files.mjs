/**
 * Shared by the theme scripts: loads the appearance sources from
 * packages/core with Node's type stripping, and reads a theme folder or .zip
 * into the files the app's parser takes.
 */

import { readFileSync, readdirSync, statSync } from "node:fs";
import { register } from "node:module";
import { join, relative, resolve, sep } from "node:path";
import { pathToFileURL } from "node:url";

// The core sources import each other without extensions; resolve them to .ts
// so Node's type stripping can run them as they are.
register(
  "data:text/javascript," +
    encodeURIComponent(`
      export async function resolve(specifier, context, next) {
        try {
          return await next(specifier, context);
        } catch (error) {
          if (!specifier.startsWith(".") || !context.parentURL?.endsWith(".ts")) throw error;
          return next(specifier + ".ts", context);
        }
      }
    `),
);

export const repoRoot = resolve(import.meta.dirname, "..");

/** Imports a module of packages/core/appearance. */
export const appearance = (file) => import(pathToFileURL(join(repoRoot, "packages/core/appearance", file)).href);

const { readZip } = await appearance("theme-archive.ts");
const { THEME_PACKAGE_MAX_BYTES } = await appearance("theme-package.ts");

const SKIPPED = new Set(["node_modules", ".git"]);

function readFolder(root, dir = root, files = []) {
  for (const entry of readdirSync(dir)) {
    if (SKIPPED.has(entry) || entry.startsWith(".")) continue;
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) readFolder(root, path, files);
    else files.push({ path: relative(root, path).split(sep).join("/"), data: new Uint8Array(readFileSync(path)) });
  }
  return files;
}

/** The files of a theme folder or .zip, or why they could not be read. */
export async function readTheme(path) {
  if (statSync(path).isDirectory()) return { files: readFolder(path) };
  const archive = await readZip(new Uint8Array(readFileSync(path)), THEME_PACKAGE_MAX_BYTES);
  return archive.ok ? { files: archive.files } : { error: archive.error };
}
