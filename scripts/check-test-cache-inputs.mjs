#!/usr/bin/env node
/**
 * Fails when a test reads a file its package's turbo `test` hash cannot see.
 *
 * CI restores the turbo cache, and a package's `test` hash covers only its own
 * files and, through turbo.json's `^cache-inputs` edge, its workspace
 * dependencies. A test that reads anything else (a sibling app's stylesheet,
 * docs/theming.md, another package's manifest) keeps the same hash when only
 * that file changes, so CI replays the previous pass without running it. Such
 * a read has to be declared as a `$TURBO_ROOT$/...` entry in the package's
 * turbo.json `tasks.test.inputs` (MUL-9982, MUL-9992).
 *
 * A declared input is only half of it: the CI job that runs the package's
 * tests is gated by a .github/ci-paths.json group, so the declared files must
 * also be in that group or a PR touching only them skips the job.
 *
 * Reads are found statically. Each test file is parsed with the TypeScript
 * parser and path expressions are folded where their parts are constants:
 * `join`/`resolve` over `__dirname`, `import.meta.dirname`, `process.cwd()`
 * (the package directory under turbo) and string constants, `new URL(x,
 * import.meta.url)`, `fileURLToPath`, `dirname`. On top of that:
 *
 *   - a string literal starting with `../` is resolved from the test file;
 *   - in a test that computes the repository root, a string literal naming an
 *     existing path from the root (`"apps/web/app"`, `"README.md"`) is a read,
 *     since such tests join it onto the root in a helper the folding can't
 *     follow.
 *
 * Only paths that exist count, which is what keeps fixture strings out. A
 * directory or a template-literal prefix is satisfied by any declared input
 * under or above it: which files beneath a scanned directory matter is the
 * author's call, the check only makes sure one was made.
 *
 * Run: node scripts/check-test-cache-inputs.mjs [repo-root]
 */

import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import ts from "typescript";

const repoRoot = resolve(process.argv[2] ?? resolve(import.meta.dirname, ".."));

const TEST_FILE = /\.test\.[cm]?[jt]sx?$/;
const SKIP_DIRS = new Set(["node_modules", ".git", ".next", ".turbo", ".source", "out", "dist", "build", "coverage"]);
const IMPORT_EXTENSIONS = ["", ".ts", ".tsx", ".js", ".mjs", ".cjs", "/index.ts", "/index.tsx", "/index.js"];
const DEPENDENCY_FIELDS = ["dependencies", "devDependencies", "optionalDependencies"];

// Which .github/ci-paths.json group starts the CI job that runs a package's
// tests (.github/workflows/ci.yml). `null`: CI does not run them.
const CI_GROUP_DEFAULT = "frontend";
const CI_GROUPS = new Map([
  ["@multica/docs", "docs"],
  ["@multica/mobile", null],
]);

const toPosix = (path) => path.split(sep).join("/");
const repoRelative = (path) => toPosix(relative(repoRoot, path));
const isInside = (path, dir) => path === dir || path.startsWith(dir + sep);

function readJsonc(path) {
  const { config, error } = ts.parseConfigFileTextToJson(path, readFileSync(path, "utf8"));
  if (error) throw new Error(`${repoRelative(path)}: ${ts.flattenDiagnosticMessageText(error.messageText, "\n")}`);
  return config;
}

/** Glob semantics of turbo inputs and ci-paths: `**`, `*`, `?`, `{a,b}`. */
export function globToRegExp(glob) {
  let out = "";
  for (let i = 0; i < glob.length; i++) {
    const c = glob[i];
    if (c === "*" && glob[i + 1] === "*") {
      i++;
      if (glob[i + 1] === "/") {
        i++;
        out += "(?:.*/)?";
      } else {
        out += ".*";
      }
    } else if (c === "*") out += "[^/]*";
    else if (c === "?") out += "[^/]";
    else if (c === "{") out += "(?:";
    else if (c === "}") out += ")";
    else if (c === ",") out += "|";
    else out += c.replace(/[.+^$()|[\]\\]/g, "\\$&");
  }
  return new RegExp(`^${out}$`);
}

/** The directory part of a glob before its first wildcard. */
const staticBase = (glob) => {
  const wild = glob.search(/[*?{]/);
  if (wild < 0) return glob;
  return glob.slice(0, glob.lastIndexOf("/", wild) + 1).replace(/\/$/, "");
};

/**
 * Whether `glob` can match a file under directory `dir`. Segments are matched
 * one by one, so `apps/*` + `/package.json` reaches into `apps` but not into
 * `apps/desktop/src`; only `**` matches the levels below it.
 */
function reachesInto(glob, dir) {
  const parts = glob.split("/");
  const dirs = dir.split("/");
  for (let i = 0; i < dirs.length; i++) {
    if (i >= parts.length - 1) return false;
    if (parts[i] === "**") return true;
    if (!globToRegExp(parts[i]).test(dirs[i])) return false;
  }
  return true;
}

/** A read of `ref` is declared by `globs` (repo-relative). */
export function covers(globs, ref) {
  if (ref.kind === "file") return globs.some((glob) => globToRegExp(glob).test(ref.path));
  return globs.some((glob) => reachesInto(glob, ref.path));
}

function workspacePackages() {
  const manifest = readFileSync(join(repoRoot, "pnpm-workspace.yaml"), "utf8");
  const groups = [...manifest.matchAll(/^\s+-\s+['"]?([^'"\s]+)\/\*['"]?\s*$/gm)].map((m) => m[1]);
  const packages = [];
  for (const group of groups) {
    const groupDir = join(repoRoot, group);
    if (!existsSync(groupDir)) continue;
    for (const entry of readdirSync(groupDir).sort()) {
      const manifestPath = join(groupDir, entry, "package.json");
      if (!existsSync(manifestPath)) continue;
      packages.push({ dir: join(groupDir, entry), manifest: JSON.parse(readFileSync(manifestPath, "utf8")) });
    }
  }
  return packages;
}

function dependencyDirs(pkg, byName) {
  const seen = new Set();
  const queue = [pkg.manifest];
  while (queue.length > 0) {
    const manifest = queue.pop();
    for (const field of DEPENDENCY_FIELDS) {
      for (const name of Object.keys(manifest[field] ?? {})) {
        const dep = byName.get(name);
        if (!dep || seen.has(dep.dir)) continue;
        seen.add(dep.dir);
        queue.push(dep.manifest);
      }
    }
  }
  return [...seen];
}

function declaredInputs(pkg) {
  const path = join(pkg.dir, "turbo.json");
  if (!existsSync(path)) return [];
  const inputs = readJsonc(path).tasks?.test?.inputs ?? [];
  return inputs.filter((input) => input.startsWith("$TURBO_ROOT$/")).map((input) => input.slice("$TURBO_ROOT$/".length));
}

function walk(dir, keep, found = []) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (!SKIP_DIRS.has(entry.name) && !entry.name.startsWith(".")) walk(join(dir, entry.name), keep, found);
    } else if (keep(entry.name)) {
      found.push(join(dir, entry.name));
    }
  }
  return found;
}

const pathKind = (path) => {
  try {
    return statSync(path).isDirectory() ? "dir" : "file";
  } catch {
    return null;
  }
};

/**
 * Folds path expressions in one test file. A value is `{ text, partial }`:
 * `partial` means only a prefix is known (a template literal or a join whose
 * tail is not a constant). Absolute `text` is a filesystem path; `file://`
 * text is a URL.
 */
function pathReads(file, pkgDir) {
  const source = ts.createSourceFile(file, readFileSync(file, "utf8"), ts.ScriptTarget.Latest, true);
  const fileDir = dirname(file);

  const bindings = new Map();
  const collect = (node) => {
    if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.initializer) {
      bindings.set(node.name.text, bindings.has(node.name.text) ? null : node.initializer);
    }
    ts.forEachChild(node, collect);
  };
  collect(source);

  const calleeName = (expr) =>
    ts.isIdentifier(expr) ? expr.text : ts.isPropertyAccessExpression(expr) ? expr.name.text : null;
  const isImportMeta = (node) =>
    ts.isMetaProperty(node) && node.keywordToken === ts.SyntaxKind.ImportKeyword && node.name.text === "meta";

  const visiting = new Set();
  function evaluate(node) {
    if (!node) return null;
    if (ts.isParenthesizedExpression(node) || ts.isAsExpression(node) || ts.isNonNullExpression(node)) {
      return evaluate(node.expression);
    }
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return { text: node.text, partial: false };
    if (ts.isTemplateExpression(node)) {
      let text = node.head.text;
      for (const span of node.templateSpans) {
        const value = evaluate(span.expression);
        if (!value || value.partial) return { text, partial: true };
        text += value.text + span.literal.text;
      }
      return { text, partial: false };
    }
    if (ts.isIdentifier(node)) {
      if (node.text === "__dirname") return { text: fileDir, partial: false };
      if (node.text === "__filename") return { text: file, partial: false };
      const init = bindings.get(node.text);
      if (!init || visiting.has(node.text)) return null;
      visiting.add(node.text);
      try {
        return evaluate(init);
      } finally {
        visiting.delete(node.text);
      }
    }
    if (ts.isPropertyAccessExpression(node) && isImportMeta(node.expression)) {
      if (node.name.text === "dirname") return { text: fileDir, partial: false };
      if (node.name.text === "filename") return { text: file, partial: false };
      if (node.name.text === "url") return { text: pathToFileURL(file).href, partial: false };
      return null;
    }
    if (ts.isNewExpression(node) && calleeName(node.expression) === "URL" && node.arguments?.length === 2) {
      const [spec, base] = node.arguments.map(evaluate);
      if (!spec || !base?.text.startsWith("file:")) return null;
      return { text: new URL(spec.text, base.text).href, partial: spec.partial };
    }
    if (!ts.isCallExpression(node)) return null;

    const name = calleeName(node.expression);
    if (name === "cwd" && ts.isPropertyAccessExpression(node.expression)) return { text: pkgDir, partial: false };
    const args = node.arguments.map(evaluate);
    if (name === "fileURLToPath" && args[0]?.text.startsWith("file:")) {
      return { text: fileURLToPath(args[0].text), partial: args[0].partial };
    }
    if (name === "dirname" && args[0] && isAbsolute(args[0].text) && !args[0].partial) {
      return { text: dirname(args[0].text), partial: false };
    }
    if (name !== "join" && name !== "resolve") return null;
    const known = [];
    let partial = false;
    for (const arg of args) {
      if (!arg) {
        partial = known.length > 0;
        break;
      }
      known.push(arg.text);
      if (arg.partial) {
        partial = true;
        break;
      }
    }
    if (known.length === 0) return null;
    const start = name === "resolve" ? known.findLastIndex((part) => isAbsolute(part)) : 0;
    if (start < 0 || !isAbsolute(known[start])) return null;
    const joined = join(...known.slice(start));
    return { text: partial ? `${joined}/` : joined, partial };
  }

  const values = [];
  let computesRoot = false;
  const asPath = (value) => (value.text.startsWith("file:") ? { ...value, text: fileURLToPath(value.text) } : value);

  const visit = (node) => {
    if (ts.isCallExpression(node) || ts.isNewExpression(node)) {
      const value = evaluate(node);
      if (value && (isAbsolute(value.text) || value.text.startsWith("file:"))) {
        const path = asPath(value);
        if (resolve(path.text) === repoRoot) computesRoot = true;
        values.push({ ...path, node });
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(source);

  const literals = [];
  const visitLiterals = (node) => {
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) literals.push({ text: node.text, partial: false, node });
    else if (ts.isTemplateExpression(node)) literals.push({ text: node.head.text, partial: true, node });
    ts.forEachChild(node, visitLiterals);
  };
  visitLiterals(source);

  const topLevel = new Set(readdirSync(repoRoot).filter((entry) => !SKIP_DIRS.has(entry)));
  const reads = [];
  const line = (node) => source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1;
  const add = (path, partial, node) => {
    const absolute = resolve(partial && !path.endsWith("/") ? dirname(path) : path);
    const kind = partial ? (pathKind(absolute) === "dir" ? "dir" : null) : pathKind(absolute);
    if (kind) reads.push({ path: absolute, kind, line: line(node) });
  };

  for (const value of values) add(value.text, value.partial, value.node);
  for (const literal of literals) {
    const text = literal.text.replace(/[?#].*$/, "");
    // `"../.."` only climbs to a base that something else is joined onto.
    if (/^\.\.(\/\.\.)*\/?$/.test(text)) continue;
    if (text.startsWith("../")) {
      const base = resolve(fileDir, text);
      if (literal.partial) {
        add(`${base}/`, true, literal.node);
        continue;
      }
      const hit = IMPORT_EXTENSIONS.map((ext) => base + ext).find((candidate) => pathKind(candidate));
      if (hit) add(hit, false, literal.node);
      continue;
    }
    if (!computesRoot || !text || /^[./]|:|\s/.test(text) || !topLevel.has(text.split("/")[0])) continue;
    // A bare name is ambiguous when the package has its own (`package.json`).
    if (!text.includes("/") && (pathKind(join(pkgDir, text)) || pathKind(join(fileDir, text)))) continue;
    add(join(repoRoot, text) + (literal.partial ? "/" : ""), literal.partial, literal.node);
  }
  return reads;
}

export function check() {
  const packages = workspacePackages();
  const byName = new Map(packages.map((pkg) => [pkg.manifest.name, pkg]));
  const ciPaths = JSON.parse(readFileSync(join(repoRoot, ".github/ci-paths.json"), "utf8"));
  const problems = [];

  for (const pkg of packages) {
    if (!pkg.manifest.scripts?.test) continue;
    const name = pkg.manifest.name;
    const pkgRel = repoRelative(pkg.dir);
    const hashed = [pkg.dir, ...dependencyDirs(pkg, byName)];
    const inputs = declaredInputs(pkg);

    const missing = new Map();
    for (const file of walk(pkg.dir, (entry) => TEST_FILE.test(entry))) {
      for (const read of pathReads(file, pkg.dir)) {
        if (!isInside(read.path, repoRoot) || read.path === repoRoot) continue;
        if (read.path.split(sep).includes("node_modules")) continue;
        if (hashed.some((dir) => isInside(read.path, dir))) continue;
        const ref = { path: repoRelative(read.path), kind: read.kind };
        if (covers(inputs, ref)) continue;
        const key = `${ref.path}${ref.kind === "dir" ? "/" : ""}`;
        if (!missing.has(key)) missing.set(key, `${repoRelative(file)}:${read.line}`);
      }
    }
    for (const [path, at] of missing) {
      problems.push(
        `${name}: ${at} reads ${path}, which is outside ${pkgRel} and its workspace dependencies.\n` +
          `    Add "$TURBO_ROOT$/${path}${path.endsWith("/") ? "**" : ""}" (or a narrower glob) to ` +
          `tasks.test.inputs in ${pkgRel}/turbo.json.`,
      );
    }

    const group = CI_GROUPS.has(name) ? CI_GROUPS.get(name) : CI_GROUP_DEFAULT;
    if (group === null) continue;
    const triggers = ciPaths[group] ?? [];
    for (const input of inputs) {
      const base = staticBase(input);
      const files = pathKind(join(repoRoot, base)) === "dir"
        ? walk(join(repoRoot, base), () => true).map(repoRelative).filter((path) => globToRegExp(input).test(path))
        : [base];
      const untriggered = files.filter((path) => !triggers.some((glob) => globToRegExp(glob).test(path)));
      if (untriggered.length > 0) {
        problems.push(
          `${name}: ${pkgRel}/turbo.json declares $TURBO_ROOT$/${input}, but the "${group}" group in ` +
            `.github/ci-paths.json does not match ${untriggered.slice(0, 3).join(", ")}` +
            `${untriggered.length > 3 ? ` and ${untriggered.length - 3} more` : ""}, ` +
            "so a PR changing only that skips the tests.",
        );
      }
    }
  }
  return problems;
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) {
  const problems = check();
  if (problems.length > 0) {
    console.error(`Test reads the turbo cache cannot see (${problems.length}):\n`);
    for (const problem of problems) console.error(`  ${problem}\n`);
    console.error(
      "A cached pass is replayed when only an undeclared file changes. Declare the read where it is named, " +
        "keeping the glob to what the test actually reads.",
    );
    process.exit(1);
  }
  console.log("Every file a test reads outside its package is a declared turbo input.");
}
