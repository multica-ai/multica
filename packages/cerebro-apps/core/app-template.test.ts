import { describe, expect, it } from "vitest";

import { createAppTemplate } from "./app-template";

describe("createAppTemplate", () => {
  it("creates every required source file without credentials or production URLs", () => {
    const files = createAppTemplate("Returns helper", "0.1.0");
    expect(files.map((file) => file.path)).toEqual([
      "app.json",
      "frontend/index.html",
      "frontend/app.js",
      "backend/index.mjs",
    ]);
    const manifest = JSON.parse(files[0]?.content ?? "{}") as Record<string, unknown>;
    expect(manifest).toMatchObject({ manifest: { schema_version: "1", name: "Returns helper", version: "0.1.0" } });
    expect(files.find((file) => file.path === "frontend/index.html")?.content).toContain('crossorigin="use-credentials"');
    expect(files.find((file) => file.path === "frontend/app.js")?.content).toContain("createMulticaApp");
    expect(files.find((file) => file.path === "frontend/app.js")?.content).toContain("window.location.pathname");
    expect(JSON.stringify(files)).not.toMatch(/multica\.firtal\.com|registry\.firtal\.com|api[_-]?key|secret/i);
  });
});
