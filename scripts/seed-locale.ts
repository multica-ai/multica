#!/usr/bin/env node
/**
 * AI-seed translation JSON files for a target locale from a source locale.
 *
 * Usage:
 *   pnpm seed-locale --source en --target he
 *   pnpm seed-locale --source en --target ja --model claude-opus-4-7
 *
 * The script reads every `packages/views/locales/<source>/*.json`, sends the
 * key/value tree to Claude via the Anthropic API, and writes the translated
 * result to `packages/views/locales/<target>/*.json`.
 *
 * Requirements:
 *   - ANTHROPIC_API_KEY in the environment (https://console.anthropic.com/).
 *   - Node 22+ (the repo's CI version supports `--experimental-strip-types`).
 *
 * Output discipline:
 *   - The model is instructed to preserve the exact key set, ICU placeholders
 *     ({{name}}, {count}), <Trans> tags, markdown, code identifiers, entity IDs
 *     (MUL-123), and product brand names.
 *   - Validated by `validateKeyParity` before write — the script aborts the
 *     whole run if any namespace returns a different key set than the source.
 *
 * AI output is a starting point, not the final translation. Human review is
 * required before merging — focus on auth/, settings/, onboarding/, common/
 * where tone matters most.
 */

import { readdirSync, readFileSync, mkdirSync, writeFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url));
const LOCALES_DIR = resolve(SCRIPT_DIR, "..", "packages", "views", "locales");

interface Args {
  source: string;
  target: string;
  model: string;
}

function parseArgs(argv: string[]): Args {
  const out: Partial<Args> = { model: "claude-opus-4-7" };
  for (let i = 0; i < argv.length; i++) {
    const flag = argv[i];
    if (flag === "--source") out.source = argv[++i];
    else if (flag === "--target") out.target = argv[++i];
    else if (flag === "--model") out.model = argv[++i];
  }
  if (!out.source || !out.target) {
    console.error("Usage: pnpm seed-locale --source en --target <locale> [--model claude-opus-4-7]");
    process.exit(1);
  }
  return out as Args;
}

type Json = string | number | boolean | null | { [key: string]: Json } | Json[];

function flattenKeys(obj: Json, prefix = ""): string[] {
  if (obj === null || typeof obj !== "object" || Array.isArray(obj)) {
    return [prefix];
  }
  const entries = Object.entries(obj);
  if (entries.length === 0) return [];
  return entries.flatMap(([k, v]) =>
    flattenKeys(v, prefix ? `${prefix}.${k}` : k),
  );
}

function validateKeyParity(sourceJson: Json, targetJson: Json, ns: string): void {
  const sourceKeys = new Set(flattenKeys(sourceJson));
  const targetKeys = new Set(flattenKeys(targetJson));
  const missing = [...sourceKeys].filter((k) => !targetKeys.has(k));
  const extra = [...targetKeys].filter((k) => !sourceKeys.has(k));
  if (missing.length || extra.length) {
    throw new Error(
      `Key drift in ${ns}.json:\n` +
        (missing.length ? `  missing: ${missing.slice(0, 5).join(", ")}${missing.length > 5 ? "…" : ""}\n` : "") +
        (extra.length ? `  extra:   ${extra.slice(0, 5).join(", ")}${extra.length > 5 ? "…" : ""}` : ""),
    );
  }
}

const SYSTEM_PROMPT = `You translate UI strings for Multica, an AI-native task management platform. Translate the provided JSON from English into the target language.

Hard rules:
- Output a single JSON object with the EXACT same key set as the input. Do not add or remove any keys, including nested ones.
- Preserve every ICU/i18next placeholder unchanged: {{name}}, {{count}}, {{identifier}}, {count}, etc.
- Preserve every Trans interpolation tag unchanged: <0>...</0>, <1/>, etc.
- Preserve markdown syntax: **bold**, *italic*, [link](url), code blocks, lists.
- Preserve newlines (\\n) and inline formatting.
- Never translate product names or technical identifiers: Multica, GitHub, Google, Claude Code, Codex, Cursor, ClawHub, Skills.sh, OAuth, CLI, API, EC2, AWS, IAM, daemon, cron, Hermes, CSS, HTML, SKILL.md.
- Never translate entity IDs like MUL-123.
- Keep these lowercase English nouns as-is inside the translated text, just like the existing Chinese (zh-Hans) translations do: issue, issues, task, tasks, skill, skills, todo, in_progress, done, backlog.
- Translate concepts/verbs/UI labels fully: workspace, agent, project, squad, runtime, autopilot, save, cancel, delete, inbox, members, settings.
- Keep brand wordmarks intact (capitalization preserved): Multica.
- For plural pairs (key_one + key_other), translate both and keep the same _one/_other suffix structure.
- Be concise; UI strings are tight.

Return ONLY the translated JSON object — no prose, no markdown code fences.`;

async function callClaude(args: {
  apiKey: string;
  model: string;
  targetLocale: string;
  namespace: string;
  source: Json;
}): Promise<Json> {
  const userPrompt = `Translate this Multica UI namespace from English to locale "${args.targetLocale}". Namespace: ${args.namespace}.json.\n\n${JSON.stringify(args.source, null, 2)}`;

  const res = await fetch("https://api.anthropic.com/v1/messages", {
    method: "POST",
    headers: {
      "content-type": "application/json",
      "x-api-key": args.apiKey,
      "anthropic-version": "2023-06-01",
    },
    body: JSON.stringify({
      model: args.model,
      max_tokens: 16000,
      system: SYSTEM_PROMPT,
      messages: [{ role: "user", content: userPrompt }],
    }),
  });

  if (!res.ok) {
    const body = await res.text();
    throw new Error(`Anthropic API ${res.status}: ${body}`);
  }
  const json = (await res.json()) as { content?: Array<{ type: string; text?: string }> };
  const text = json.content?.find((b) => b.type === "text")?.text ?? "";
  // The model sometimes wraps JSON in ```json fences despite the instruction;
  // strip those defensively before parsing.
  const stripped = text.trim().replace(/^```(?:json)?\n?/, "").replace(/\n?```$/, "");
  return JSON.parse(stripped) as Json;
}

async function main() {
  const { source, target, model } = parseArgs(process.argv.slice(2));
  const apiKey = process.env.ANTHROPIC_API_KEY;
  if (!apiKey) {
    console.error("Set ANTHROPIC_API_KEY to use this script.");
    process.exit(1);
  }

  const sourceDir = resolve(LOCALES_DIR, source);
  const targetDir = resolve(LOCALES_DIR, target);
  mkdirSync(targetDir, { recursive: true });

  const namespaces = readdirSync(sourceDir)
    .filter((f) => f.endsWith(".json"))
    .map((f) => f.replace(/\.json$/, ""))
    .sort();

  console.log(`Seeding ${namespaces.length} namespaces: ${source} → ${target} (model: ${model})`);

  for (const ns of namespaces) {
    const sourcePath = resolve(sourceDir, `${ns}.json`);
    const targetPath = resolve(targetDir, `${ns}.json`);
    const sourceJson = JSON.parse(readFileSync(sourcePath, "utf8")) as Json;
    console.log(`  ${ns}.json…`);
    const translated = await callClaude({ apiKey, model, targetLocale: target, namespace: ns, source: sourceJson });
    validateKeyParity(sourceJson, translated, ns);
    writeFileSync(targetPath, JSON.stringify(translated, null, 2) + "\n", "utf8");
  }

  console.log(`Done. Review ${target}/*.json before committing.`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
