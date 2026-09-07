// @vitest-environment node

import { describe, it, expect } from "vitest";
import { localDirectoryLabel } from "./local-directory-label";

// Canonical priority matrix for the display name of a local_directory row.
// The component suites (project-resources-rename.test.tsx) keep the wiring
// and named regressions; the read-order truth lives here.
describe("localDirectoryLabel", () => {
  const ref = (overrides: Partial<{ label: string }> = {}) => ({
    local_path: "/Users/dev/work/game-client",
    daemon_id: "d1",
    ...overrides,
  });

  it("prefers the top-level column — where renames write — over the ref copy", () => {
    expect(
      localDirectoryLabel({
        label: "Renamed Client",
        resource_ref: ref({ label: "Game Client" }),
      }),
    ).toBe("Renamed Client");
  });

  it("falls back to the ref copy: rows created by older clients have no column", () => {
    expect(
      localDirectoryLabel({
        label: null,
        resource_ref: ref({ label: "Game Client" }),
      }),
    ).toBe("Game Client");
  });

  it("shows the path when neither copy names the row — a cleared name must not resurrect", () => {
    expect(
      localDirectoryLabel({ label: null, resource_ref: ref() }),
    ).toBe("/Users/dev/work/game-client");
  });

  it("treats whitespace-only labels as absent instead of rendering a blank row", () => {
    expect(
      localDirectoryLabel({ label: "   ", resource_ref: ref() }),
    ).toBe("/Users/dev/work/game-client");
  });

  it("trims the winning value", () => {
    expect(
      localDirectoryLabel({
        label: "  Renamed Client  ",
        resource_ref: ref({ label: "Game Client" }),
      }),
    ).toBe("Renamed Client");
  });
});
