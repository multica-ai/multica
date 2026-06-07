// @vitest-environment jsdom

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { McpConnector } from "@multica/core/types";

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

import {
  ConnectorConfigForm,
  substituteTemplate,
  deepMerge,
  mergeConnectorIntoConfig,
} from "./connector-config-form";

function makeConnector(over: Partial<McpConnector> = {}): McpConnector {
  return {
    id: "c-1",
    workspace_id: null,
    slug: "github",
    name: "GitHub",
    icon: null,
    description: "Manage issues and PRs",
    popularity: 100,
    input_schema: {
      fields: [
        { key: "TOKEN", label: "API token", type: "password", required: true },
      ],
    },
    mcp_template: {
      mcpServers: {
        github: {
          command: "npx",
          args: ["-y", "@github/mcp"],
          env: { GITHUB_TOKEN: "{{TOKEN}}" },
        },
      },
    },
    is_custom: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  };
}

describe("substituteTemplate", () => {
  it("replaces placeholders inside string leaves, including substrings", () => {
    const out = substituteTemplate(
      { a: "Bearer {{TOKEN}}", b: ["{{X}}", 1, true] },
      { TOKEN: "abc", X: "y" },
    );
    expect(out).toEqual({ a: "Bearer abc", b: ["y", 1, true] });
  });

  it("leaves unknown placeholders untouched", () => {
    expect(substituteTemplate("{{MISSING}}", {})).toBe("{{MISSING}}");
  });
});

describe("deepMerge", () => {
  it("merges nested objects without dropping sibling keys", () => {
    const base = { mcpServers: { a: { command: "x" } } };
    const next = { mcpServers: { b: { command: "y" } } };
    expect(deepMerge(base, next)).toEqual({
      mcpServers: { a: { command: "x" }, b: { command: "y" } },
    });
  });
});

describe("mergeConnectorIntoConfig", () => {
  it("normalises null/string current config to an object", () => {
    expect(mergeConnectorIntoConfig(null, { mcpServers: { a: {} } })).toEqual({
      mcpServers: { a: {} },
    });
    expect(
      mergeConnectorIntoConfig("not json", { mcpServers: { a: {} } }),
    ).toEqual({ mcpServers: { a: {} } });
  });

  it("preserves pre-existing mcpServers entries", () => {
    const current = { mcpServers: { existing: { command: "keep" } } };
    const resolved = { mcpServers: { added: { command: "new" } } };
    expect(mergeConnectorIntoConfig(current, resolved)).toEqual({
      mcpServers: {
        existing: { command: "keep" },
        added: { command: "new" },
      },
    });
  });
});

function renderForm(
  overrides: {
    connector?: McpConnector;
    currentConfig?: unknown;
    onSave?: ReturnType<typeof vi.fn>;
  } = {},
) {
  const onSave = overrides.onSave ?? vi.fn().mockResolvedValue(undefined);
  const currentConfig =
    "currentConfig" in overrides
      ? overrides.currentConfig
      : { mcpServers: { existing: { command: "keep" } } };
  render(
    <ConnectorConfigForm
      connector={overrides.connector ?? makeConnector()}
      currentConfig={currentConfig}
      open
      onOpenChange={() => {}}
      onSave={onSave as (u: { mcp_config: unknown }) => Promise<void>}
    />,
  );
  return { onSave };
}

describe("ConnectorConfigForm", () => {
  it("renders one control per input_schema field", () => {
    renderForm();
    expect(screen.getByLabelText(/API token/)).toBeInTheDocument();
  });

  it("blocks submit until required fields are filled", async () => {
    const user = userEvent.setup();
    const { onSave } = renderForm();
    const submit = screen.getByRole("button", { name: "Add connector" });
    expect(submit).toBeDisabled();
    await user.type(screen.getByLabelText(/API token/), "secret-token");
    expect(submit).toBeEnabled();
    void onSave;
  });

  it("substitutes values and merges into the agent config, preserving existing servers", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn().mockResolvedValue(undefined);
    renderForm({ onSave });

    await user.type(screen.getByLabelText(/API token/), "secret-token");
    await user.click(screen.getByRole("button", { name: "Add connector" }));

    expect(onSave).toHaveBeenCalledTimes(1);
    const arg = onSave.mock.calls[0]?.[0] as { mcp_config: unknown };
    expect(arg.mcp_config).toEqual({
      mcpServers: {
        existing: { command: "keep" },
        github: {
          command: "npx",
          args: ["-y", "@github/mcp"],
          env: { GITHUB_TOKEN: "secret-token" },
        },
      },
    });
  });

  it("allows adding a connector with no fields directly", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn().mockResolvedValue(undefined);
    const connector = makeConnector({
      input_schema: { fields: [] },
      mcp_template: { mcpServers: { simple: { command: "run" } } },
    });
    renderForm({ onSave, connector, currentConfig: null });

    await user.click(screen.getByRole("button", { name: "Add connector" }));

    expect(onSave).toHaveBeenCalledWith({
      mcp_config: { mcpServers: { simple: { command: "run" } } },
    });
  });
});
