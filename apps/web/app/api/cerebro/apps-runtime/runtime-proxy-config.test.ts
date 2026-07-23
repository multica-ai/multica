import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

describe("Apps runtime proxy configuration", () => {
  it("leaves Apps runtime requests to the runtime-configured route handler", () => {
    const appDirectory = process.cwd();
    const nextConfig = readFileSync(resolve(appDirectory, "next.config.mjs"), "utf8");
    const route = readFileSync(resolve(appDirectory, "app/api/cerebro/apps-runtime/[...path]/route.ts"), "utf8");

    expect(appDirectory).toContain("/apps/web");
    expect(nextConfig).not.toContain('source: "/api/cerebro/apps-runtime/:path*"');
    expect(route).toContain("process.env.CEREBRO_APPS_RUNTIME_URL");
    expect(route).toContain('"access-control-allow-origin"');
    expect(route).toContain('"access-control-allow-credentials"');
  });
});
