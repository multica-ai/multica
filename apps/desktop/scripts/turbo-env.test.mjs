import { execFileSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const repoRoot = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
);

describe("desktop development environment passthrough", () => {
  it("allows the Platform Agent dev binary and runtime mode through Turbo strict env", () => {
    const turboEntry = resolve(repoRoot, "node_modules", "turbo", "bin", "turbo");
    const output = execFileSync(
      process.execPath,
      [turboEntry, "run", "dev", "--filter=@multica/desktop", "--dry=json"],
      {
        cwd: repoRoot,
        encoding: "utf8",
        env: {
          ...process.env,
          PLATFORM_AGENT_CLI_DEV_BINARY: "/tmp/platform-agent-cli",
          PLATFORM_AGENT_MODE: "http",
        },
      },
    );
    const dryRun = JSON.parse(output);
    const allowed =
      dryRun.globalCacheInputs.environmentVariables.specified.env;

    expect(allowed).toContain("PLATFORM_AGENT_CLI_DEV_BINARY");
    expect(allowed).toContain("PLATFORM_AGENT_MODE");
  });
});
