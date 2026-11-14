import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import { RuntimeConfigTab } from "./runtime-config-tab";

const resources = { en: { common: enCommon, agents: enAgents } };
describe("OpenClaw runtime edit round trip", () => {
 it.each(["isolated-local", "local", "gateway", "future-mode"])("preserves %s and unknown settings on model-only edit", async (mode) => {
  const runtime_config = { mode, model_override: "old-model", future_key: { retained: true }, gateway: { token: "***", host: "fixture.invalid", future_pin: 42 } };
  const onSave = vi.fn().mockResolvedValue(undefined);
  const agent = { id: "fixture", runtime_config } as unknown as Agent;
  const view = (a: Agent) => <I18nProvider locale="en" resources={resources}><RuntimeConfigTab agent={a} onSave={onSave} /></I18nProvider>;
  const { rerender } = render(view(agent));
  fireEvent.change(document.getElementById("openclaw-model-override")!, { target: { value: "new-model" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(onSave).toHaveBeenCalledOnce());
  expect(onSave.mock.calls[0]![0]).toEqual({ runtime_config: { ...runtime_config, model_override: "new-model" } });
  rerender(view({ ...agent, runtime_config: onSave.mock.calls[0]![0].runtime_config }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).toBeDisabled());
 });
});
