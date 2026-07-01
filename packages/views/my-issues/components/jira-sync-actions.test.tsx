import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enSettings from "../../locales/en/settings.json";

const TEST_RESOURCES = { en: { settings: enSettings } };

const mockSyncNow = vi.hoisted(() => vi.fn());
const mockClearSynced = vi.hoisted(() => vi.fn());

vi.mock("../../settings/jira/use-jira-sync", () => ({
  useJiraSync: () => ({
    syncNow: mockSyncNow,
    clearSynced: mockClearSynced,
    running: false,
    clearing: false,
    lastResult: null,
    clearedCount: null,
    error: null,
  }),
  getJiraBridge: () =>
    (globalThis as unknown as { window: { jiraAPI?: unknown } }).window.jiraAPI,
}));

import { JiraSyncActions } from "./jira-sync-actions";

function Wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient();
  return (
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        {children}
      </I18nProvider>
    </QueryClientProvider>
  );
}

describe("JiraSyncActions", () => {
  beforeEach(() => {
    mockSyncNow.mockReset().mockResolvedValue({
      created: 1,
      updated: 0,
      skipped: 0,
      commentsAdded: 0,
      errors: [],
    });
    mockClearSynced.mockReset().mockResolvedValue({ deleted: 2 });
  });

  it("renders sync and clear buttons on desktop", () => {
    (globalThis as unknown as { window: { jiraAPI: unknown } }).window.jiraAPI = {};
    render(<JiraSyncActions wsId="ws-1" />, { wrapper: Wrapper });
    expect(screen.getByRole("button", { name: /sync now/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /clear synced/i })).toBeInTheDocument();
  });

  it("renders nothing on web (no jira bridge)", () => {
    delete (globalThis as unknown as { window: { jiraAPI?: unknown } }).window.jiraAPI;
    const { container } = render(<JiraSyncActions wsId="ws-1" />, { wrapper: Wrapper });
    expect(container).toBeEmptyDOMElement();
  });

  it("triggers a sync on click", async () => {
    (globalThis as unknown as { window: { jiraAPI: unknown } }).window.jiraAPI = {};
    render(<JiraSyncActions wsId="ws-1" />, { wrapper: Wrapper });
    fireEvent.click(screen.getByRole("button", { name: /sync now/i }));
    await waitFor(() => expect(mockSyncNow).toHaveBeenCalledTimes(1));
  });

  it("clears only after confirming in the dialog", async () => {
    (globalThis as unknown as { window: { jiraAPI: unknown } }).window.jiraAPI = {};
    render(<JiraSyncActions wsId="ws-1" />, { wrapper: Wrapper });
    // Clicking the toolbar button opens the confirm dialog; it does not clear yet.
    fireEvent.click(screen.getByRole("button", { name: /clear synced/i }));
    expect(mockClearSynced).not.toHaveBeenCalled();
    // Once the dialog is open a second "clear synced" button (the confirm
    // action) appears; clicking it runs the clear.
    await waitFor(() =>
      expect(screen.getAllByRole("button", { name: /clear synced/i }).length).toBeGreaterThan(1),
    );
    const buttons = screen.getAllByRole("button", { name: /clear synced/i });
    fireEvent.click(buttons[buttons.length - 1]);
    await waitFor(() => expect(mockClearSynced).toHaveBeenCalledTimes(1));
  });
});
