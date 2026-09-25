// @vitest-environment jsdom

import type { ComponentProps, ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import type { ManagedMcpServer } from "./mcp-config-model";
import { McpServerDialog } from "./mcp-server-dialog";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function managedServer(overrides: Partial<ManagedMcpServer> = {}): ManagedMcpServer {
  return {
    name: "fetch",
    config: { command: "uvx" },
    container: "mcpServers",
    transport: "stdio",
    enabled: true,
    ...overrides,
  };
}

function renderDialog(overrides: Partial<ComponentProps<typeof McpServerDialog>> = {}) {
  const onSave = vi.fn().mockResolvedValue(undefined);
  const onOpenChange = vi.fn();
  const props = {
    open: true,
    server: null,
    existingNames: new Set<string>(),
    onSave,
    onOpenChange,
    ...overrides,
  };
  const view = render(<McpServerDialog {...props} />, { wrapper: Wrapper });
  return { ...view, onSave: props.onSave, onOpenChange: props.onOpenChange };
}

describe("McpServerDialog", () => {
  it.each(["context7.dev", "legacy server", " padded "])(
    "edits configuration while preserving the historical name %s exactly",
    async (name) => {
      const user = userEvent.setup();
      const { onSave, onOpenChange } = renderDialog({
        server: managedServer({ name }),
        existingNames: new Set([name]),
        hideNameWhenEditing: true,
      });

      expect(screen.queryByLabelText("Server name")).toBeNull();
      await user.clear(screen.getByLabelText("Command"));
      await user.type(screen.getByLabelText("Command"), "updated-command");
      await user.click(screen.getByRole("button", { name: "Save" }));

      await waitFor(() =>
        expect(onSave).toHaveBeenCalledWith(name, { command: "updated-command" }),
      );
      expect(onOpenChange).toHaveBeenCalledWith(false);
    },
  );

  it.each(["create", "rename"])(
    "rejects a newly entered historical-style name during %s",
    async (action) => {
      const user = userEvent.setup();
      const { onSave, onOpenChange } = renderDialog({
        server: action === "rename" ? managedServer() : null,
      });

      const input = screen.getByLabelText("Server name");
      await user.clear(input);
      await user.type(input, "context7.dev");
      if (action === "create") {
        await user.type(screen.getByLabelText("Command"), "uvx");
      }
      await user.click(screen.getByRole("button", {
        name: action === "create" ? "Add" : "Save",
      }));

      expect(input).toHaveFocus();
      expect(input).toHaveAttribute("aria-invalid", "true");
      expect(screen.getByRole("alert")).toHaveTextContent(
        "Use only letters, numbers, hyphens, and underscores.",
      );
      expect(onSave).not.toHaveBeenCalled();
      expect(onOpenChange).not.toHaveBeenCalled();
    },
  );

  it.each([
    ["sse", "STDIO", "Command", "uvx", { command: "uvx" }],
    ["websocket", "Streamable HTTP", "Server URL", "https://new.example/mcp", {
      type: "http", url: "https://new.example/mcp",
    }],
  ])(
    "replaces a %s server through the visual editor using %s",
    async (transport, selected, label, value, expected) => {
      const user = userEvent.setup();
      const { onSave } = renderDialog({
        server: managedServer({ transport, config: {} }),
        replacementMode: true,
      });

      expect(screen.getByRole("tab", { name: "JSON" })).toHaveAttribute(
        "aria-selected", "true",
      );
      await user.click(screen.getByRole("tab", { name: "Visual editor" }));
      await user.click(screen.getByRole("button", { name: new RegExp(`^${selected}`) }));
      await user.type(screen.getByLabelText(label), value);
      await user.click(screen.getByRole("button", { name: "Replace configuration" }));

      expect(onSave).toHaveBeenCalledWith("fetch", expected);
    },
  );

  it("blocks duplicate submits and dismissal until a save completes", async () => {
    const user = userEvent.setup();
    let finishSave!: () => void;
    const onSave = vi.fn(() => new Promise<void>((resolve) => { finishSave = resolve; }));
    const { onOpenChange } = renderDialog({ server: managedServer(), onSave });

    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    fireEvent.submit(screen.getByLabelText("Command").closest("form")!);
    await user.keyboard("{Escape}");
    await user.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(onOpenChange).not.toHaveBeenCalled();
    expect(onSave).toHaveBeenCalledExactlyOnceWith("fetch", { command: "uvx" });

    finishSave();
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it.each(["Cancel", "Close", "Escape"])(
    "discards an unsaved draft when dismissed with %s",
    async (action) => {
      const user = userEvent.setup();
      const { onSave, onOpenChange } = renderDialog();
      await user.type(screen.getByLabelText("Server name"), "draft");

      if (action === "Escape") await user.keyboard("{Escape}");
      else await user.click(screen.getByRole("button", { name: action }));

      expect(onOpenChange).toHaveBeenCalledWith(false);
      expect(onSave).not.toHaveBeenCalled();
    },
  );

  it("clears submitted JSON errors when returning to the visual editor", async () => {
    const user = userEvent.setup();
    const { onSave } = renderDialog();
    await user.type(screen.getByLabelText("Server name"), "fetch");
    await user.type(screen.getByLabelText("Command"), "uvx");
    await user.click(screen.getByRole("tab", { name: "JSON" }));
    fireEvent.change(screen.getByLabelText("MCP server JSON configuration"), {
      target: { value: "{invalid" },
    });
    await user.click(screen.getByRole("button", { name: "Add" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Invalid JSON");

    await user.click(screen.getByRole("tab", { name: "Visual editor" }));

    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByLabelText("Command")).toHaveValue("uvx");
    expect(screen.getByLabelText("Command")).not.toHaveAttribute("aria-invalid");
    await user.click(screen.getByRole("button", { name: "Add" }));
    expect(onSave).toHaveBeenCalledWith("fetch", { command: "uvx" });
  });

  it("removes one argument and environment row while preserving their siblings", async () => {
    const user = userEvent.setup();
    const { onSave } = renderDialog({
      server: managedServer({
        config: {
          command: "uvx",
          args: ["first", "remove", "last"],
          env: { FIRST: "one", REMOVE: "two", LAST: "three" },
        },
      }),
    });

    await user.click(screen.getByRole("button", { name: "Remove argument 2" }));
    await user.clear(screen.getByLabelText("Startup arguments 2"));
    await user.type(screen.getByLabelText("Startup arguments 2"), "updated-last");
    await user.click(screen.getByRole("button", { name: "Remove environment variable 2" }));
    await user.clear(screen.getByLabelText("Environment variables: Variable name 2"));
    await user.type(screen.getByLabelText("Environment variables: Variable name 2"), "UPDATED");
    await user.clear(screen.getByLabelText("Environment variables: Value 2"));
    await user.type(screen.getByLabelText("Environment variables: Value 2"), "four");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSave).toHaveBeenCalledWith("fetch", {
      command: "uvx",
      args: ["first", "updated-last"],
      env: { FIRST: "one", UPDATED: "four" },
    });
  });

  it("removes one HTTP header while preserving and editing its siblings", async () => {
    const user = userEvent.setup();
    const { onSave } = renderDialog({
      server: managedServer({
        transport: "http",
        config: {
          type: "http",
          url: "https://example.test/mcp",
          headers: { First: "one", Remove: "two", Last: "three" },
        },
      }),
    });

    await user.click(screen.getByRole("button", { name: "Remove header 2" }));
    await user.clear(screen.getByLabelText("HTTP headers: Header name 2"));
    await user.type(screen.getByLabelText("HTTP headers: Header name 2"), "Updated");
    await user.clear(screen.getByLabelText("HTTP headers: Value 2"));
    await user.type(screen.getByLabelText("HTTP headers: Value 2"), "four");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSave).toHaveBeenCalledWith("fetch", {
      type: "http",
      url: "https://example.test/mcp",
      headers: { First: "one", Updated: "four" },
    });
  });
});
