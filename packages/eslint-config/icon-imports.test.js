import assert from "node:assert/strict";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { ESLint } from "eslint";
import base from "./base.js";

const BANNED = [
  "lucide-react",
  "react-icons/fa",
  "@heroicons/react/24/outline",
  "@tabler/icons-react",
  "@radix-ui/react-icons",
  "@phosphor-icons/react/dist/ssr",
];
const ALLOWED = ["@multica/ui/icons", "react-icons-extra", "@radix-ui/react-dialog", "@heroicons/reactive"];

async function restricted(eslint, filePath, source) {
  const [result] = await eslint.lintText(`import { X } from "${source}";\nexport { X };\n`, { filePath });
  return result.messages.filter((m) => m.ruleId === "no-restricted-imports").length;
}

const targets = [
  ["base config", new ESLint({ overrideConfigFile: true, overrideConfig: base }), "src/icon.ts"],
  [
    "desktop renderer config",
    new ESLint({ cwd: fileURLToPath(new URL("../../apps/desktop", import.meta.url)) }),
    "src/renderer/src/icon.tsx",
  ],
];

for (const [label, eslint, filePath] of targets) {
  test(`${label} bans icon libraries and their subpaths`, async () => {
    for (const source of BANNED) assert.equal(await restricted(eslint, filePath, source), 1, source);
  });

  test(`${label} allows @multica/ui/icons and look-alike packages`, async () => {
    for (const source of ALLOWED) assert.equal(await restricted(eslint, filePath, source), 0, source);
  });
}
