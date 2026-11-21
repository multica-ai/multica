import { StrictMode, type ReactNode } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError } from "@multica/core/api";
import enSettings from "../../locales/en/settings.json";
import { LarkChannelAuthorization } from "./lark-channel-authorization";
const mockBeginInstall = vi.hoisted(() => vi.fn());
const mockGetStatus = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: mockInvalidate }),
}));
vi.mock("@multica/core/lark", () => ({
  larkKeys: { installations: (id: string) => ["lark", id, "installations"] },
}));
vi.mock("@multica/core/api", async (original) => ({
  ...(await original<typeof import("@multica/core/api")>()),
  api: {
    beginLarkInstall: mockBeginInstall,
    getLarkInstallStatus: mockGetStatus,
  },
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), message: vi.fn() },
}));
vi.mock("react-qr-code", () => ({
  QRCode: ({ value }: { value: string }) => (
    <span data-testid="qr-code" data-value={value} />
  ),
}));
function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={{ en: { settings: enSettings } }}>
      {children}
    </I18nProvider>
  );
}
function StrictModeWrapper({ children }: { children: ReactNode }) {
  return (
    <StrictMode>
      <I18nWrapper>{children}</I18nWrapper>
    </StrictMode>
  );
}
function resetFixtures() {
  vi.clearAllMocks();
}

describe("LarkInstallDialog (polling terminal errors)", () => {
  beforeEach(() => {
    resetFixtures();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockBeginInstall.mockResolvedValue({
      session_id: "sess-1",
      qr_code_url: "https://accounts.feishu.cn/oauth/v1/device?u=abc",
      expires_in_seconds: 300,
      poll_interval_seconds: 2,
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  async function openDialog() {
    render(
      <LarkChannelAuthorization
        wsId="workspace-1"
        agentId="agent-1"
        region="feishu"
        onClose={vi.fn()}
      />,
      {
        wrapper: I18nWrapper,
      },
    );
    // Let the begin-session promise resolve and the QR render.
    await waitFor(() => {
      expect(screen.getByTestId("qr-code")).toBeTruthy();
    });
  }

  it("falls into a terminal session_lost error state when status polling 404s instead of looping forever", async () => {
    mockGetStatus.mockRejectedValue(
      new ApiError("install session not found", 404, "Not Found"),
    );

    await openDialog();
    // Drive the polling timer (intervalMs = max(2000, 2*1000)) and let
    // the rejected promise propagate into the catch.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2100);
    });

    await waitFor(() => {
      expect(
        screen.getByText(
          /Install session expired or was lost\. Scan again to start over\./i,
        ),
      ).toBeTruthy();
    });
    expect(screen.getByRole("button", { name: /Scan again/i })).toBeTruthy();
    // The dialog renders multiple Close affordances (footer button + the
    // built-in dialog dismiss); we only need to confirm at least one is
    // mounted alongside the retry button.
    expect(
      screen.getAllByRole("button", { name: /Close/i }).length,
    ).toBeGreaterThan(0);
  });

  it("treats 403 as a terminal forbidden error state (no infinite retry on revoked permission)", async () => {
    mockGetStatus.mockRejectedValue(
      new ApiError("forbidden", 403, "Forbidden"),
    );

    await openDialog();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2100);
    });

    await waitFor(() => {
      expect(
        screen.getByText(
          /You no longer have permission to install Lark Bots in this workspace/i,
        ),
      ).toBeTruthy();
    });
    // Drive another full poll interval — the polling loop must NOT
    // schedule a follow-up fetch after a terminal 4xx.
    const callsAfterTerminal = mockGetStatus.mock.calls.length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(mockGetStatus.mock.calls.length).toBe(callsAfterTerminal);
  });

  it("renders the QR after a React StrictMode double-mount (regression for empty dialog body)", async () => {
    render(
      <LarkChannelAuthorization
        wsId="workspace-1"
        agentId="agent-1"
        region="feishu"
        onClose={vi.fn()}
      />,
      {
        wrapper: StrictModeWrapper,
      },
    );

    // The QR must appear even though the dialog mounted, unmounted, and
    // mounted again under StrictMode. The previous bug left the body
    // empty here.
    await waitFor(
      () => {
        expect(screen.getByTestId("qr-code")).toBeTruthy();
      },
      { timeout: 2000 },
    );

    // And the QR's value should match what the (latest) begin call
    // returned — not be empty / undefined.
    const qr = screen.getByTestId("qr-code");
    expect(qr.getAttribute("data-value")).toBe(
      "https://accounts.feishu.cn/oauth/v1/device?u=abc",
    );
  });
});
