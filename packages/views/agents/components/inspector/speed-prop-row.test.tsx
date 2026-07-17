// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { runtimeModelsKeys } from "@multica/core/runtimes";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import enIssues from "../../../locales/en/issues.json";
import { SpeedPropRow } from "./speed-prop-row";

it("renders and changes an advertised speed mode", () => {
  const client = new QueryClient();
  client.setQueryData(runtimeModelsKeys.forRuntime("rt"), {
    supported: true,
    models: [{ id: "gpt-5.6-sol", label: "GPT-5.6 Sol", default: true,
      speed: { supported_levels: [{ value: "standard", label: "Standard" }, { value: "fast", label: "Fast" }] } }],
  });
  const onChange = vi.fn();
  render(<I18nProvider locale="en" resources={{ en: { common: enCommon, agents: enAgents, issues: enIssues } }}><QueryClientProvider client={client}><SpeedPropRow runtimeId="rt" runtimeOnline model="gpt-5.6-sol" value="standard" canEdit onChange={onChange} /></QueryClientProvider></I18nProvider>);
  fireEvent.click(screen.getByRole("button", { name: /Speed: Standard/i }));
  fireEvent.change(screen.getByRole("textbox", { name: /Search speed/i }), { target: { value: "fast" } });
  fireEvent.click(screen.getByRole("button", { name: "Fast" }));
  expect(onChange).toHaveBeenCalledWith("fast");
});
