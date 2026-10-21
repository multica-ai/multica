#!/usr/bin/env node
// Merges docs/unofficial-api/openapi/{openapi.yaml,components/**,paths/**}
// into a single OpenAPI 3.1 document at docs/unofficial-api/dist/openapi.yaml.
//
// Layout contract (see ../README.md):
// - openapi/openapi.yaml holds info/servers/security/tags; its own
//   `paths`/`components` are the merge target and are overwritten here.
// - openapi/components/<type>.yaml (flat) or
//   openapi/components/<type>/*.yaml (nested, for a domain to split a type
//   across files) each contain a plain map merged into components.<type>.
//   <type> must be a real OpenAPI components key (securitySchemes,
//   parameters, schemas, responses, headers, requestBodies, examples,
//   links, callbacks). Colliding keys across files are an error.
// - openapi/paths/**/*.{yaml,yml} each contain a plain map of
//   path-string -> PathItem, merged into the document's top-level `paths`.
//   Colliding path strings across files are an error.

import { readFileSync, writeFileSync, mkdirSync, readdirSync, statSync } from "node:fs";
import { join, dirname, basename, extname } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

// Deliberately not a direct dependency: @stoplight/yaml is pulled in
// transitively by @stoplight/spectral-cli (our only devDependency, already
// required for `npm run lint`), so reusing it avoids a second YAML library
// for the same job. If spectral-cli ever drops it, add it back explicitly.
const require = createRequire(import.meta.url);
const { parse: parseYaml, safeStringify: stringifyYaml } = require("@stoplight/yaml");

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");
const openapiDir = join(root, "openapi");
const distDir = join(root, "dist");

const COMPONENT_TYPES = new Set([
  "securitySchemes",
  "parameters",
  "schemas",
  "responses",
  "headers",
  "requestBodies",
  "examples",
  "links",
  "callbacks",
]);

function isYamlFile(name) {
  return extname(name) === ".yaml" || extname(name) === ".yml";
}

function walkYamlFiles(dir) {
  const out = [];
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    const st = statSync(full);
    if (st.isDirectory()) {
      out.push(...walkYamlFiles(full));
    } else if (st.isFile() && isYamlFile(entry)) {
      out.push(full);
    }
  }
  return out;
}

function loadYaml(path) {
  const content = readFileSync(path, "utf8");
  const doc = parseYaml(content);
  return doc ?? {};
}

function relativeToOpenapiDir(path) {
  return path.slice(openapiDir.length + 1);
}

function mergeComponents() {
  const componentsDir = join(openapiDir, "components");
  const merged = {};
  for (const file of walkYamlFiles(componentsDir)) {
    const rel = relativeToOpenapiDir(file);
    const parentDir = basename(dirname(file));
    const stem = basename(file, extname(file));
    const type = COMPONENT_TYPES.has(parentDir) ? parentDir : stem;
    if (!COMPONENT_TYPES.has(type)) {
      throw new Error(
        `${rel}: cannot infer a components type from this path. ` +
          `Name the file components/<type>.yaml or place it under ` +
          `components/<type>/, where <type> is one of: ` +
          `${[...COMPONENT_TYPES].join(", ")}.`
      );
    }
    const content = loadYaml(file);
    merged[type] ??= {};
    for (const [key, value] of Object.entries(content)) {
      if (Object.prototype.hasOwnProperty.call(merged[type], key)) {
        throw new Error(
          `Duplicate components.${type}.${key}: already defined before ${rel}.`
        );
      }
      merged[type][key] = value;
    }
  }
  return merged;
}

function mergePaths() {
  const pathsDir = join(openapiDir, "paths");
  const merged = {};
  for (const file of walkYamlFiles(pathsDir)) {
    const rel = relativeToOpenapiDir(file);
    const content = loadYaml(file);
    for (const [key, value] of Object.entries(content)) {
      if (Object.prototype.hasOwnProperty.call(merged, key)) {
        throw new Error(`Duplicate path "${key}": already defined before ${rel}.`);
      }
      merged[key] = value;
    }
  }
  return merged;
}

function main() {
  const base = loadYaml(join(openapiDir, "openapi.yaml"));
  const components = mergeComponents();
  const paths = mergePaths();

  const result = {
    ...base,
    paths,
    components: { ...base.components, ...components },
  };

  mkdirSync(distDir, { recursive: true });
  const outPath = join(distDir, "openapi.yaml");
  writeFileSync(outPath, stringifyYaml(result, { lineWidth: 0 }), "utf8");
  console.log(`Wrote ${outPath}`);
  console.log(
    `  paths: ${Object.keys(paths).length}, ` +
      Object.entries(components)
        .map(([type, entries]) => `${type}: ${Object.keys(entries).length}`)
        .join(", ")
  );
}

main();
