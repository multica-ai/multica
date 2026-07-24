// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { ApiError } from "@multica/core/api";
import { configStore } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const composioErrorRef = vi.hoisted(() => ({
  current: null as Error | null,
}));
const queryCallsRef = vi.hoisted(() => ({
  current: [] as { queryKey: unknown[]; enabled?: boolean }[],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    queryCallsRef.current.push(opts);
    return {
      data: undefined,
      error: opts.enabled === false ? null : composioErrorRef.current,
      isError: opts.enabled !== false && composioErrorRef.current != null,
    };
  },
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/composio", () => ({
  composioToolkitsOptions: () => ({ queryKey: ["composio", "toolkits"] }),
}));

vi.mock("./lark-tab", () => ({
  LarkTab: () => <div data-testid="lark-tab" />,
}));

vi.mock("./composio-tab", () => ({
  ComposioTab: () => <div data-testid="composio-tab" />,
}));

vi.mock("./slack-tab", () => ({
  SlackTab: () => <div data-testid="slack-tab" />,
}));

vi.mock("./wechat-tab", () => ({
  WechatTab: () => <div data-testid="wechat-tab" />,
}));

import { IntegrationsTab } from "./integrations-tab";

function renderTab() {
  return render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
      <IntegrationsTab />
    </I18nProvider>,
  );
}

describe("Settings IntegrationsTab", () => {
  beforeEach(() => {
    queryCallsRef.current = [];
    composioErrorRef.current = null;
    configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: true });
  });

  it("hides Composio and disables the toolkits query when the feature flag is off", () => {
    configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: false });

    renderTab();

    expect(screen.queryByTestId("composio-tab")).toBeNull();
    expect(queryCallsRef.current).toHaveLength(1);
    expect(queryCallsRef.current[0]?.enabled).toBe(false);
  });

  it("shows Composio when the feature flag is on and the integration is configured", () => {
    renderTab();

    expect(screen.getByTestId("composio-tab")).toBeInTheDocument();
    expect(queryCallsRef.current[0]?.enabled).toBe(true);
  });

  it("hides Composio when the feature flag is on but the server reports 503", () => {
    composioErrorRef.current = new ApiError("unavailable", 503, "Service Unavailable");

    renderTab();

    expect(screen.queryByTestId("composio-tab")).toBeNull();
  });
});
