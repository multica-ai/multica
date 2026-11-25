import { z } from "zod";
import { parseWithFallback } from "../api/schema";

const fileSchema = z
  .object({
    id: z.string().min(1),
    root_id: z.string().min(1),
    path: z.string(),
    change: z.string(),
    before_bytes: z.number().nonnegative(),
    after_bytes: z.number().nonnegative(),
    before_mode: z.string(),
    after_mode: z.string(),
    binary: z.boolean(),
    body_status: z.string(),
    reason: z.string(),
    // Line-count stats from the collection pass. Optional: snapshots written
    // before the field landed simply carry no counts, and degraded backends
    // must still parse.
    insertions: z.number().int().nonnegative().optional(),
    deletions: z.number().int().nonnegative().optional(),
  })
  .transform((f) => ({
    id: f.id,
    rootId: f.root_id,
    path: f.path,
    change: f.change,
    beforeBytes: f.before_bytes,
    afterBytes: f.after_bytes,
    beforeMode: f.before_mode,
    afterMode: f.after_mode,
    binary: f.binary,
    bodyStatus: f.body_status,
    reason: f.reason,
    // Spread keeps the properties optional in the output type, matching the
    // wire: snapshots and fixtures without counts stay valid.
    ...(f.insertions !== undefined ? { insertions: f.insertions } : {}),
    ...(f.deletions !== undefined ? { deletions: f.deletions } : {}),
  }));
const rootSchema = z.object({
  id: z.string(),
  label: z.string(),
  kind: z.string(),
  coverage: z.string(),
  reason: z.string(),
});
const collectionSchema = z
  .object({
    schema_version: z.literal(1),
    collection_id: z.string().nullable(),
    status: z.string(),
    reason: z.string(),
    scope: z.string(),
    roots: z.array(rootSchema),
    files: z.array(fileSchema),
    files_total: z.number().int().nonnegative(),
    totals_complete: z.boolean(),
    next_cursor: z.string().regex(/^\d+$/).nullable(),
    // Collection-wide line-count totals over the files that could be counted.
    // stats_complete=false means some files (omitted/unavailable bodies) are
    // not covered by the totals; both are absent on snapshots from before the
    // fields landed.
    insertions_total: z.number().int().nonnegative().optional(),
    deletions_total: z.number().int().nonnegative().optional(),
    stats_complete: z.boolean().optional(),
  })
  .refine(
    (c) =>
      c.files_total >= c.files.length &&
      (c.files_total !== 0 || c.next_cursor === null),
    "Inconsistent file total",
  )
  .transform((c) => ({
    collectionId: c.collection_id,
    status: c.status,
    reason: c.reason,
    scope: c.scope,
    roots: c.roots,
    files: c.files,
    filesTotal: c.files_total,
    totalsComplete: c.totals_complete,
    nextCursor: c.next_cursor,
    ...(c.insertions_total !== undefined
      ? { insertionsTotal: c.insertions_total }
      : {}),
    ...(c.deletions_total !== undefined
      ? { deletionsTotal: c.deletions_total }
      : {}),
    ...(c.stats_complete !== undefined
      ? { statsComplete: c.stats_complete }
      : {}),
  }));
const bodySchema = z
  .intersection(
    fileSchema,
    z.object({ before: z.string().nullable(), after: z.string().nullable() }),
  )
  .refine((f) => {
    if (f.bodyStatus !== "available" && f.bodyStatus !== "redacted")
      return true;
    switch (f.change) {
      case "added":
        return f.before === null && f.after !== null;
      case "deleted":
        return f.before !== null && f.after === null;
      case "modified":
      case "mode_changed":
        return f.before !== null && f.after !== null;
      default:
        return false;
    }
  }, "Missing file side");
export type RunFileChange = z.output<typeof fileSchema>;
export type RunFileChanges = z.output<typeof collectionSchema>;
export type RunFileChangeBody = z.output<typeof bodySchema>;
export const unavailableFileChanges: RunFileChanges = {
  collectionId: null,
  status: "unavailable",
  reason: "invalid_response",
  scope: "",
  roots: [],
  files: [],
  filesTotal: 0,
  totalsComplete: false,
  nextCursor: null,
};
export function parseFileChanges(raw: unknown): RunFileChanges {
  return parseWithFallback(raw, collectionSchema, unavailableFileChanges, {
    endpoint: "task/file-changes",
  });
}
export function parseFileChangeBody(raw: unknown): RunFileChangeBody | null {
  return parseWithFallback<RunFileChangeBody | null>(raw, bodySchema, null, {
    endpoint: "task/file-changes/body",
  });
}

const knownStatuses = new Set([
  "collecting",
  "ready",
  "partial",
  "unavailable",
]);
const knownRootKinds = new Set(["git", "directory"]);
const knownChanges = new Set(["added", "deleted", "modified", "mode_changed"]);
const knownBodyStatuses = new Set([
  "available",
  "redacted",
  "binary",
  "omitted",
  "unavailable",
]);

export function isKnownFileChangesScope(scope: string): boolean {
  return scope === "registered_workdir_roots";
}

/** Keep unknown wire values, but never interpret them as complete coverage. */
export function fileChangesStatus(
  page: RunFileChanges,
): "collecting" | "ready" | "partial" | "unavailable" {
  if (!knownStatuses.has(page.status) || !isKnownFileChangesScope(page.scope))
    return "unavailable";
  switch (page.status) {
    case "collecting":
      return "collecting";
    case "ready":
      return page.totalsComplete &&
        page.roots.every(
          (root) =>
            knownRootKinds.has(root.kind) && root.coverage === "complete",
        ) &&
        page.files.every(
          (file) =>
            knownChanges.has(file.change) &&
            knownBodyStatuses.has(file.bodyStatus),
        )
        ? "ready"
        : "partial";
    case "partial":
      return "partial";
    default:
      return "unavailable";
  }
}

/** Unknown scope/root/file semantics cannot authorize a body interpretation. */
export function canReadFileChange(
  page: RunFileChanges,
  file: RunFileChange,
): boolean {
  const root = page.roots.find((candidate) => candidate.id === file.rootId);
  return (
    fileChangesStatus(page) !== "unavailable" &&
    !!root &&
    knownRootKinds.has(root.kind) &&
    ["complete", "partial"].includes(root.coverage) &&
    knownChanges.has(file.change) &&
    !file.binary &&
    ["available", "redacted"].includes(file.bodyStatus)
  );
}
