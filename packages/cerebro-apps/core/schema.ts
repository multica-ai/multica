import type { AppManifest } from "./types";

const semver = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?$/;
const views = new Set(["form", "lookup", "approval"]);

export function validateAppManifest(value: unknown): string[] {
  const errors: string[] = [];
  if (!value || typeof value !== "object") return ["Manifest must be an object"];
  const manifest = value as Partial<AppManifest>;
  if (manifest.schema_version !== "1") errors.push("schema_version must be 1");
  if (!manifest.name?.trim()) errors.push("name is required");
  if (!manifest.version || !semver.test(manifest.version)) errors.push("version must use semantic versioning");
  if (!Array.isArray(manifest.scopes)) errors.push("scopes must be an array");
  for (const view of manifest.views ?? []) {
    if (!view.id || !view.title || !views.has(view.type)) errors.push("Each view requires an id, title, and supported type");
  }
  return errors;
}
