import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { CEREBRO_FLAGS, CEREBRO_FLAG_GROUPS, CEREBRO_FLAG_DEFAULTS } from "./registry";

/**
 * Guards the generated CLI catalog (embedded by `multica feature list`, see
 * server/cmd/multica/cerebro_feature.go) against drift from this registry.
 * FIR-3009. Regenerate with `pnpm generate:feature-catalog` after editing
 * registry.ts.
 */
describe("cerebro feature catalog sync", () => {
  const catalogPath = resolve(
    dirname(fileURLToPath(import.meta.url)),
    "../../server/cmd/multica/cerebro_feature_catalog.json",
  );

  it("matches the checked-in cerebro_feature_catalog.json (run `pnpm generate:feature-catalog` if this fails)", () => {
    const checkedIn = JSON.parse(readFileSync(catalogPath, "utf8"));
    const expected = {
      groups: CEREBRO_FLAG_GROUPS.map((g) => ({
        key: g.key,
        label: g.label,
        description: g.description,
      })),
      flags: CEREBRO_FLAGS.map((f) => ({
        key: f.key,
        label: f.label,
        description: f.description,
        group: f.group,
        default: CEREBRO_FLAG_DEFAULTS[f.key],
      })),
    };
    expect(checkedIn).toEqual(expected);
  });
});
