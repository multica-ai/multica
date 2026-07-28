// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enAgents from "../../locales/en/agents.json";
import enCommon from "../../locales/en/common.json";
import { CLOUD_PREVIEW_RUNTIME_ID } from "../../runtimes/components/cloud-preview";
import { ModelDropdown } from "./model-dropdown";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents },
};

describe("ModelDropdown Cloud mock catalog", () => {
  afterEach(() => {
    cleanup();
    document.body.innerHTML = "";
  });

  it("offers mock Cloud models without querying a daemon", () => {
    const onChange = vi.fn();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={queryClient}>
          <ModelDropdown
            runtimeId={CLOUD_PREVIEW_RUNTIME_ID}
            runtimeOnline
            value=""
            onChange={onChange}
          />
        </QueryClientProvider>
      </I18nProvider>,
    );

    fireEvent.click(screen.getByText("Default (provider)"));
    expect(screen.getByText("GPT-5.6 Sol")).toBeInTheDocument();
    expect(screen.getByText("GPT-5.6 Terra")).toBeInTheDocument();
    expect(screen.queryByText("Claude Sonnet 4.6")).toBeNull();

    fireEvent.click(screen.getByText("GPT-5.6 Terra"));
    expect(onChange).toHaveBeenCalledWith("gpt-5.6-terra");
  });
});
