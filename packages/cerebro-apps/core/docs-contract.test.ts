import { readdir, readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const docsRoot = resolve(fileURLToPath(new URL(".", import.meta.url)), "../../../docs/mini-apps");
const repoRoot = resolve(docsRoot, "../..");

async function readSourceText(dir: string): Promise<string> {
  const entries = await readdir(dir, { withFileTypes: true });
  const chunks = await Promise.all(entries.map(async (entry) => {
    if ([".git", "node_modules", "graphify-out", ".turbo"].includes(entry.name)) return "";
    const path = resolve(dir, entry.name);
    if (entry.isDirectory()) return readSourceText(path);
    if (!/\.(go|ts|tsx|js|mjs|md)$/.test(entry.name)) return "";
    return `${path}\n${await readFile(path, "utf8")}`;
  }));
  return chunks.join("\n");
}

describe("mini-app documentation contract", () => {
  it("keeps every canonical guide connected to the shipped APIs", async () => {
    const names = ["README.md", "manifest.md", "sdk.md", "workflows.md", "when-to-build-a-mini-app.md", "plan-coverage.md"];
    const docs = await Promise.all(names.map((name) => readFile(resolve(docsRoot, name), "utf8")));
    const combined = docs.join("\n");
    expect(combined).toContain("connections.call");
    expect(combined).toContain("view.show_and_wait");
    expect(combined).toContain("frontend/index.html");
    expect(docs[4]).toContain("separately deployed app");
    for (let gate = 1; gate <= 11; gate++) expect(docs[5]).toContain(`G${gate}`);
  });

  it("keeps the stronger gates tied to proof names that exist", async () => {
    const coverage = await readFile(resolve(docsRoot, "plan-coverage.md"), "utf8");
    const proofRoots = ["server/internal/cerebro/apps", "server/internal/cerebro/runtime", "apps/cerebro-apps-runtime", "e2e"];
    const proofText = (await Promise.all(proofRoots.map((root) => readSourceText(resolve(repoRoot, root))))).join("\n");
    const requiredProofs = [
      "TestProductionMiniAppsContainNoWorkflowFakes",
      "TestBrokerIssuesAppBoundPersonalKeyAndCachesIt",
      "TestAllFiveWorkflowTriggersHaveProductionRoutes",
      "TestAppConnectionCallAppliesConnectionAndHumanCeilings",
      "TestAppConnectionCallSupportsApprovedMCPToolAndHumanCeiling",
      "TestViewSubmissionCanResumeOneRequest",
      "container-integration.test.mjs",
      "cerebro-mini-apps.spec.ts",
    ];
    for (const proof of requiredProofs) {
      expect(coverage).toContain(proof);
      expect(proofText).toContain(proof);
    }
  });
});
