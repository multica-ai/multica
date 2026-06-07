import type { ReactNode } from "react";
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent, AgentRuntime } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";
import { ProviderModelCell, type AgentRow } from "./agent-columns";

const RES = { en: { common: enCommon, agents: enAgents } };
function Wrap({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={RES}>
      {children}
    </I18nProvider>
  );
}

function row(agent: Partial<Agent>, runtime: Partial<AgentRuntime> | null): AgentRow {
  return {
    agent: { id: "a1", name: "A", archived_at: null, runtime_config: {}, model: "", ...agent } as Agent,
    runtime: (runtime as AgentRuntime) ?? null,
  } as AgentRow;
}

describe("ProviderModelCell", () => {
  it("shows the runtime provider with the agent model", () => {
    render(
      <ProviderModelCell row={row({ model: "gpt-5.5" }, { provider: "codex" })} />,
      { wrapper: Wrap },
    );
    expect(screen.getByText("codex · gpt-5.5")).toBeInTheDocument();
  });

  it("shows the runtime provider when there is no model", () => {
    render(<ProviderModelCell row={row({ runtime_config: {}, model: "" }, { provider: "claude" })} />, { wrapper: Wrap });
    expect(screen.getAllByText("claude").length).toBeGreaterThan(0);
  });

  it("renders a dash for an archived agent", () => {
    render(<ProviderModelCell row={row({ archived_at: "2026-01-01T00:00:00Z" }, { provider: "claude" })} />, { wrapper: Wrap });
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByText("claude")).toBeNull();
  });

  it("renders a dash when no runtime provider exists", () => {
    render(<ProviderModelCell row={row({ model: "gpt-5.5" }, null)} />, { wrapper: Wrap });
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
    expect(screen.queryByText("— · gpt-5.5")).toBeNull();
  });
});
