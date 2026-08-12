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
    // Bulk editing is the advertised entry point; pasting into a field stays
    // as an undocumented shortcut for people who reach for it.
    expect(
      screen.getByRole("button", { name: /bulk edit/i }),
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
      "Line 2 isn't a KEY=value assignment",
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
      'Duplicate key "API_KEY" on line 2',
    );
  });
});

describe("EnvTab bulk editing", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getAgentEnv.mockResolvedValue({
      custom_env: { API_KEY: "secret value", BASE_URL: "https://example.com" },
    });
    updateAgentEnv.mockResolvedValue({ custom_env: {} });
  });

  async function revealAndBulkEdit() {
    const user = userEvent.setup();
    renderTab();
    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    await user.click(screen.getByRole("button", { name: /bulk edit/i }));
    return user;
  }

  it("seeds the textarea with the current variables", async () => {
    await revealAndBulkEdit();

    expect(screen.getByRole("textbox", { name: /bulk edit/i })).toHaveValue(
      "API_KEY=secret value\nBASE_URL=https://example.com",
    );
    // The row editor is what the textarea replaces, not something beside it.
    expect(screen.queryAllByPlaceholderText("KEY")).toHaveLength(0);
    expect(
      screen.getByText(/values are shown in plain text/i),
    ).toBeInTheDocument();
  });

  it("saves what the textarea holds", async () => {
    const user = await revealAndBulkEdit();

    const textarea = screen.getByRole("textbox", { name: /bulk edit/i });
    await user.clear(textarea);
    await user.type(textarea, "WIN_DIR=C:\\Users\\me{Enter}PG=p$ssw0rd");
    await user.click(screen.getByRole("button", { name: /^save$/i }));

    expect(updateAgentEnv.mock.calls[0]?.[1]).toEqual({
      custom_env: { WIN_DIR: String.raw`C:\Users\me`, PG: "p$ssw0rd" },
    });
  });

  it("blocks saving while the text does not parse and names the line", async () => {
    const user = await revealAndBulkEdit();

    const textarea = screen.getByRole("textbox", { name: /bulk edit/i });
    await user.clear(textarea);
    await user.type(textarea, "FIRST=one{Enter}echo unsafe");

    expect(
      screen.getByText("Line 2 isn't a KEY=value assignment"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^save$/i })).toBeDisabled();
    // Leaving via the toggle would silently discard the text, so it waits too.
    expect(screen.getByRole("button", { name: /edit as rows/i })).toBeDisabled();

    // Fixing the line releases both.
    await user.clear(textarea);
    await user.type(textarea, "FIRST=one");
    expect(screen.getByRole("button", { name: /^save$/i })).toBeEnabled();
    expect(screen.getByRole("button", { name: /edit as rows/i })).toBeEnabled();
  });

  it("names the duplicated key and its line", async () => {
    const user = await revealAndBulkEdit();

    const textarea = screen.getByRole("textbox", { name: /bulk edit/i });
    await user.clear(textarea);
    await user.type(textarea, "A=1{Enter}A=2");

    expect(
      screen.getByText('Duplicate key "A" on line 2'),
    ).toBeInTheDocument();
  });

  it("carries edits back into the row editor, still masked", async () => {
    const user = await revealAndBulkEdit();

    const textarea = screen.getByRole("textbox", { name: /bulk edit/i });
    await user.clear(textarea);
    await user.type(textarea, "ONLY_KEY=only value");
    await user.click(screen.getByRole("button", { name: /edit as rows/i }));

    expect(
      screen.getAllByPlaceholderText("KEY").map((i) => (i as HTMLInputElement).value),
    ).toEqual(["ONLY_KEY"]);
    expect(
      screen.getAllByPlaceholderText("value").map((i) => (i as HTMLInputElement).type),
    ).toEqual(["password"]);
  });
});

describe("EnvTab value-field paste", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getAgentEnv.mockResolvedValue({ custom_env: {} });
    updateAgentEnv.mockResolvedValue({ custom_env: {} });
  });

  it("leaves the value field alone — a pasted value is a value", async () => {
    const user = userEvent.setup();
    renderTab();

    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    await user.type(screen.getByPlaceholderText("KEY"), "CONFIG");

    // `foo=bar` looks like an assignment but was pasted as a value. Hijacking
    // it would save { foo: "bar" } instead of { CONFIG: "foo=bar" }.
    const valueInput = screen.getByPlaceholderText("value");
    const nativeAllowed = fireEvent.paste(valueInput, {
      clipboardData: { getData: () => "foo=bar" },
    });

    expect(nativeAllowed).toBe(true);
    expect(toastError).not.toHaveBeenCalled();
    expect(
      screen.getAllByPlaceholderText("KEY").map((i) => (i as HTMLInputElement).value),
    ).toEqual(["CONFIG"]);
  });

  it("does not intercept a whole file pasted into the value field either", async () => {
    const user = userEvent.setup();
    renderTab();

    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    const nativeAllowed = fireEvent.paste(screen.getByPlaceholderText("value"), {
      clipboardData: { getData: () => "FIRST=one\nSECOND=two" },
    });

    expect(nativeAllowed).toBe(true);
    expect(screen.getAllByPlaceholderText("KEY")).toHaveLength(1);
  });

  it("reports the offending line when a paste cannot be parsed", async () => {
    const user = userEvent.setup();
    renderTab();

    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    fireEvent.paste(screen.getByPlaceholderText("KEY"), {
      clipboardData: { getData: () => "FIRST=one\necho unsafe" },
    });

    expect(toastError).toHaveBeenCalledWith(
      "Line 2 isn't a KEY=value assignment",
    );
  });
});

describe("EnvTab unsaved-work reporting", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getAgentEnv.mockResolvedValue({ custom_env: { API_KEY: "secret" } });
    updateAgentEnv.mockResolvedValue({ custom_env: {} });
  });

  it("reports bulk text that does not parse as unsaved work", async () => {
    const onDirtyChange = vi.fn();
    const user = userEvent.setup();
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <EnvTab agent={agent} onDirtyChange={onDirtyChange} />
      </I18nProvider>,
    );

    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    await user.click(screen.getByRole("button", { name: /bulk edit/i }));
    onDirtyChange.mockClear();

    const textarea = screen.getByRole("textbox", { name: /bulk edit/i });
    await user.clear(textarea);
    await user.type(textarea, "not-an-assignment");

    // Without this the parent's discard guard sees a clean tab and lets a tab
    // switch drop the text with no prompt.
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);
    expect(screen.getByText(/unsaved changes/i)).toBeInTheDocument();

    // Clearing the error settles back to genuinely clean.
    await user.clear(textarea);
    await user.type(textarea, "API_KEY=secret");
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
  });
});

describe("EnvTab values that bulk text cannot represent", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    updateAgentEnv.mockResolvedValue({ custom_env: {} });
  });

  it("refuses to enter bulk mode rather than serialize lossily", async () => {
    getAgentEnv.mockResolvedValue({
      custom_env: { PEM: "line1\nline2", SAFE: "ok" },
    });
    const user = userEvent.setup();
    renderTab();

    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    await user.click(screen.getByRole("button", { name: /bulk edit/i }));

    expect(toastError).toHaveBeenCalledWith(
      '"PEM" can\'t be shown as text — edit it as a row',
    );
    // Still in the row editor, data untouched.
    expect(
      screen.queryByRole("textbox", { name: /bulk edit/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.getAllByPlaceholderText("KEY").map((i) => (i as HTMLInputElement).value),
    ).toEqual(["PEM", "SAFE"]);
  });

  it("refuses bulk mode when two rows share a key", async () => {
    getAgentEnv.mockResolvedValue({ custom_env: { A: "1" } });
    const user = userEvent.setup();
    renderTab();

    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    await user.click(screen.getByRole("button", { name: /^add$/i }));
    await user.type(screen.getAllByPlaceholderText("KEY")[1]!, "A");
    await user.click(screen.getByRole("button", { name: /bulk edit/i }));

    // No text could round-trip two entries sharing a key, so entering bulk
    // mode would show a duplicate error the user could not trace to a row.
    expect(toastError).toHaveBeenCalledWith("Duplicate environment variable keys");
    expect(
      screen.queryByRole("textbox", { name: /bulk edit/i }),
    ).not.toBeInTheDocument();
  });

  it("keeps a quote-then-hash value intact through a bulk round trip", async () => {
    getAgentEnv.mockResolvedValue({ custom_env: { TRICKY: 'foo" #bar' } });
    const user = userEvent.setup();
    renderTab();

    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    await user.click(screen.getByRole("button", { name: /bulk edit/i }));
    await user.click(screen.getByRole("button", { name: /edit as rows/i }));
    await user.click(screen.getByRole("button", { name: /^add$/i }));
    await user.type(screen.getAllByPlaceholderText("KEY")[1]!, "NEW");
    await user.click(screen.getByRole("button", { name: /^save$/i }));

    expect(updateAgentEnv.mock.calls[0]?.[1]).toEqual({
      custom_env: { TRICKY: 'foo" #bar', NEW: "" },
    });
  });
});
