// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const getAgentEnv = vi.hoisted(() => vi.fn());
const updateAgentEnv = vi.hoisted(() => vi.fn());
const toastError = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { getAgentEnv, updateAgentEnv },
}));

vi.mock("sonner", () => ({
  toast: { error: toastError, success: vi.fn() },
}));

import { EnvTab } from "./env-tab";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const agent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-08-08T00:00:00Z",
  updated_at: "2026-08-08T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderTab() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <EnvTab agent={agent} />
    </I18nProvider>,
  );
}

describe("EnvTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getAgentEnv.mockResolvedValue({ custom_env: {} });
    updateAgentEnv.mockResolvedValue({ custom_env: {} });
  });

  it("turns pasted environment-file assignments into separate rows", async () => {
    const user = userEvent.setup();
    renderTab();

    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    expect(
      screen.getByText(/paste KEY=value lines into a key field/i),
    ).toBeInTheDocument();
    const keyInput = screen.getByPlaceholderText("KEY");
    keyInput.focus();
    fireEvent.paste(keyInput, {
      clipboardData: {
        getData: () =>
          'API_KEY="secret value"\nexport BASE_URL=https://example.com/api',
      },
    });

    expect(
      screen
        .getAllByPlaceholderText("KEY")
        .map((input) => (input as HTMLInputElement).value),
    ).toEqual(["API_KEY", "BASE_URL"]);
    expect(
      screen
        .getAllByPlaceholderText("value")
        .map((input) => (input as HTMLInputElement).value),
    ).toEqual(["secret value", "https://example.com/api"]);
    expect(
      screen
        .getAllByPlaceholderText("value")
        .every((input) => (input as HTMLInputElement).type === "password"),
    ).toBe(true);
    expect(screen.getAllByPlaceholderText("KEY")[0]).toHaveFocus();
  });

  it("leaves a bare key with a trailing newline to native paste handling", async () => {
    const user = userEvent.setup();
    renderTab();

    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    const keyInput = screen.getByPlaceholderText("KEY");
    const pasteAllowed = fireEvent.paste(keyInput, {
      clipboardData: { getData: () => "API_KEY\n" },
    });

    expect(pasteAllowed).toBe(true);
    expect(toastError).not.toHaveBeenCalled();
  });

  it("leaves a comment-only paste to native handling without an error", async () => {
    const user = userEvent.setup();
    renderTab();

    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    const pasteAllowed = fireEvent.paste(screen.getByPlaceholderText("KEY"), {
      clipboardData: { getData: () => "# database settings\n" },
    });

    expect(pasteAllowed).toBe(true);
    expect(toastError).not.toHaveBeenCalled();
  });

  it("preserves special environment keys in the save payload", async () => {
    const user = userEvent.setup();
    renderTab();

    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    fireEvent.paste(screen.getByPlaceholderText("KEY"), {
      clipboardData: { getData: () => "__proto__=secret" },
    });
    await user.click(screen.getByRole("button", { name: /^save$/i }));

    const payload = updateAgentEnv.mock.calls[0]?.[1] as {
      custom_env: Record<string, string>;
    };
    expect(Object.hasOwn(payload.custom_env, "__proto__")).toBe(true);
    expect(payload.custom_env.__proto__).toBe("secret");
  });

  it("rejects malformed environment-file pastes without changing the row", async () => {
    const user = userEvent.setup();
    renderTab();

    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    const keyInput = screen.getByPlaceholderText("KEY");
    const pasteAllowed = fireEvent.paste(keyInput, {
      clipboardData: { getData: () => "FIRST=one\necho unsafe" },
    });

    expect(pasteAllowed).toBe(false);
    expect(keyInput).toHaveValue("");
    expect(toastError).toHaveBeenCalledWith(
      "Couldn't parse pasted environment variables",
    );
  });

  it("rejects duplicate keys during paste", async () => {
    const user = userEvent.setup();
    renderTab();

    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    const pasteAllowed = fireEvent.paste(screen.getByPlaceholderText("KEY"), {
      clipboardData: { getData: () => "API_KEY=first\nAPI_KEY=second" },
    });

    expect(pasteAllowed).toBe(false);
    expect(toastError).toHaveBeenCalledWith(
      "Duplicate environment variable keys",
    );
  });
});
