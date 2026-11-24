#!/usr/bin/env node
// The part of Composio's v3.1 OpenAPI spec that Multica depends on, and a
// checker for responses against it. scripts/fake-composio.test.mjs runs the
// local fake against it, and server/pkg/composio/composiotest embeds the same
// snapshot for the Go SDK tests and fakes.
//
//   node scripts/composio-spec.mjs update              # fetch the live spec and rewrite the snapshot
//   node scripts/composio-spec.mjs update --from FILE  # same, from a downloaded openapi.json
//
// The snapshot keeps only the responses of OPERATIONS, the components they
// reference, and no schema descriptions. Calling a new Composio endpoint means
// adding it to OPERATIONS, running `update`, and teaching the fake to answer it.
// After an update, run `node --test scripts/fake-composio.test.mjs` and
// `go test ./pkg/composio/... ./internal/integrations/composio/...` (from
// server/): a failure there is Composio's API moving under the fake or the SDK.
//
// .github/workflows/composio-spec-drift.yml runs `update` against the live spec
// every day and fails, with the diff and those test results in its summary,
// when the snapshot no longer matches.

import { readFileSync, realpathSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

export const SPEC_URL = "https://backend.composio.dev/api/v3.1/openapi.json";
export const API_PREFIX = "/api/v3.1";
export const SNAPSHOT = fileURLToPath(new URL("../server/pkg/composio/composiotest/openapi-v3.1.json", import.meta.url));

// Every endpoint server/pkg/composio calls, as "METHOD path" under API_PREFIX.
export const OPERATIONS = [
  "GET /toolkits",
  "GET /toolkits/{slug}",
  "GET /auth_configs",
  "POST /auth_configs",
  "POST /connected_accounts/link",
  "GET /connected_accounts",
  "POST /connected_accounts/{nanoid}/revoke",
  "DELETE /connected_accounts/{nanoid}",
  "GET /tools",
  "GET /tools/{tool_slug}",
  "POST /tools/execute/{tool_slug}",
  "POST /tool_router/session",
];

// Values under these keys are data, not schemas.
const DATA_KEYS = new Set(["example", "examples", "default", "enum", "const"]);

function stripDescriptions(node) {
  if (Array.isArray(node)) return node.map(stripDescriptions);
  if (!node || typeof node !== "object") return node;
  const out = {};
  for (const [key, value] of Object.entries(node)) {
    if (key === "description") continue;
    if (DATA_KEYS.has(key)) out[key] = value;
    else if (key === "properties") {
      out[key] = Object.fromEntries(Object.entries(value).map(([name, schema]) => [name, stripDescriptions(schema)]));
    } else out[key] = stripDescriptions(value);
  }
  return out;
}

function refsIn(node, found = new Set()) {
  if (Array.isArray(node)) node.forEach((item) => refsIn(item, found));
  else if (node && typeof node === "object") {
    if (typeof node.$ref === "string") found.add(node.$ref);
    for (const [key, value] of Object.entries(node)) if (!DATA_KEYS.has(key)) refsIn(value, found);
  }
  return found;
}

export function prune(spec) {
  const paths = {};
  for (const op of OPERATIONS) {
    const [method, path] = op.split(" ");
    const source = spec.paths?.[API_PREFIX + path]?.[method.toLowerCase()];
    if (!source) throw new Error(`${method} ${API_PREFIX}${path} is not in the spec`);
    const responses = {};
    for (const [status, response] of Object.entries(source.responses ?? {})) {
      const { content, ...rest } = response;
      responses[status] = { ...rest };
      if (content) {
        responses[status].content = Object.fromEntries(
          Object.entries(content).map(([type, media]) => [type, { schema: stripDescriptions(media.schema) }]),
        );
      }
    }
    (paths[API_PREFIX + path] ??= {})[method.toLowerCase()] = { operationId: source.operationId, responses };
  }
  const schemas = {};
  const pending = [...refsIn(paths)];
  while (pending.length > 0) {
    const ref = pending.pop();
    const name = /^#\/components\/schemas\/(.+)$/.exec(ref)?.[1];
    if (!name) throw new Error(`unsupported $ref ${ref}`);
    if (schemas[name]) continue;
    if (!spec.components?.schemas?.[name]) throw new Error(`${ref} is not in the spec`);
    schemas[name] = stripDescriptions(spec.components.schemas[name]);
    pending.push(...refsIn(schemas[name]));
  }
  return {
    openapi: spec.openapi,
    info: { title: spec.info?.title, version: spec.info?.version },
    "x-source": SPEC_URL,
    "x-update": "node scripts/composio-spec.mjs update",
    paths,
    components: { schemas: Object.fromEntries(Object.entries(schemas).sort(([a], [b]) => a.localeCompare(b))) },
  };
}

export function loadSnapshot() {
  return JSON.parse(readFileSync(SNAPSHOT, "utf8"));
}

// responseSchema returns the JSON schema of op's response with the given
// status, or undefined when the spec declares none.
export function responseSchema(spec, op, status) {
  const [method, path] = op.split(" ");
  const operation = spec.paths[API_PREFIX + path]?.[method.toLowerCase()];
  if (!operation) throw new Error(`${op} is not in the snapshot; add it to OPERATIONS in scripts/composio-spec.mjs`);
  return operation.responses[String(status)]?.content?.["application/json"]?.schema;
}

// successStatus is the 2xx status op answers with.
export function successStatus(spec, op) {
  const [method, path] = op.split(" ");
  return Number(Object.keys(spec.paths[API_PREFIX + path][method.toLowerCase()].responses).find((s) => s.startsWith("2")));
}

const isZero = (value) =>
  value === null || value === "" || value === 0 || value === false ||
  (Array.isArray(value) && value.length === 0) ||
  (typeof value === "object" && !Array.isArray(value) && Object.keys(value).length === 0);

function typeOf(value) {
  if (value === null) return "null";
  if (Array.isArray(value)) return "array";
  if (typeof value === "number") return Number.isInteger(value) ? "integer" : "number";
  return typeof value;
}

// check lists where value departs from schema: a type or enum mismatch, a
// missing required field, or a field the schema does not define. An object
// schema with properties and no additionalProperties is closed: Composio's
// spec spells out an open object as `additionalProperties: {nullable: true}`.
// oneOf is checked like anyOf.
//
// With fields: true, as for values marshalled from Go structs, required fields
// are not enforced and zero-valued fields are skipped, since an unset Go field
// marshals to one.
export function check(spec, schema, value, { fields = false } = {}) {
  const errors = [];
  const resolve = (s) => {
    while (s?.$ref) s = spec.components.schemas[s.$ref.replace("#/components/schemas/", "")];
    return s;
  };

  function visit(rawSchema, v, at, out) {
    const s = resolve(rawSchema);
    if (!s || typeof s !== "object") return;
    if (v === null) {
      if (!s.nullable && (s.type || s.properties || s.items || s.enum)) out.push(`${at}: is null, the spec does not allow null`);
      return;
    }
    for (const branch of s.allOf ?? []) visit(branch, v, at, out);
    const alternatives = s.anyOf ?? s.oneOf;
    if (alternatives) {
      let closest;
      for (const branch of alternatives) {
        const branchErrors = [];
        visit(branch, v, at, branchErrors);
        if (branchErrors.length === 0) {
          closest = undefined;
          break;
        }
        if (!closest || branchErrors.length < closest.length) closest = branchErrors;
      }
      if (closest) out.push(...closest.map((e) => `${e} (closest of ${alternatives.length} alternatives)`));
      if (!s.type && !s.properties) return;
    }
    const type = s.type ?? (s.properties ? "object" : s.items ? "array" : undefined);
    const actual = typeOf(v);
    if (type && actual !== type && !(type === "number" && actual === "integer")) {
      out.push(`${at}: is ${actual}, the spec says ${type}`);
      return;
    }
    if (s.enum && !s.enum.includes(v)) out.push(`${at}: ${JSON.stringify(v)} is not one of ${JSON.stringify(s.enum)}`);
    if (actual === "array" && s.items) v.forEach((item, i) => visit(s.items, item, `${at}[${i}]`, out));
    if (actual !== "object") return;
    const properties = s.properties ?? {};
    if (!fields) {
      for (const name of s.required ?? []) if (!(name in v)) out.push(`${at}: misses required field ${name}`);
    }
    const extra = s.additionalProperties;
    for (const [name, item] of Object.entries(v)) {
      if (fields && isZero(item)) continue;
      const where = `${at}.${name}`;
      if (Object.hasOwn(properties, name)) visit(properties[name], item, where, out);
      else if (extra === false || (extra === undefined && s.properties)) out.push(`${where}: is not in the spec`);
      else if (extra && typeof extra === "object") visit(extra, item, where, out);
    }
  }

  visit(schema, value, "$", errors);
  return errors;
}

// format is JSON.stringify(value, null, 2) with every object or array that
// fits in a 100-column line written on one line, which keeps the snapshot
// small and its diffs readable.
function format(value, indent = "") {
  const inline = JSON.stringify(value);
  if (inline.length + indent.length <= 100 || value === null || typeof value !== "object") return inline;
  const inner = `${indent}  `;
  const entries = Array.isArray(value)
    ? value.map((item) => inner + format(item, inner))
    : Object.entries(value).map(([key, item]) => `${inner}${JSON.stringify(key)}: ${format(item, inner)}`);
  const [open, close] = Array.isArray(value) ? ["[", "]"] : ["{", "}"];
  return `${open}\n${entries.join(",\n")}\n${indent}${close}`;
}

async function update(args) {
  const from = args[args.indexOf("--from") + 1];
  let spec;
  if (args.includes("--from")) spec = JSON.parse(readFileSync(from, "utf8"));
  else {
    const res = await fetch(SPEC_URL, { signal: AbortSignal.timeout(60_000) });
    if (!res.ok) throw new Error(`GET ${SPEC_URL}: HTTP ${res.status}`);
    spec = await res.json();
  }
  writeFileSync(SNAPSHOT, `${format(prune(spec))}\n`);
  console.log(`[composio-spec] wrote ${OPERATIONS.length} operations of ${spec.info?.title} ${spec.info?.version} to ${SNAPSHOT}`);
}

if (process.argv[1] && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url))) {
  const [command, ...args] = process.argv.slice(2);
  if (command !== "update") {
    console.error("Usage: node scripts/composio-spec.mjs update [--from openapi.json]");
    process.exit(1);
  }
  try {
    await update(args);
  } catch (error) {
    console.error(`[composio-spec] ${error.message}`);
    process.exit(1);
  }
}
