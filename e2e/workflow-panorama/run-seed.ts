/**
 * Seed or repair workflow panorama demo data.
 *
 * Usage:
 * - `bun run e2e/workflow-panorama/run-seed.ts`
 * - `bun run e2e/workflow-panorama/run-seed.ts --repair-existing`
 * - `bun run e2e/workflow-panorama/run-seed.ts --repair-existing --dry-run`
 */

import { createDemoApi } from "./seed-panorama";
import { repairPanoramaWorkflow, seedFullPanoramaWorkflow } from "./seed-full-panorama";

interface WorkflowSummary {
  id: string;
  title: string;
}

async function main() {
  const args = new Set(process.argv.slice(2));
  const repairExisting = args.has("--repair-existing");
  const dryRun = args.has("--dry-run");

  console.log("Logging into demo workspace...");
  const api = await createDemoApi();

  if (repairExisting) {
    await repairExistingWorkflows(api, dryRun);
    return;
  }

  console.log("Seeding full panorama workflow...");
  const seed = await seedFullPanoramaWorkflow(api);

  console.log("");
  console.log("Seed complete");
  console.log(`Workflow: ${seed.workflow.title}`);
  console.log(`ID:       ${seed.workflow.id}`);
  console.log(`Stages:   ${seed.stages.length}`);
  console.log(`Agents:   ${seed.agents.length}`);
  console.log(`Nodes:    ${seed.nodes.length}`);
  console.log(`Edges:    ${seed.edges.length}`);
  console.log(`URL:      http://localhost:3000/tasks/demo111/workflows/${seed.workflow.id}`);
}

async function repairExistingWorkflows(api: Awaited<ReturnType<typeof createDemoApi>>, dryRun: boolean) {
  const workspaces = await api.getWorkspaces();
  const workspace = workspaces.find((item) => item.slug === "demo111");
  if (!workspace) {
    throw new Error("Workspace demo111 not found.");
  }

  const response = await api.listWorkflows(workspace.id);
  const workflows: WorkflowSummary[] = response?.workflows ?? [];
  const repairedPlans = [];

  console.log(`Scanning ${workflows.length} workflows in demo111...`);

  for (const workflow of workflows) {
    const plan = await repairPanoramaWorkflow(api, workflow, dryRun);
    if (!plan) {
      continue;
    }

    repairedPlans.push(plan);
    console.log("");
    console.log(`${dryRun ? "Would repair" : "Repaired"} workflow ${workflow.id}`);
    console.log(`Title: ${workflow.title}`);
    console.log(`Reasons: ${plan.reason.join(", ")}`);
    console.log(`Position updates: ${plan.positionUpdates.length}`);
    console.log(`Missing edges: ${plan.missingEdges.length}`);
  }

  console.log("");
  if (repairedPlans.length === 0) {
    console.log(dryRun ? "No abnormal panorama workflows found." : "No abnormal panorama workflows needed repair.");
    return;
  }

  console.log(dryRun ? `Dry run complete. ${repairedPlans.length} workflows need repair.` : `Repair complete. ${repairedPlans.length} workflows repaired.`);
}

main().catch((err) => {
  console.error("Panorama seed command failed:", err);
  process.exit(1);
});
