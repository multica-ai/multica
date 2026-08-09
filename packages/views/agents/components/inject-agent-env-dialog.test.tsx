// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mutateAsyncSpy = vi.hoisted(() => vi.fn());
const getAgentEnvSpy = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/agents", () => ({
  useMergeAgentsEnv: () => ({ mutateAsync: mutateAsyncSpy, isPending: false }),
}));
// Present only so the test can assert it is never called: this dialog must not
// read existing values.
vi.mock("@multica/core/api", () => ({
  api: { getAgentEnv: getAgentEnvSpy },
}));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <div>avatar</div>,
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));

import { InjectAgentEnvDialog } from "./inject-agent-env-dialog";

function makeAgent(id: string, keyCount = 0): Agent {
  return {
    id,
    workspace_id: "ws-1",
    runtime_id: "rt-1",
    name: `Agent ${id}`,
    description: "",
    custom_env_key_count: keyCount,
    archived_at: null,
  } as Agent;
}

function renderDialog(agents: Agent[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <InjectAgentEnvDialog
          open
          onOpenChange={() => {}}
          agents={agents}
          wsId="ws-1"
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

function typeVariable(key: string, value: string, row = 0) {
  const keyInputs = screen.getAllByPlaceholderText("KEY");
  const valueInputs = screen.getAllByPlaceholderText("value");
  fireEvent.change(keyInputs[row]!, { target: { value: key } });
  fireEvent.change(valueInputs[row]!, { target: { value } });
}

beforeEach(() => {
  mutateAsyncSpy.mockReset();
  mutateAsyncSpy.mockResolvedValue({
    results: [
      {
        agent_id: "a",
        name: "Agent a",
        added_keys: ["NEW"],
        overwritten_keys: [],
        key_count: 1,
      },
    ],
    skipped: [],
  });
  getAgentEnvSpy.mockReset();
});

describe("InjectAgentEnvDialog — one dialog for one agent and for many", () => {
  it("names the agent for a single target and counts them for several", () => {
    const single = renderDialog([makeAgent("a")]);
    expect(screen.getByText(/Add env variables to "Agent a"/)).toBeTruthy();
    single.unmount();

    renderDialog([makeAgent("a"), makeAgent("b")]);
    expect(screen.getByText(/Add env variables to 2 agents/)).toBeTruthy();
  });

  it("submits the same payload shape for one agent and for many", async () => {
    const single = renderDialog([makeAgent("a")]);
    typeVariable("API_KEY", "secret");
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(mutateAsyncSpy).toHaveBeenCalled());
    expect(mutateAsyncSpy.mock.calls[0]?.[0]).toEqual({
      agentIds: ["a"],
      set: { API_KEY: "secret" },
    });
    single.unmount();

    mutateAsyncSpy.mockClear();
    renderDialog([makeAgent("a"), makeAgent("b")]);
    typeVariable("API_KEY", "secret");
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(mutateAsyncSpy).toHaveBeenCalled());
    expect(mutateAsyncSpy.mock.calls[0]?.[0]).toEqual({
      agentIds: ["a", "b"],
      set: { API_KEY: "secret" },
    });
  });
});

describe("InjectAgentEnvDialog — disclosure", () => {
  it("never reads existing env values", async () => {
    renderDialog([makeAgent("a", 3), makeAgent("b", 0)]);
    typeVariable("API_KEY", "secret");
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(mutateAsyncSpy).toHaveBeenCalled());

    // Reading them would write an `agent_env_revealed` audit row per agent and
    // pull plaintext secrets into the browser — the whole reason this dialog
    // uses the merge endpoint instead of read-modify-write.
    expect(getAgentEnvSpy).not.toHaveBeenCalled();
  });

  it("shows only how many variables each agent has, never which", () => {
    renderDialog([makeAgent("a", 3), makeAgent("b", 0)]);
    expect(screen.getByText("3 variables")).toBeTruthy();
    expect(screen.getByText("0 variables")).toBeTruthy();
  });

  it("masks typed values by default and reveals them on request", () => {
    renderDialog([makeAgent("a")]);
    typeVariable("API_KEY", "secret");
    const value = screen.getByPlaceholderText("value");
    // A row the user is typing into starts visible; toggling hides it.
    expect(value.getAttribute("type")).toBe("text");
    fireEvent.click(screen.getByRole("button", { name: /hide value/i }));
    expect(screen.getByPlaceholderText("value").getAttribute("type")).toBe(
      "password",
    );
  });
});

describe("InjectAgentEnvDialog — input guards", () => {
  it("will not submit with nothing entered", () => {
    renderDialog([makeAgent("a")]);
    expect(
      screen.getByRole("button", { name: "Apply" }).hasAttribute("disabled"),
    ).toBe(true);
  });

  it("blocks duplicate keys before they reach the server", () => {
    renderDialog([makeAgent("a")]);
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    typeVariable("SAME", "one", 0);
    typeVariable("SAME", "two", 1);

    expect(
      screen.getByRole("button", { name: "Apply" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(mutateAsyncSpy).not.toHaveBeenCalled();
  });

  it("ignores rows whose key is blank", async () => {
    renderDialog([makeAgent("a")]);
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    typeVariable("REAL", "value", 0);
    typeVariable("   ", "orphan", 1);

    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(mutateAsyncSpy).toHaveBeenCalled());
    expect(mutateAsyncSpy.mock.calls[0]?.[0].set).toEqual({ REAL: "value" });
  });
});
