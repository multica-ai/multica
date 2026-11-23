// @vitest-environment node
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { i18n, type Lang } from "./i18n";

// The per-tool tables on the environment variables page are hand-written in
// every locale, so a new built-in runtime used to land without its
// `MULTICA_<PREFIX>` or `MULTICA_<PREFIX>_ARGS` entry and only got caught by a
// later docs sweep. This reads the daemon sources that define those variables
// and requires each one in every locale. The sources are listed in the "docs"
// group of .github/ci-paths.json so a server-only runtime PR still runs it.

const repoRoot = path.resolve(__dirname, "../../..");
const docsDir = path.resolve(__dirname, "../content/docs");

const sources = {
  probe: "server/internal/daemon/agents_probe.go",
  registry: "server/pkg/agent/builtin_runtimes.go",
  config: "server/internal/daemon/config.go",
  flags: "server/internal/featureflags/keys.go",
} as const;

type Exemption = {
  reason: string;
  // A key in server/internal/featureflags/keys.go that defaults to off.
  // AGENTS.md keeps features behind such a flag out of the public docs.
  flag?: string;
};

// Runtimes the daemon registers that the docs deliberately leave out.
// Remove an entry once the runtime is documented in every locale.
const undocumentedPrefixes: Record<string, Exemption> = {
  MULTICA_MUSEAPP: {
    reason:
      "Muse app remote receptionist (#9155). Whether it is publicly documented awaits a product decision.",
  },
};

type Variable = { name: string; source: string };

function readRepoFile(rel: string): string {
  return readFileSync(path.join(repoRoot, rel), "utf8");
}

function matchAll(source: string, pattern: RegExp): string[] {
  return [...source.matchAll(pattern)].map((match) => match[1]!);
}

// A runtime is detected through MULTICA_<PREFIX>_PATH (a CLI) or
// MULTICA_<PREFIX>_ENDPOINT (a remote service such as museapp).
function extractProbePrefixes(goSource: string): string[] {
  return matchAll(goSource, /"(MULTICA_[A-Z0-9]+)_(?:PATH|ENDPOINT)"/g);
}

function extractRegistryPrefixes(goSource: string): string[] {
  return matchAll(goSource, /\bEnvPrefix:\s*"(MULTICA_[A-Z0-9]+)"/g);
}

function extractArgsVariables(goSource: string): string[] {
  return matchAll(goSource, /\bshellArgsFromEnv\("(MULTICA_[A-Z0-9]+_ARGS)"\)/g);
}

function defaultOffFlags(goSource: string): Set<string> {
  const keyByConst = new Map(
    [...goSource.matchAll(/^\s*([A-Z]\w*)\s*=\s*"([a-z0-9_]+)"/gm)].map(
      (match) => [match[1]!, match[2]!] as const,
    ),
  );
  const off = new Set<string>();
  for (const name of matchAll(goSource, /\bIsEnabled\(ctx,\s*([A-Z]\w*),\s*false\)/g)) {
    const key = keyByConst.get(name);
    if (key) off.add(key);
  }
  return off;
}

function unique(variables: Variable[]): Variable[] {
  const seen = new Map<string, Variable>();
  for (const variable of variables) {
    if (!seen.has(variable.name)) seen.set(variable.name, variable);
  }
  return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name));
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// A prefix counts only as its own inline-code token (`MULTICA_QWEN`), so
// `MULTICA_QWENPAW` or `MULTICA_QWEN_ARGS` cannot stand in for it.
function documentsPrefix(mdx: string, prefix: string): boolean {
  return mdx.includes(`\`${prefix}\``);
}

function documentsVariable(mdx: string, name: string): boolean {
  return new RegExp(`(?<![A-Z0-9_])${escapeRegExp(name)}(?![A-Z0-9_])`).test(mdx);
}

function localeFile(lang: Lang): string {
  return lang === i18n.defaultLanguage
    ? "environment-variables.mdx"
    : `environment-variables.${lang}.mdx`;
}

describe("extractors", () => {
  it("finds CLI and endpoint prefixes, but not other _PATH variables", () => {
    const go = [
      'if e, ok := probe("MULTICA_QWEN_PATH", "qwen", "MULTICA_QWEN_MODEL"); ok {',
      'if e, ok := probe("MULTICA_QWENPAW_PATH", "qwenpaw", ""); ok {',
      'if strings.TrimSpace(os.Getenv("MULTICA_MUSEAPP_ENDPOINT")) != "" {',
      'os.Getenv("MULTICA_DSH_PLUGIN_PATH")',
    ].join("\n");
    expect(extractProbePrefixes(go)).toEqual([
      "MULTICA_QWEN",
      "MULTICA_QWENPAW",
      "MULTICA_MUSEAPP",
    ]);
  });

  it("finds registry prefixes and _ARGS variables", () => {
    expect(extractRegistryPrefixes('\t\tEnvPrefix:         "MULTICA_OMP",')).toEqual([
      "MULTICA_OMP",
    ]);
    expect(
      extractArgsVariables('ampArgs, err := shellArgsFromEnv("MULTICA_AMP_ARGS")'),
    ).toEqual(["MULTICA_AMP_ARGS"]);
  });

  it("treats only flags whose IsEnabled default is false as default-off", () => {
    const go = [
      "const (",
      '\tDocsV1 = "docs_v1"',
      '\tLocalSearchIndex = "local_search_index"',
      '\tagentBuilderCompat = "agents_agent_builder"',
      ")",
      "return flags.IsEnabled(ctx, DocsV1, false)",
      "return flags.IsEnabled(ctx, LocalSearchIndex, true)",
    ].join("\n");
    expect([...defaultOffFlags(go)]).toEqual(["docs_v1"]);
  });

  it("matches prefixes and variables as whole tokens", () => {
    const mdx = "| Qwen | `MULTICA_QWENPAW` |\n`MULTICA_QWEN_ARGS`";
    expect(documentsPrefix(mdx, "MULTICA_QWEN")).toBe(false);
    expect(documentsPrefix(mdx, "MULTICA_QWENPAW")).toBe(true);
    expect(documentsVariable(mdx, "MULTICA_QWEN_ARGS")).toBe(true);
    expect(documentsVariable(mdx, "MULTICA_QWENPAW_ARGS")).toBe(false);
  });
});

describe("environment variables docs", () => {
  const prefixes = unique([
    ...extractProbePrefixes(readRepoFile(sources.probe)).map((name) => ({
      name,
      source: sources.probe,
    })),
    ...extractRegistryPrefixes(readRepoFile(sources.registry)).map((name) => ({
      name,
      source: sources.registry,
    })),
  ]);
  const argsVariables = unique(
    extractArgsVariables(readRepoFile(sources.config)).map((name) => ({
      name,
      source: sources.config,
    })),
  );
  const locales = i18n.languages.map((lang) => {
    const file = localeFile(lang);
    const full = path.join(docsDir, file);
    return { file, mdx: existsSync(full) ? readFileSync(full, "utf8") : null };
  });

  it("still recognizes how the daemon declares runtime variables", () => {
    const names = prefixes.map((p) => p.name);
    expect(names).toContain("MULTICA_CLAUDE");
    expect(names).toContain("MULTICA_OMP");
    expect(names).toContain("MULTICA_MUSEAPP");
    expect(argsVariables.map((v) => v.name)).toContain("MULTICA_CLAUDE_ARGS");
  });

  it("has a page for every locale", () => {
    expect(locales.filter((l) => l.mdx === null).map((l) => l.file)).toEqual([]);
  });

  it("lists every built-in runtime prefix and _ARGS variable in every locale", () => {
    const missing: string[] = [];
    for (const { file, mdx } of locales) {
      if (mdx === null) continue;
      for (const { name, source } of prefixes) {
        if (name in undocumentedPrefixes) continue;
        if (!documentsPrefix(mdx, name)) {
          missing.push(`${file}: \`${name}\` missing from the tool prefix table (declared in ${source})`);
        }
      }
      for (const { name, source } of argsVariables) {
        if (name.replace(/_ARGS$/, "") in undocumentedPrefixes) continue;
        if (!documentsVariable(mdx, name)) {
          missing.push(`${file}: ${name} missing from the MULTICA_<PREFIX>_ARGS list (read in ${source})`);
        }
      }
    }
    expect(
      missing,
      `Add these to apps/docs/content/docs/environment-variables*.mdx in the same PR, or list the prefix in undocumentedPrefixes in ${path.relative(repoRoot, __filename)} with a reason.`,
    ).toEqual([]);
  });

  it("keeps every exemption current", () => {
    const offFlags = defaultOffFlags(readRepoFile(sources.flags));
    const known = new Set(prefixes.map((p) => p.name));
    const stale: string[] = [];
    for (const [prefix, { reason, flag }] of Object.entries(undocumentedPrefixes)) {
      if (!reason.trim()) stale.push(`${prefix}: exemption needs a reason`);
      if (!known.has(prefix)) {
        stale.push(`${prefix}: no longer declared by the daemon; remove the exemption`);
      }
      if (flag !== undefined && !offFlags.has(flag)) {
        stale.push(
          `${prefix}: flag "${flag}" is not a default-off key in ${sources.flags}; document the runtime and remove the exemption`,
        );
      }
      if (locales.every(({ mdx }) => mdx !== null && documentsPrefix(mdx, prefix))) {
        stale.push(`${prefix}: documented in every locale; remove the exemption`);
      }
    }
    expect(stale).toEqual([]);
  });
});
