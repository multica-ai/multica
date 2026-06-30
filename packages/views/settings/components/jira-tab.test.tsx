import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import enSettings from "../../locales/en/settings.json";

const TEST_RESOURCES = { en: { settings: enSettings } };

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const mockSyncNow = vi.hoisted(() => vi.fn());

vi.mock("../jira/use-jira-sync", () => ({
  useJiraSync: () => ({
    syncNow: mockSyncNow,
    running: false,
    lastResult: { created: 3, updated: 1, skipped: 0, commentsAdded: 2, errors: [] },
    error: null,
  }),
  getJiraBridge: () =>
    (globalThis as unknown as { window: { jiraAPI?: unknown } }).window.jiraAPI,
}));

import { JiraTab } from "./jira-tab";

function installJiraAPI() {
  (globalThis as unknown as { window: { jiraAPI: unknown } }).window.jiraAPI = {
    request: vi.fn(),
    getConfig: vi.fn().mockResolvedValue({
      siteUrl: "https://acme.atlassian.net",
      email: "me@acme.com",
      apiToken: "",
      hasToken: true,
      jql: "assignee = currentUser()",
      statusMapping: {},
      pollIntervalMinutes: 0,
    }),
    setConfig: vi.fn().mockResolvedValue({}),
    onPollTick: vi.fn(),
  };
}

describe("JiraTab", () => {
  beforeEach(() => {
    mockSyncNow.mockReset();
  });

  it("renders connection fields and a sync button on desktop", async () => {
    installJiraAPI();
    render(<JiraTab />, { wrapper: I18nWrapper });
    expect(await screen.findByLabelText(/site/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /sync now/i })).toBeInTheDocument();
  });

  it("shows last sync counts", async () => {
    installJiraAPI();
    render(<JiraTab />, { wrapper: I18nWrapper });
    expect(await screen.findByText(/3 created/i)).toBeInTheDocument();
  });

  it("renders a desktop-only notice when jiraAPI is absent", () => {
    delete (globalThis as unknown as { window: { jiraAPI?: unknown } }).window.jiraAPI;
    render(<JiraTab />, { wrapper: I18nWrapper });
    expect(screen.getByText(/desktop app/i)).toBeInTheDocument();
  });
});
