import { describe, expect, it } from "vitest";
import type { RuntimeProfile } from "@multica/core/types";
import {
  buildRuntimeCatalog,
  formatRuntimeProfileCommand,
  PROTOCOL_FAMILIES,
} from "./runtime-profile-catalog";

function profile(
  id: string,
  displayName: string,
  updatedAt: string,
  enabled = true,
  overrides: Partial<RuntimeProfile> = {},
): RuntimeProfile {
  return {
    id,
    workspace_id: "ws-1",
    display_name: displayName,
    protocol_family: "codex",
    command_name: "codex",
    description: null,
    fixed_args: [],
    visibility: "workspace",
    created_by: "user-1",
    enabled,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: updatedAt,
    ...overrides,
  };
}

describe("buildRuntimeCatalog", () => {
  it("keeps custom profiles separate from built-in protocol families", () => {
    const catalog = buildRuntimeCatalog([
      profile("prof-1", "Team Codex", "2026-01-02T00:00:00Z"),
    ]);

    expect(catalog.customs).toHaveLength(1);
    expect(catalog.customs[0]).toMatchObject({
      kind: "custom",
      id: "prof-1",
      protocolFamily: "codex",
    });
    expect(catalog.builtins).toHaveLength(PROTOCOL_FAMILIES.length);
    expect(catalog.builtins[0]).toMatchObject({
      kind: "builtin",
      id: `builtin:${PROTOCOL_FAMILIES[0]}`,
      protocolFamily: PROTOCOL_FAMILIES[0],
    });
  });

  it("sorts custom profiles by enabled state, recency, then display name", () => {
    const catalog = buildRuntimeCatalog([
      profile("disabled-new", "Disabled New", "2026-01-04T00:00:00Z", false),
      profile("enabled-old", "Alpha", "2026-01-01T00:00:00Z"),
      profile("enabled-new-a", "Zulu", "2026-01-03T00:00:00Z"),
      profile("enabled-new-b", "Bravo", "2026-01-03T00:00:00Z"),
    ]);

    expect(catalog.customs.map((entry) => entry.id)).toEqual([
      "enabled-new-b",
      "enabled-new-a",
      "enabled-old",
      "disabled-new",
    ]);
  });
});

describe("formatRuntimeProfileCommand", () => {
  it("renders command_name together with fixed_args for edit/display surfaces", () => {
    expect(
      formatRuntimeProfileCommand(
        profile("prof-args", "Args", "2026-01-02T00:00:00Z", true, {
          command_name: "agent",
          fixed_args: ["--model", "composer-2.5", "--label", "two words"],
        }),
      ),
    ).toBe("agent --model composer-2.5 --label 'two words'");
  });
});
