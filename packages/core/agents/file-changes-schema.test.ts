// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  parseFileChanges,
  parseFileChangeBody,
  fileChangesStatus,
  canReadFileChange,
} from "./file-changes-schema";

const file = {
  id: "f",
  root_id: "r",
  path: "a.ts",
  change: "modified",
  before_bytes: 1,
  after_bytes: 2,
  before_mode: "100644",
  after_mode: "100644",
  binary: false,
  body_status: "available",
  reason: "",
};
const collection = {
  schema_version: 1,
  collection_id: "c",
  status: "ready",
  reason: "",
  scope: "registered_workdir_roots",
  roots: [],
  files: [file],
  files_total: 1,
  totals_complete: true,
  next_cursor: "100",
};
describe("run file changes boundary", () => {
  it("converts metadata and preserves pagination", () => {
    expect(parseFileChanges(collection)).toMatchObject({
      filesTotal: 1,
      nextCursor: "100",
      files: [{ rootId: "r", beforeBytes: 1 }],
    });
  });
  it("carries optional line-count totals when the wire provides them", () => {
    const withStats = {
      ...collection,
      files: [{ ...file, insertions: 9, deletions: 4 }],
      insertions_total: 9,
      deletions_total: 4,
      stats_complete: true,
    };
    expect(parseFileChanges(withStats)).toMatchObject({
      insertionsTotal: 9,
      deletionsTotal: 4,
      statsComplete: true,
      files: [{ insertions: 9, deletions: 4 }],
    });
    // Snapshots written before the fields landed parse without them.
    const parsed = parseFileChanges(collection);
    expect(parsed.insertionsTotal).toBeUndefined();
    expect(parsed.deletionsTotal).toBeUndefined();
    expect(parsed.statsComplete).toBeUndefined();
    expect(parsed.files[0]!.insertions).toBeUndefined();
  });
  it("rejects negative line counts instead of parsing them", () => {
    // A negative count poisons the whole page, which degrades to the safe
    // unavailable default rather than a partially-parsed snapshot.
    const degradedTotal = parseFileChanges({
      ...collection,
      insertions_total: -1,
    });
    expect(degradedTotal.status).toBe("unavailable");
    expect(degradedTotal.insertionsTotal).toBeUndefined();
    const degradedFile = parseFileChanges({
      ...collection,
      files: [{ ...file, deletions: -2 }],
    });
    expect(degradedFile.status).toBe("unavailable");
    expect(degradedFile.files).toHaveLength(0);
  });
  it.each([
    null,
    {},
    { ...collection, files: null },
    { ...collection, files_total: -1 },
    { ...collection, schema_version: 2 },
    { ...collection, totals_complete: undefined },
  ])("never treats malformed data as an empty successful diff", (raw) => {
    expect(parseFileChanges(raw)).toMatchObject({
      status: "unavailable",
      totalsComplete: false,
    });
  });
  it("preserves unknown states for the UI safe default", () => {
    expect(parseFileChanges({ ...collection, status: "future" }).status).toBe(
      "future",
    );
  });
  it("distinguishes absent sides from empty files", () => {
    expect(
      parseFileChangeBody({
        ...file,
        change: "added",
        before: null,
        after: "",
      }),
    ).toMatchObject({ before: null, after: "", rootId: "r" });
    expect(parseFileChangeBody({ ...file, before: "x" })).toBeNull();
    expect(
      parseFileChangeBody({ ...file, before: null, after: "x" }),
    ).toBeNull();
  });
});

it.each([
  { change: "added", before: null, after: "" },
  { change: "deleted", before: "", after: null },
  { change: "mode_changed", before: "", after: "" },
])("preserves empty body sides for $change", (sides) => {
  expect(parseFileChangeBody({ ...file, ...sides })).toMatchObject(sides);
});

// Canonical contract-v1 enum matrix; component tests cover the remaining review probe and wording.
describe("unknown contract semantics", () => {
  const known = parseFileChanges({
    ...collection,
    roots: [
      { id: "r", label: "repo", kind: "git", coverage: "complete", reason: "" },
    ],
  });
  it.each([
    {
      field: "status",
      page: { ...known, status: "future" },
      expected: "unavailable",
    },
    {
      field: "scope",
      page: { ...known, scope: "future" },
      expected: "unavailable",
    },
    {
      field: "root.kind",
      page: { ...known, roots: [{ ...known.roots[0]!, kind: "future" }] },
      expected: "partial",
    },
    {
      field: "root.coverage",
      page: { ...known, roots: [{ ...known.roots[0]!, coverage: "future" }] },
      expected: "partial",
    },
    {
      field: "file.change",
      page: { ...known, files: [{ ...known.files[0]!, change: "future" }] },
      expected: "partial",
    },
    {
      field: "file.body_status",
      page: { ...known, files: [{ ...known.files[0]!, bodyStatus: "future" }] },
      expected: "partial",
    },
  ])("degrades unknown $field and disallows its body", ({ page, expected }) => {
    expect(fileChangesStatus(page)).toBe(expected);
    expect(canReadFileChange(page, page.files[0]!)).toBe(false);
  });
  it.each(["collecting", "ready", "partial", "unavailable"])(
    "retains known collection status %s",
    (status) => {
      expect(fileChangesStatus({ ...known, status })).toBe(status);
    },
  );
  it.each(["git", "directory"])("accepts known root kind %s", (kind) => {
    const page = { ...known, roots: [{ ...known.roots[0]!, kind }] };
    expect(fileChangesStatus(page)).toBe("ready");
    expect(canReadFileChange(page, page.files[0]!)).toBe(true);
  });
  it.each(["available", "redacted", "binary", "omitted", "unavailable"])(
    "retains known body status %s without confusing body and enumeration completeness",
    (bodyStatus) => {
      const page = { ...known, files: [{ ...known.files[0]!, bodyStatus }] };
      expect(fileChangesStatus(page)).toBe("ready");
      expect(canReadFileChange(page, page.files[0]!)).toBe(
        ["available", "redacted"].includes(bodyStatus),
      );
    },
  );
  it.each(["added", "deleted", "modified", "mode_changed"])(
    "accepts known change kind %s",
    (change) => {
      const page = { ...known, files: [{ ...known.files[0]!, change }] };
      expect(fileChangesStatus(page)).toBe("ready");
      expect(canReadFileChange(page, page.files[0]!)).toBe(true);
    },
  );
  it("does not elevate an unknown machine reason over an unavailable status", () => {
    expect(
      fileChangesStatus({ ...known, status: "unavailable", reason: "future" }),
    ).toBe("unavailable");
  });
});

describe("integrity digests", () => {
  it("carries the capture-time anchors when the wire provides them", () => {
    const parsed = parseFileChanges({
      ...collection,
      digest: "a".repeat(64),
      digest_version: 1,
      files: [{ ...collection.files[0]!, body_sha256: "b".repeat(64) }],
    });
    expect(parsed.digest).toBe("a".repeat(64));
    expect(parsed.digestVersion).toBe(1);
    expect(parsed.files[0]!.bodySha256).toBe("b".repeat(64));
  });

  it("keeps snapshots without digests valid — absence is not verification", () => {
    const parsed = parseFileChanges(collection);
    expect(parsed.digest).toBeUndefined();
    expect(parsed.files[0]!.bodySha256).toBeUndefined();
  });
});
