#!/usr/bin/env node
/**
 * Reports files in packages/ui that nothing reaches.
 *
 * This exists because knip structurally cannot see them. knip derives entry
 * points from a workspace's package.json `exports` (graph/build.js calls
 * `getEntrySpecifiersFromManifest` unconditionally — there is no config flag to
 * opt out), and packages/ui exports four wildcards:
 *
 *     "./components/ui/*"     -> "./components/ui/*.tsx"
 *     "./components/common/*" -> "./components/common/*.tsx"
 *     "./markdown/*"          -> "./markdown/*.tsx"
 *     "./hooks/*"             -> "./hooks/*.ts"
 *
 * Each expands to a glob covering every file in the directory, so all of them
 * register as entry points and none can ever be reported unused. Setting
 * `entry: []`, negating the paths in `entry`, and `--production` were all
 * tried; manifest entries are a separate code path from configured ones and
 * none of them subtract from it. packages/views is unaffected — 44 of its 45
 * exports name a specific file — which is why knip does flag dead files there.
 *
 * That blind spot is exactly where the 16 dead components removed in MUL-6353
 * lived, so without this check the cleanup has no regression guard at all.
 *
 * Specifiers are resolved against the importing file's own directory, never
 * matched by basename. 17 of the 59 files guarded here share a basename with
 * some other file in the repo (`button`, `card`, `input`, `label`, …), so
 * basename matching would silently pass a dead file whenever any unrelated
 * module imported a same-named sibling.
 *
 * Liveness is reachability, not a reference count: the walk starts from every
 * file outside the guarded directories and follows imports inward, so a pair
 * of dead components importing only each other stays dead.
 *
 * Edges come from real syntax nodes via the TypeScript parser, not from a
 * regex over source text. Text matching counts a commented-out import as a
 * live edge, which is precisely the state a file passes through when its last
 * real importer is removed. 52 specifiers in this repo appear only in comments
 * or prose today; none currently point into packages/ui, but that is a
 * coincidence rather than a property.
 *
 * Run: node scripts/check-ui-wildcard-exports.mjs
 */

import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import ts from "typescript";

const repoRoot = resolve(import.meta.dirname, "..");
const uiRoot = join(repoRoot, "packages", "ui");
const pkgName = "@multica/ui";

const SOURCE_EXTENSIONS = [".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"];
const SKIP_DIRS = new Set(["node_modules", ".git", ".next", ".turbo", "out", "dist", "build"]);

/**
 * Module specifiers this file really depends on: static import/export,
 * `import()`, `require()`, and `import("x")` type nodes. A bare string that
 * happens to sit next to the word `from` is not one of them.
 *
 * `vi.mock("...")` is deliberately excluded. It names a module without
 * importing it, so a component whose only remaining mention is a test mock is
 * dead — counting it would let a component outlive its last real use.
 */
function importedSpecifiers(sourceText, filePath) {
  const source = ts.createSourceFile(filePath, sourceText, ts.ScriptTarget.Latest, true);
  const specifiers = new Set();

  const visit = (node) => {
    if (
      (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) &&
      node.moduleSpecifier &&
      ts.isStringLiteral(node.moduleSpecifier)
    ) {
      specifiers.add(node.moduleSpecifier.text);
    } else if (
      ts.isCallExpression(node) &&
      (node.expression.kind === ts.SyntaxKind.ImportKeyword ||
        (ts.isIdentifier(node.expression) && node.expression.text === "require")) &&
      node.arguments.length > 0 &&
      ts.isStringLiteral(node.arguments[0])
    ) {
      specifiers.add(node.arguments[0].text);
    } else if (
      ts.isImportTypeNode(node) &&
      ts.isLiteralTypeNode(node.argument) &&
      ts.isStringLiteral(node.argument.literal)
    ) {
      specifiers.add(node.argument.literal.text);
    }
    ts.forEachChild(node, visit);
  };

  visit(source);
  return specifiers;
}

function walk(dir, out = []) {
  for (const dirent of readdirSync(dir, { withFileTypes: true })) {
    if (dirent.name.startsWith(".")) continue;
    const full = join(dir, dirent.name);
    if (dirent.isDirectory()) {
      if (!SKIP_DIRS.has(dirent.name)) walk(full, out);
    } else if (SOURCE_EXTENSIONS.some((ext) => dirent.name.endsWith(ext))) {
      out.push(full);
    }
  }
  return out;
}

/** Every on-disk path a specifier could mean, extensionless forms included. */
function resolutionCandidates(base) {
  return [base, ...SOURCE_EXTENSIONS.flatMap((ext) => [`${base}${ext}`, join(base, `index${ext}`)])];
}

const manifest = JSON.parse(readFileSync(join(uiRoot, "package.json"), "utf8"));
const exportEntries = Object.entries(manifest.exports ?? {});

// Derived from the manifest rather than hard-coded: narrow a wildcard export
// and this check narrows with it instead of going quietly stale.
const wildcards = exportEntries
  .filter(([subpath, target]) => subpath.includes("*") && String(target).includes("*"))
  .map(([subpath, target]) => {
    const targetPath = String(target).replace(/^\.\//, "");
    return {
      dir: targetPath.slice(0, targetPath.lastIndexOf("/")),
      extension: targetPath.slice(targetPath.lastIndexOf("*") + 1),
      prefix: subpath.replace(/^\.\//, "").replace("*", ""),
    };
  });

if (wildcards.length === 0) {
  console.log("No wildcard exports in packages/ui — knip covers this workspace on its own.");
  process.exit(0);
}

// A file named by a non-wildcard export is an entry point in its own right, so
// it is exempt. This is what makes the remedy the failure message suggests
// actually work.
const exactExportTargets = new Set(
  exportEntries
    .filter(([subpath, target]) => !subpath.includes("*") && !String(target).includes("*"))
    .map(([, target]) => join(uiRoot, String(target).replace(/^\.\//, ""))),
);

const guarded = new Set();
for (const { dir, extension } of wildcards) {
  let entries;
  try {
    entries = readdirSync(join(uiRoot, dir));
  } catch {
    continue; // Exported directory does not exist yet.
  }
  for (const entry of entries) {
    if (!entry.endsWith(extension)) continue;
    const file = join(uiRoot, dir, entry);
    if (statSync(file).isFile() && !exactExportTargets.has(file)) guarded.add(file);
  }
}

const sourceFiles = walk(repoRoot);
const known = new Set(sourceFiles);

/** Resolve one specifier from one importer to a guarded file, or null. */
function resolveSpecifier(specifier, importer) {
  let base;
  if (specifier.startsWith(".")) {
    base = resolve(dirname(importer), specifier);
  } else if (specifier === pkgName || specifier.startsWith(`${pkgName}/`)) {
    const subpath = specifier.slice(pkgName.length + 1);
    const wildcard = wildcards.find((w) => subpath.startsWith(w.prefix) && w.prefix !== "");
    if (!wildcard) return null;
    base = join(uiRoot, wildcard.dir, subpath.slice(wildcard.prefix.length));
  } else {
    return null; // A bare external package.
  }
  for (const candidate of resolutionCandidates(base)) {
    if (known.has(candidate)) return candidate;
  }
  return null;
}

// Reachability: roots are everything outside the guarded set, so a guarded file
// counts as live only when the walk actually arrives at it.
const imports = new Map();
for (const file of sourceFiles) {
  const found = new Set();
  for (const specifier of importedSpecifiers(readFileSync(file, "utf8"), file)) {
    const target = resolveSpecifier(specifier, file);
    if (target !== null) found.add(target);
  }
  imports.set(file, found);
}

const reached = new Set();
const queue = sourceFiles.filter((file) => !guarded.has(file));
while (queue.length > 0) {
  for (const target of imports.get(queue.pop()) ?? []) {
    if (!reached.has(target)) {
      reached.add(target);
      queue.push(target);
    }
  }
}

const dead = [...guarded]
  .filter((file) => !reached.has(file))
  .map((file) => relative(repoRoot, file).split(sep).join("/"))
  .sort();

if (dead.length > 0) {
  console.error(`Unused files in packages/ui wildcard exports (${dead.length})`);
  for (const file of dead) console.error(`  ${file}`);
  console.error(
    "\nNothing imports these. Delete them — shadcn components come back with" +
      "\n`pnpm ui:add <name>`. If a file is genuinely entry-like, give it a" +
      "\nnon-wildcard entry in packages/ui/package.json `exports`.",
  );
  process.exit(1);
}

console.log(
  `packages/ui wildcard exports clean (${guarded.size} files under ` +
    `${wildcards.map((w) => w.dir).join(", ")}).`,
);
