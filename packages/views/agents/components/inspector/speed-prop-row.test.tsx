// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { runtimeModelsKeys } from "@multica/core/runtimes";
import { SpeedPropRow } from "./speed-prop-row";

it("renders and changes an advertised speed mode", () => {
  const client = new QueryClient();
  client.setQueryData(runtimeModelsKeys.forRuntime("rt"), {
    supported: true,
    models: [{ id: "gpt-5.6-sol", label: "GPT-5.6 Sol", default: true,
      speed: { supported_levels: [{ value: "standard", label: "Standard" }, { value: "fast", label: "Fast" }] } }],
  });
  const onChange = vi.fn();
  render(<QueryClientProvider client={client}><SpeedPropRow runtimeId="rt" runtimeOnline model="gpt-5.6-sol" value="standard" canEdit onChange={onChange} /></QueryClientProvider>);
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "fast" } });
  expect(onChange).toHaveBeenCalledWith("fast");
});
