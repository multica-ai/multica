// @vitest-environment node
import { readFileSync, readdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { GOVERNANCE_EVENT_KINDS } from "@multica/core/governance";

// The server writes governance kinds from `GovernanceKind*` constants. The
// client lists them in GOVERNANCE_EVENT_KINDS and labels each one under
// log.kind in every locale. A kind added on the server without a client entry
// reads as "unknown" in the log, so this test fails the build instead.

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "../../..");
const localesDir = resolve(here, "../locales");

function serverKinds(): string[] {
  const source = readFileSync(
    resolve(repoRoot, "server/internal/service/governance_log.go"),
    "utf8",
  );
  return [...source.matchAll(/GovernanceKind\w+\s*=\s*"([a-z_]+)"/g)].map(
    (match) => match[1]!,
  );
}

function localeKindKeys(locale: string): string[] {
  const bundle = JSON.parse(
    readFileSync(resolve(localesDir, locale, "governance.json"), "utf8"),
  ) as { log: { kind: Record<string, string> } };
  return Object.keys(bundle.log.kind);
}

describe("governance event kinds", () => {
  it("client kind list matches the kinds the server writes", () => {
    expect([...GOVERNANCE_EVENT_KINDS].sort()).toEqual(serverKinds().sort());
  });

  it("every locale labels every client kind", () => {
    const locales = readdirSync(localesDir, { withFileTypes: true })
      .filter((entry) => entry.isDirectory())
      .map((entry) => entry.name);
    expect(locales.length).toBeGreaterThan(1);
    for (const locale of locales) {
      const labelled = new Set(localeKindKeys(locale));
      const missing = [...GOVERNANCE_EVENT_KINDS, "unknown"].filter(
        (kind) => !labelled.has(kind),
      );
      expect(missing, `${locale} is missing kind labels`).toEqual([]);
    }
  });
});
