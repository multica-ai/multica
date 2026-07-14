import { describe, expect, it } from "vitest";

import { permissionDetailPath } from "./permission-detail-path";

describe("permissionDetailPath", () => {
  it("encodes workspace slugs and complete tool keys as path segments", () => {
    expect(
      permissionDetailPath("Firtal workspace", "tools:Web fetch/read"),
    ).toBe(
      "/Firtal%20workspace/cerebro/permissions/tools%3AWeb%20fetch%2Fread",
    );
  });
});
