import { readFileSync, readdirSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const retiredDistribution = [
  ["upstream download page", /https?:\/\/(?:www\.)?multica\.ai\/download\b/i],
  ["upstream installer", /https?:\/\/raw\.githubusercontent\.com\/multica-ai\/multica\//i],
  ["upstream release artifacts", /https?:\/\/github\.com\/multica-ai\/multica\/releases\b/i],
  ["upstream Homebrew distribution", /\bbrew\s+(?:install|tap)\s+multica-ai\//i],
  ["upstream container distribution", /\bghcr\.io\/multica-ai\//i],
  ["published-image upgrade", /\bdocker\s+compose\b[^\n`]*\bpull\b/],
  ["floating application image", /MULTICA_IMAGE_TAG(?::-|=)latest\b/],
  ["upstream checkout for local build", /\bgit\s+clone\b[^\n]*github\.com\/multica-ai\/multica(?:\.git)?\b/],
];

export function distributionViolations(path, content) {
  return content.split("\n").flatMap((line, index) => retiredDistribution
    .filter(([, pattern]) => pattern.test(line))
    .map(([reason]) => `${path}:${index + 1}: ${reason}`));
}

function* documentation(directory) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    if (["node_modules", ".next", ".source", ".turbo"].includes(entry.name)) continue;
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) yield* documentation(path);
    else if (/\.(?:mdx?|[cm]?[jt]sx?|json)$/.test(entry.name)
      && !/\.(?:test|spec|d)\./.test(entry.name)) yield path;
  }
}

export function checkDocsDistribution(sourceRoot = root) {
  const violations = [];
  for (const path of documentation(resolve(sourceRoot, "apps/docs"))) {
    violations.push(...distributionViolations(
      relative(sourceRoot, path).replaceAll("\\", "/"),
      readFileSync(path, "utf8"),
    ));
  }
  return violations;
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const violations = checkDocsDistribution(process.argv[2]);
  if (violations.length) {
    console.error(`Retired documentation distribution paths:\n${violations.join("\n")}`);
    process.exitCode = 1;
  } else console.log("Documentation distribution guard passed.");
}
