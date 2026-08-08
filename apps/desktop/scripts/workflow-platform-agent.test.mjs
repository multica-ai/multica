import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const repoRoot = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
);

describe.each(["desktop-smoke.yml", "release.yml"])(
  "%s Platform Agent release staging",
  (workflowName) => {
    const workflow = readFileSync(
      resolve(repoRoot, ".github", "workflows", workflowName),
      "utf8",
    );

    it("downloads the centralized pinned release into runner.temp before strict packaging", () => {
      const download = workflow.indexOf(
        "node apps/desktop/scripts/platform-agent-cli-release.mjs",
      );
      const packageDesktop = workflow.lastIndexOf("node scripts/package.mjs");
      expect(download).toBeGreaterThan(-1);
      expect(workflow.slice(download, packageDesktop)).toContain(
        '${{ runner.temp }}/platform-agent-cli',
      );
      expect(packageDesktop).toBeGreaterThan(download);
    });

    it("does not duplicate the repository or version pin outside the shared script", () => {
      expect(workflow).not.toContain("multica-ai/platform-agent-cli");
      expect(workflow).not.toMatch(/PLATFORM_AGENT_CLI_VERSION:\s*["']?0\.2\.0/);
    });
  },
);
