import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentRuntime } from "@multica/core/types";
import enAgents from "../../locales/en/agents.json";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { MikaRuntimeChoice } from "./mika-runtime-choice";

vi.mock("@multica/core/api", () => ({ api: { listRuntimeModels: vi.fn() } }));

const RESOURCES = {
  en: { agents: enAgents, common: enCommon, runtimes: enRuntimes },
};

const runtimes = [
  { id: "rt-1", name: "MacBook", provider: "claude", status: "online" },
  { id: "rt-2", name: "Server", provider: "codex", status: "offline" },
] as AgentRuntime[];

function renderChoice(props: Partial<Parameters<typeof MikaRuntimeChoice>[0]>) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <I18nProvider locale="en" resources={RESOURCES}>
        <MikaRuntimeChoice
          runtimes={runtimes}
          value={{ runtimeId: "rt-1", model: "" }}
          onChange={() => {}}
          layout="list"
          {...props}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("MikaRuntimeChoice list layout", () => {
  it("selects a runtime and clears the model chosen for the previous one", async () => {
    const onChange = vi.fn();
    renderChoice({ value: { runtimeId: "rt-1", model: "opus" }, onChange });

    await userEvent.click(screen.getByRole("button", { name: /Server/ }));

    // Models are per-runtime, so carrying one across would send a model the
    // new runtime may not have.
    expect(onChange).toHaveBeenCalledWith({ runtimeId: "rt-2", model: "" });
  });

  // The dropdown layout honoured `disabled`; the list layout dropped it, so a
  // member could switch machines while the connect request was in flight and
  // end up with Mika on a runtime they no longer had selected.
  it("stops the choice changing while a submit is in flight", async () => {
    const onChange = vi.fn();
    renderChoice({ disabled: true, onChange });

    const row = screen.getByRole("button", { name: /Server/ });
    expect(row).toBeDisabled();

    await userEvent.click(row);
    expect(onChange).not.toHaveBeenCalled();
  });
});
