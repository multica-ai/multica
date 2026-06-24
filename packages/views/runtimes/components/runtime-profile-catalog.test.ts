import { describe, expect, it } from "vitest";
import type { RuntimeProfile } from "@multica/core/types";
import {
  buildRuntimeCatalog,
  formatCommandLine,
  parseCommandLine,
  PROTOCOL_FAMILIES,
} from "./runtime-profile-catalog";

function profile(
  id: string,
  displayName: string,
  updatedAt: string,
  enabled = true,
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

describe("parseCommandLine", () => {
  it("splits a pasted executable and fixed args", () => {
    expect(parseCommandLine("agent --model composer-2.5")).toEqual({
      ok: true,
      commandName: "agent",
      fixedArgs: ["--model", "composer-2.5"],
    });
  });

  it("preserves quoted whitespace and escaped characters", () => {
    expect(parseCommandLine(`agent --flag "a b c" path\\ with\\ spaces`)).toEqual({
      ok: true,
      commandName: "agent",
      fixedArgs: ["--flag", "a b c", "path with spaces"],
    });
  });

  it("rejects shell control syntax", () => {
    expect(parseCommandLine("agent && rm -rf /")).toEqual({
      ok: false,
      error: "shell_syntax",
    });
    expect(parseCommandLine("agent | tee out")).toEqual({
      ok: false,
      error: "shell_syntax",
    });
  });

  it("rejects shell expansion syntax", () => {
    expect(parseCommandLine("agent --path $HOME/bin")).toEqual({
      ok: false,
      error: "shell_expansion",
    });
    expect(parseCommandLine("agent --path $(which foo)")).toEqual({
      ok: false,
      error: "shell_expansion",
    });
  });

  it("allows literal shell-looking characters inside single quotes", () => {
    expect(parseCommandLine("agent --note '$5 reward `literal`'")).toEqual({
      ok: true,
      commandName: "agent",
      fixedArgs: ["--note", "$5 reward `literal`"],
    });
  });

  it("rejects unclosed quotes", () => {
    expect(parseCommandLine(`agent --flag "unterminated`)).toEqual({
      ok: false,
      error: "unclosed_quote",
    });
  });

  it("rejects a trailing escape", () => {
    expect(parseCommandLine("agent \\")).toEqual({
      ok: false,
      error: "trailing_escape",
    });
  });
});

describe("formatCommandLine", () => {
  it("quotes args that need shell escaping for display", () => {
    expect(formatCommandLine("agent", ["--flag", "a b c"])).toBe(
      'agent --flag "a b c"',
    );
  });
});
