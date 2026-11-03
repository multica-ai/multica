#!/usr/bin/env node
// Regenerates server/internal/service/company_template_data.json, the starter
// company's data as the Go apply service reads it. The TypeScript definitions
// are the single source of truth: the squad cards and stage owners in
// packages/core/squads/company-template.ts and the agent instruction templates
// in packages/core/agents/instruction-templates.ts.
//
// Run via `pnpm generate:company-template`. A core test compares the committed
// JSON with the TypeScript constants, so the two cannot drift silently.

import { writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const coreDir = resolve(repoRoot, "packages/core");
const outPath = resolve(repoRoot, "server/internal/service/company_template_data.json");

// One entry that re-exports both modules, bundled so zod and the templates
// resolve from packages/core/node_modules.
const entry = `
export {
  COMPANY_FLOOR_KEYS,
  COMPANY_FLOOR_NAMES,
  COMPANY_SQUAD_CARDS,
  COMPANY_STAGE_OWNERS,
  COMPANY_START_OPTIONS,
} from "./squads/company-template";
export { AGENT_INSTRUCTION_TEMPLATES } from "./agents/instruction-templates";
`;

const result = await build({
  stdin: { contents: entry, resolveDir: coreDir, sourcefile: "company-template-entry.ts", loader: "ts" },
  bundle: true,
  format: "esm",
  platform: "node",
  write: false,
  logLevel: "silent",
});
const code = result.outputFiles[0].text;
const mod = await import(`data:text/javascript;base64,${Buffer.from(code).toString("base64")}`);

const data = {
  generated_by: "scripts/generate-company-template.mjs",
  floor_keys: [...mod.COMPANY_FLOOR_KEYS],
  floor_names: { ...mod.COMPANY_FLOOR_NAMES },
  start_options: [...mod.COMPANY_START_OPTIONS],
  squads: mod.COMPANY_SQUAD_CARDS,
  stage_owners: mod.COMPANY_STAGE_OWNERS,
  instruction_templates: mod.AGENT_INSTRUCTION_TEMPLATES.map(({ id, instructions }) => ({ id, instructions })),
};

writeFileSync(outPath, `${JSON.stringify(data, null, 2)}\n`);
console.log(`wrote ${outPath} (${data.squads.length} squads, ${data.instruction_templates.length} instruction templates)`);
