// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { i18n, type Lang } from "./i18n";
import { undocumentedRuntimes } from "./undocumented-runtimes";

// How many tools Multica supports, and which of them can be steered mid-run
// from which version, are defined once in the daemon but copied by hand into
// the README, the providers page and the landing site in every locale. These
// are the facts AI assistants quote back when recommending Multica, and both
// have drifted before. This reads the daemon sources and requires every copy
// to match. The sources are listed in the "docs" group of .github/ci-paths.json
// so a server-only runtime PR still runs it.

const repoRoot = path.resolve(__dirname, "../../..");
const thisFile = "apps/docs/lib/runtime-facts-coverage.test.ts";
const docsDir = "apps/docs/content/docs";

const sources = {
  families: "server/pkg/agent/agent.go",
  registry: "server/pkg/agent/builtin_runtimes.go",
  probe: "server/internal/daemon/agents_probe.go",
  steering: "server/pkg/agent/version.go",
} as const;

const readmes = ["README.md", "README.zh.md", "README.ja.md"] as const;

type CountMention = { file: string; pattern: RegExp; min: number };

const providersCountPatterns: Record<Lang, RegExp> = {
  en: /(\d+) AI coding tools/g,
  zh: /(\d+) 款 AI 编程工具/g,
  "zh-Hant": /(\d+) 款 AI 程式設計工具/g,
  ko: /(\d+)(?:가지|개) AI 코딩 도구/g,
  ja: /(\d+) 種類の AI コーディングツール/g,
  fr: /(\d+) outils de codage IA/g,
};

// A count that links to the providers page: "[31 AI coding tools](/providers)".
const linkedCount = /\[(\d+)[^\]\n]*\]\(\/providers(?:#[^)]*)?\)/g;

const comparePages = ["linear-jira", "worktree-managers", "openhands-devin-factory"] as const;

// Every hand-written statement of the tool count. Each pattern captures the
// number; the page description and the opening paragraph of providers*.mdx
// share a phrase, hence min: 2. Any other docs page that links a count to
// /providers is checked too, without an entry here.
const countMentions: CountMention[] = [
  { file: "README.md", pattern: /## Supported agent CLIs \((\d+)\)/g, min: 1 },
  { file: "README.md", pattern: /#supported-agent-clis-(\d+)\)/g, min: 1 },
  { file: "README.md", pattern: /authenticated — (\d+) of them/g, min: 1 },
  { file: "README.zh.md", pattern: /一共支持 (\d+) 种/g, min: 1 },
  { file: "README.ja.md", pattern: /(\d+) 種類に対応/g, min: 1 },
  ...i18n.languages.map((lang) => ({
    file: providersFile(lang),
    pattern: providersCountPatterns[lang],
    min: 2,
  })),
  ...comparePages.flatMap((page) =>
    i18n.languages.map((lang) => ({ file: docsFile(`compare/${page}`, lang), pattern: linkedCount, min: 1 })),
  ),
  // The comparison table cell: "31, including Devin CLI and Factory Droid".
  ...i18n.languages.map((lang) => ({
    file: docsFile("compare/openhands-devin-factory", lang),
    pattern: /^\|[^|\n]+\| (\d+)[^|\n\d]*Devin CLI/gm,
    min: 1,
  })),
  { file: docsFile("compare/openhands-devin-factory", "ja"), pattern: /その (\d+) 種類/g, min: 1 },
  { file: "apps/web/features/landing/site/cloud-page.tsx", pattern: /"(\d+) coding agents/g, min: 1 },
  { file: "apps/web/features/landing/i18n/en.ts", pattern: /supports (\d+) coding tools/g, min: 1 },
  { file: "apps/web/features/landing/i18n/zh.ts", pattern: /支持 (\d+) 款 AI 编程工具/g, min: 1 },
  { file: "apps/web/features/landing/i18n/ja.ts", pattern: /(\d+) 種類のコーディングツール/g, min: 1 },
  { file: "apps/web/features/landing/i18n/ko.ts", pattern: /(\d+)개 코딩 도구/g, min: 1 },
  { file: "apps/web/features/landing/i18n/site-en.ts", pattern: /All (\d+) agents/g, min: 1 },
  { file: "apps/web/features/landing/i18n/site-zh.ts", pattern: /全部 (\d+) 款智能体/g, min: 1 },
  { file: "apps/web/features/landing/i18n/site-ja.ts", pattern: /全 (\d+) 種のエージェント/g, min: 1 },
  { file: "apps/web/features/landing/i18n/site-ko.ts", pattern: /에이전트 (\d+)종 전체/g, min: 1 },
];

function docsFile(page: string, lang: Lang): string {
  return lang === i18n.defaultLanguage ? `${docsDir}/${page}.mdx` : `${docsDir}/${page}.${lang}.mdx`;
}

function providersFile(lang: Lang): string {
  return docsFile("providers", lang);
}

function readRepoFile(rel: string): string {
  return readFileSync(path.join(repoRoot, rel), "utf8");
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// The body of a top-level Go declaration: from its opening line to the first
// closing brace at column zero.
function goBlock(source: string, opener: RegExp): string {
  const start = source.search(opener);
  if (start === -1) throw new Error(`no match for ${opener}`);
  const end = source.indexOf("\n}", start);
  return source.slice(start, end === -1 ? undefined : end);
}

function extractSupportedTypes(goSource: string): string[] {
  const block = goBlock(goSource, /^var SupportedTypes = \[\]string\{/m);
  return [...block.matchAll(/"([a-z0-9]+)"/g)].map((m) => m[1]!);
}

type Runtime = { id: string; command: string };

function extractRegistryRuntimes(goSource: string): Runtime[] {
  const block = goBlock(goSource, /^var BuiltinRuntimes = \[\]BuiltinRuntime\{/m);
  return [...block.matchAll(/\bID:\s*"([a-z0-9]+)"[\s\S]*?\bDefaultCommand:\s*"([^"]+)"/g)].map(
    (m) => ({ id: m[1]!, command: m[2]! }),
  );
}

// probe("MULTICA_CURSOR_PATH", "cursor-agent", ...) followed by
// agents["cursor"] = e ties a runtime ID to the CLI the docs list.
function extractProbeRuntimes(goSource: string): Runtime[] {
  return [
    ...goSource.matchAll(
      /probe\("MULTICA_[A-Z0-9]+_PATH",\s*"([^"]+)"[^\n]*\n\s*agents\["([a-z0-9]+)"\]/g,
    ),
  ].map((m) => ({ id: m[2]!, command: m[1]! }));
}

type Steering = { id: string; minimum: string };

function extractSteering(goSource: string): Steering[] {
  const block = goBlock(goSource, /^func SupportsTaskSupplement\(/m);
  const result: Steering[] = [];
  let pending: string[] = [];
  for (const m of block.matchAll(/\bcase\s+([^:\n]+):|\bminimum\s*=\s*"([^"]+)"/g)) {
    if (m[1] !== undefined) {
      pending = [...m[1].matchAll(/"([a-z0-9-]+)"/g)].map((id) => id[1]!);
    } else {
      for (const id of pending) result.push({ id, minimum: m[2]! });
      pending = [];
    }
  }
  return result;
}

type Table = { header: string[]; rows: string[][] };

function splitRow(line: string): string[] {
  return line.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map((c) => c.trim());
}

function parseTables(markdown: string): Table[] {
  const tables: Table[] = [];
  let block: string[] = [];
  for (const line of [...markdown.split("\n"), ""]) {
    if (line.trimStart().startsWith("|")) {
      block.push(line);
      continue;
    }
    if (block.length >= 2 && /^[\s|:-]+$/.test(block[1]!)) {
      tables.push({ header: splitRow(block[0]!), rows: block.slice(2).map(splitRow) });
    }
    block = [];
  }
  return tables;
}

type ToolEntry = { name: string; command: string };

const commandCell = /^`([a-z0-9-]+)`/;

// A tool table pairs each name with its CLI in the next column (`claude`).
// The README packs two pairs into each row, so every such column counts.
function toolEntries(table: Table): ToolEntry[] {
  const columns = table.header
    .map((_, c) => c)
    .filter(
      (c) =>
        c > 0 &&
        table.rows.some((row) => row[c]) &&
        table.rows.every((row) => !row[c] || commandCell.test(row[c])),
    );
  return table.rows.flatMap((row) =>
    columns
      .filter((c) => row[c])
      .map((c) => ({ name: row[c - 1]!, command: commandCell.exec(row[c]!)![1]! })),
  );
}

function toolTable(markdown: string): ToolEntry[] | null {
  const found = parseTables(markdown).map(toolEntries).filter((entries) => entries.length > 0);
  return found.length === 1 ? found[0]! : null;
}

type SteeringRow = { name: string; version: string };

function steeringTable(markdown: string): SteeringRow[] | null {
  const found = parseTables(markdown).filter(
    (t) =>
      t.header.length === 2 &&
      t.rows.length > 0 &&
      t.rows.every((row) => /^\d+\.\d+\.\d+$/.test(row[1] ?? "")),
  );
  return found.length === 1
    ? found[0]!.rows.map((row) => ({ name: row[0]!, version: row[1]! }))
    : null;
}

// Hyphens count as part of a name, so "Pi" is not found inside "Oh-My-Pi".
function namesTool(text: string, name: string): boolean {
  return new RegExp(`(?<![\\w-])${escapeRegExp(name)}(?![\\w-])`).test(text);
}

// Prose drops the "CLI" suffix the tables use ("Kiro CLI" becomes "Kiro").
function proseName(name: string): string {
  return name.replace(/ CLI$/, "");
}

function lineOf(text: string, index: number): number {
  return text.slice(0, index).split("\n").length;
}

describe("extractors", () => {
  it("reads every case of SupportsTaskSupplement with its minimum", () => {
    const go = [
      "func SupportsTaskSupplement(provider, version string) bool {",
      "\tvar minimum string",
      "\tswitch provider {",
      '\tcase "pi":',
      '\t\tminimum = "1.4.0"',
      '\tcase "kiro", "kiro-beta":',
      "\t\t// a comment with case and minimum in it",
      '\t\tminimum = "2.33.0"',
      "\tdefault:",
      "\t\treturn false",
      "\t}",
      "}",
      "",
      "func other() {",
      '\tcase "codex":',
      '\t\tminimum = "0.1.0"',
      "}",
    ].join("\n");
    expect(extractSteering(go)).toEqual([
      { id: "pi", minimum: "1.4.0" },
      { id: "kiro", minimum: "2.33.0" },
      { id: "kiro-beta", minimum: "2.33.0" },
    ]);
  });

  it("ties probed runtime IDs to their commands", () => {
    const go = [
      'if e, ok := probe("MULTICA_CURSOR_PATH", "cursor-agent", "MULTICA_CURSOR_MODEL"); ok {',
      '\tagents["cursor"] = e',
      "}",
    ].join("\n");
    expect(extractProbeRuntimes(go)).toEqual([{ id: "cursor", command: "cursor-agent" }]);
  });

  it("reads tool tables with one or two name/command pairs per row", () => {
    const readme = [
      "| Provider | CLI | Provider | CLI |",
      "| --- | --- | --- | --- |",
      "| Claude Code | `claude` | Kiro CLI | `kiro-cli` |",
      "| ZCode | `zcode` / ZCode Desktop | | |",
    ].join("\n");
    expect(toolTable(readme)).toEqual([
      { name: "Claude Code", command: "claude" },
      { name: "Kiro CLI", command: "kiro-cli" },
      { name: "ZCode", command: "zcode" },
    ]);
    const providers = [
      "| Tool | Detected command | Session resumption | Skill injection path |",
      "|---|---|---:|---|",
      "| Codex | `codex` | ✓ | `$CODEX_HOME/skills/` |",
      "| Dim | `dim` | ✓ | — |",
    ].join("\n");
    expect(toolTable(providers)).toEqual([
      { name: "Codex", command: "codex" },
      { name: "Dim", command: "dim" },
    ]);
  });

  it("finds the steering table by its version column", () => {
    const mdx = ["| Tool | Minimum version |", "|---|---|", "| Pi | 1.4.0 |"].join("\n");
    expect(steeringTable(mdx)).toEqual([{ name: "Pi", version: "1.4.0" }]);
  });

  it("reads counts linked to the providers page", () => {
    const mdx = "on [31 AI coding tools](/providers), see [the matrix](/providers#steer) or [2 runs](/tasks)";
    expect([...mdx.matchAll(linkedCount)].map((m) => m[1])).toEqual(["31"]);
  });

  it("matches tool names as whole names", () => {
    expect(namesTool("Grok, Kiro, Pi, and Antigravity", "Pi")).toBe(true);
    expect(namesTool("Oh-My-Pi and Grok", "Pi")).toBe(false);
    expect(namesTool("Claude Code、Codex、Pi", "Codex")).toBe(true);
  });
});

describe("runtime facts in docs", () => {
  const supportedTypes = extractSupportedTypes(readRepoFile(sources.families));
  const registry = extractRegistryRuntimes(readRepoFile(sources.registry));
  const probed = extractProbeRuntimes(readRepoFile(sources.probe));
  const steering = extractSteering(readRepoFile(sources.steering));

  const runtimeIds = new Set([...supportedTypes, ...registry.map((r) => r.id)]);
  const documentedIds = [...runtimeIds].filter((id) => !(id in undocumentedRuntimes));
  const toolCount = documentedIds.length;
  const commandById = new Map([...probed, ...registry].map((r) => [r.id, r.command]));

  const englishTools = toolTable(readRepoFile(providersFile("en"))) ?? [];
  const englishNameByCommand = new Map(englishTools.map((t) => [t.command, t.name]));

  it("still recognizes how the daemon declares runtimes", () => {
    expect(supportedTypes).toContain("claude");
    expect(registry.map((r) => r.id)).toContain("omp");
    expect(commandById.get("cursor")).toBe("cursor-agent");
    expect(steering.map((s) => s.id)).toContain("claude");
    for (const lang of i18n.languages) {
      const mdx = readRepoFile(providersFile(lang));
      expect(toolTable(mdx), `${providersFile(lang)}: supported tools table`).not.toBeNull();
      expect(steeringTable(mdx), `${providersFile(lang)}: steering table`).not.toBeNull();
    }
  });

  it("lists every steerable tool at its minimum version in every locale", () => {
    const problems: string[] = [];
    for (const lang of i18n.languages) {
      const file = providersFile(lang);
      const mdx = readRepoFile(file);
      const nameByCommand = new Map((toolTable(mdx) ?? []).map((t) => [t.command, t.name]));
      const rows = steeringTable(mdx) ?? [];
      const expected = new Set<string>();
      for (const { id, minimum } of steering) {
        const command = commandById.get(id);
        if (command === undefined) {
          problems.push(
            `${sources.steering}: "${id}" supports steering but has no CLI in ${sources.probe} or ${sources.registry}`,
          );
          continue;
        }
        const name = nameByCommand.get(command);
        if (name === undefined) {
          problems.push(`${file}: the supported tools table has no row for \`${command}\` (runtime "${id}")`);
          continue;
        }
        expected.add(name);
        const row = rows.find((r) => r.name === name);
        if (!row) {
          problems.push(`${file}: add "| ${name} | ${minimum} |" to the steering table`);
        } else if (row.version !== minimum) {
          problems.push(`${file}: steering table lists ${name} at ${row.version}; the minimum is ${minimum}`);
        }
      }
      for (const row of rows) {
        if (!expected.has(row.name)) {
          problems.push(`${file}: steering table lists ${row.name}, which does not support steering; remove the row`);
        }
      }
    }
    expect(
      problems,
      `Steering support is defined by SupportsTaskSupplement in ${sources.steering}.`,
    ).toEqual([]);
  });

  it("names every steerable tool, and no other, in each README", () => {
    const steerable = new Set(
      steering.flatMap(({ id }) => {
        const name = englishNameByCommand.get(commandById.get(id) ?? "");
        return name === undefined ? [] : [proseName(name)];
      }),
    );
    const others = englishTools.map((t) => proseName(t.name)).filter((n) => !steerable.has(n));
    const problems: string[] = [];
    for (const file of readmes) {
      const line = readRepoFile(file)
        .split("\n")
        .find((l) => l.includes("/providers#"));
      if (line === undefined) {
        problems.push(`${file}: no line links to the providers steering section (/providers#...)`);
        continue;
      }
      for (const name of steerable) {
        if (!namesTool(line, name)) problems.push(`${file}: the steering line does not name ${name}`);
      }
      for (const name of others) {
        if (namesTool(line, name)) {
          problems.push(`${file}: the steering line names ${name}, which does not support steering`);
        }
      }
    }
    expect(
      problems,
      `Steering support is defined by SupportsTaskSupplement in ${sources.steering}; tool names come from ${providersFile("en")}.`,
    ).toEqual([]);
  });

  it("states the same tool count everywhere", () => {
    const problems: string[] = [];
    for (const file of [...i18n.languages.map(providersFile), ...readmes]) {
      const tools = toolTable(readRepoFile(file));
      if (tools === null) {
        problems.push(`${file}: could not find the supported tools table`);
      } else if (tools.length !== toolCount) {
        problems.push(`${file}: the supported tools table has ${tools.length} tools, not ${toolCount}`);
      }
    }
    const listed = new Set(countMentions.map((m) => m.file));
    const otherLinkedCounts = readdirSync(path.join(repoRoot, docsDir), { recursive: true })
      .map((entry) => `${docsDir}/${String(entry).split(path.sep).join("/")}`)
      .filter((file) => file.endsWith(".mdx") && !listed.has(file))
      .map((file) => ({ file, pattern: linkedCount, min: 0 }));
    for (const { file, pattern, min } of [...countMentions, ...otherLinkedCounts]) {
      const text = readRepoFile(file);
      const matches = [...text.matchAll(pattern)];
      if (matches.length < min) {
        problems.push(
          `${file}: expected ${min} match(es) for ${pattern}; if the wording changed, update countMentions in ${thisFile}`,
        );
      }
      for (const m of matches) {
        if (Number(m[1]) !== toolCount) {
          problems.push(`${file}:${lineOf(text, m.index!)}: says ${m[1]} tools, not ${toolCount}`);
        }
      }
    }
    expect(
      problems,
      `The daemon registers ${toolCount} documented tools: SupportedTypes in ${sources.families} plus BuiltinRuntimes in ${sources.registry}, minus undocumentedRuntimes in apps/docs/lib/undocumented-runtimes.ts.`,
    ).toEqual([]);
  });
});
