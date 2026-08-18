import { describe, expect, it } from "vitest";
import {
  PlatformExtensionDetailSchema,
  PlatformExtensionImportResultSchema,
  PlatformExtensionListSchema,
} from "./schemas";

const mapping = {
  release: {
    id: "11111111-1111-4111-8111-111111111111",
    extension_key: "research-team",
    version: "1.0.0",
    digest: `sha256:${"a".repeat(64)}`,
  },
  runtime: {
    id: "22222222-2222-4222-8222-222222222222",
    provider: "platform-agent-cli",
    name: "Platform Agent CLI",
  },
  squad: {
    id: "33333333-3333-4333-8333-333333333333",
    name: "Research Team v1.0.0",
  },
  agents: [
    {
      source_key: "leader",
      id: "44444444-4444-4444-8444-444444444444",
      name: "Research Team v1.0.0 / Leader",
      leader: true,
      runtime: {
        id: "22222222-2222-4222-8222-222222222222",
        provider: "platform-agent-cli",
        name: "Platform Agent CLI",
      },
    },
  ],
  skills: [
    {
      source_key: "research",
      id: "55555555-5555-4555-8555-555555555555",
      name: "Research Team v1.0.0 / Research",
    },
  ],
};

describe("platform Extension response schemas", () => {
  it("parses list, detail, and idempotent import mappings", () => {
    expect(PlatformExtensionListSchema.parse([mapping])).toHaveLength(1);
    expect(
      PlatformExtensionDetailSchema.parse({
        ...mapping,
        manifest: { schema_version: "multica.extension-bundle/v1" },
      }).manifest,
    ).toEqual({ schema_version: "multica.extension-bundle/v1" });
    expect(
      PlatformExtensionImportResultSchema.parse({
        ...mapping,
        idempotent: true,
      }).idempotent,
    ).toBe(true);
  });

  it("parses an imported Squad whose internal Agents are awaiting runtime configuration", () => {
    const unbound = {
      ...mapping,
      runtime: null,
      agents: [{ ...mapping.agents[0], runtime: null }],
    };
    expect(PlatformExtensionListSchema.parse([unbound]).at(0)?.runtime).toBeNull();
  });

  it("preserves Agent prompts and Skill files in the canonical manifest", () => {
    const parsed = PlatformExtensionDetailSchema.parse({
      ...mapping,
      manifest: {
        agents: [{ key: "lead", name: "Lead", prompt: "# Lead\n\nCoordinate." }],
        skills: [{
          key: "evidence",
          name: "Evidence",
          files: [
            { path: "SKILL.md", content: "# Evidence" },
            { path: "assets/logo.bin", content: "AP8=", encoding: "base64" },
          ],
        }],
      },
    });

    expect(parsed.manifest.agents?.[0]?.prompt).toBe("# Lead\n\nCoordinate.");
    expect(parsed.manifest.skills?.[0]?.files?.[1]?.encoding).toBe("base64");
  });

  it("rejects malformed nested mappings", () => {
    expect(
      PlatformExtensionListSchema.safeParse([
        { ...mapping, agents: [{ ...mapping.agents[0], leader: "yes" }] },
      ]).success,
    ).toBe(false);
    expect(
      PlatformExtensionImportResultSchema.safeParse({
        ...mapping,
        idempotent: "false",
      }).success,
    ).toBe(false);
    for (const manifest of [undefined, null, "bundle", 1, []]) {
      const detail = { ...mapping, manifest };
      if (manifest === undefined) delete (detail as { manifest?: unknown }).manifest;
      expect(PlatformExtensionDetailSchema.safeParse(detail).success).toBe(false);
    }
  });
});
