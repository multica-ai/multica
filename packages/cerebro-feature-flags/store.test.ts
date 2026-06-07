import { describe, expect, it } from "vitest";
import { resolveFlag } from "./store";
import { CEREBRO_FLAG_DEFAULTS } from "./registry";

// cerebro_grants defaults to OFF in the registry — a good probe for the
// workspace-override precedence (FIR-2505).
const KEY = "cerebro_grants" as const;

describe("resolveFlag precedence (FIR-2505 workspace overrides)", () => {
  it("falls back to the registry default when nothing is set", () => {
    expect(resolveFlag(KEY, {}, {}, {})).toBe(CEREBRO_FLAG_DEFAULTS[KEY]);
  });

  it("a personal override wins over the default", () => {
    expect(resolveFlag(KEY, { [KEY]: true }, {}, {})).toBe(true);
  });

  it("a LOCKED workspace override wins over a conflicting personal override", () => {
    // Owner forces ON + locked; member tried to turn it off personally.
    expect(resolveFlag(KEY, { [KEY]: false }, { [KEY]: true }, { [KEY]: true })).toBe(true);
  });

  it("a locked workspace OFF cannot be turned on by a member", () => {
    expect(resolveFlag(KEY, { [KEY]: true }, { [KEY]: false }, { [KEY]: true })).toBe(false);
  });

  it("an UNLOCKED workspace override is a soft default a member may still override", () => {
    // Workspace default ON (unlocked), member personally turned it off → off wins.
    expect(resolveFlag(KEY, { [KEY]: false }, { [KEY]: true }, {})).toBe(false);
    // No personal override → the unlocked workspace default applies.
    expect(resolveFlag(KEY, {}, { [KEY]: true }, {})).toBe(true);
  });
});
