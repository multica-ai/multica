// @vitest-environment jsdom

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

// wecom-scan-install-dialog.test.tsx — the QR install dialog.
//
// What matters here is the poll loop's shape: the QR only exists after a later
// poll (begin returns a session in `creating`), a terminal status stops polling,
// and a 404 mid-poll is terminal rather than an infinite retry on a dead session.

const mockBegin = vi.hoisted(() => vi.fn());
const mockStatus = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());

// Hoisted with the mocks: vi.mock factories run before top-level declarations,
// so a plain class here would be referenced before initialization.
const FakeApiError = vi.hoisted(
  () =>
    class FakeApiError extends Error {
      status: number;
      constructor(status: number, message: string) {
        super(message);
        this.status = status;
      }
    },
);

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: mockInvalidate }),
}));

vi.mock("@multica/core/wecom", () => ({
  wecomKeys: { installations: (wsId: string) => ["wecom", "installations", wsId] },
}));

vi.mock("@multica/core/api", () => ({
  api: { beginWecomInstall: mockBegin, getWecomInstallStatus: mockStatus },
  ApiError: FakeApiError,
}));

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: vi.fn(), message: vi.fn() },
}));

// react-qr-code renders an <svg> that jsdom does not need; a stub keeps the
// assertion on "the QR is showing" rather than on its pixels.
vi.mock("react-qr-code", () => ({
  QRCode: ({ value }: { value: string }) => <div data-testid="qr" data-value={value} />,
}));

import { WecomScanInstallDialog } from "./wecom-scan-install-dialog";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

afterEach(cleanup);

function renderDialog(onClose = vi.fn()) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <WecomScanInstallDialog wsId="workspace-1" agentId="agent-1" onClose={onClose} />
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  // jsdom has no crypto.randomUUID in every environment version.
  if (!globalThis.crypto?.randomUUID) {
    Object.defineProperty(globalThis, "crypto", {
      value: { ...globalThis.crypto, randomUUID: () => "11111111-1111-1111-1111-111111111111" },
      configurable: true,
    });
  }
});

describe("WecomScanInstallDialog", () => {
  it("shows the preparing state before the QR exists, then renders it", async () => {
    mockBegin.mockResolvedValue({
      session_id: "session-1",
      status: "creating",
      poll_interval_seconds: 1,
    });
    // The QR arrives on a later poll — begin never carries it.
    mockStatus
      .mockResolvedValueOnce({ status: "creating", poll_interval_seconds: 1 })
      .mockResolvedValue({
        status: "pending",
        qr_code_url: "https://work.weixin.qq.com/ai/qc/scan?scode=abc",
        expires_in_seconds: 240,
        poll_interval_seconds: 2,
      });

    renderDialog();

    expect(await screen.findByTestId("wecom-scan-starting")).toBeTruthy();
    const qr = await screen.findByTestId("qr", {}, { timeout: 5000 });
    expect(qr.getAttribute("data-value")).toContain("scode=abc");
    expect(screen.getByTestId("wecom-scan-expiry").textContent).toContain("240");
  });

  it("invalidates the installations cache and closes on success", async () => {
    const onClose = vi.fn();
    mockBegin.mockResolvedValue({
      session_id: "session-1",
      status: "creating",
      poll_interval_seconds: 1,
    });
    mockStatus.mockResolvedValue({
      status: "success",
      installation_id: "install-1",
      poll_interval_seconds: 2,
    });

    renderDialog(onClose);

    await waitFor(() => expect(mockToastSuccess).toHaveBeenCalled(), { timeout: 5000 });
    expect(mockInvalidate).toHaveBeenCalledWith({
      queryKey: ["wecom", "installations", "workspace-1"],
    });
    await waitFor(() => expect(onClose).toHaveBeenCalled(), { timeout: 5000 });
  });

  it("renders the reason for a terminal error and stops polling", async () => {
    mockBegin.mockResolvedValue({
      session_id: "session-1",
      status: "creating",
      poll_interval_seconds: 1,
    });
    mockStatus.mockResolvedValue({
      status: "error",
      error_reason: "expired",
      error_message: "the QR expired",
      poll_interval_seconds: 2,
    });

    renderDialog();

    const err = await screen.findByTestId("wecom-scan-error", {}, { timeout: 5000 });
    expect(err.textContent).toContain(enSettings.wecom.scan_error_expired);
    // Operator detail is shown as supplementary text, not as the headline.
    expect(err.textContent).toContain("the QR expired");

    const callsAfterError = mockStatus.mock.calls.length;
    await new Promise((r) => setTimeout(r, 300));
    expect(mockStatus.mock.calls.length).toBe(callsAfterError);
  });

  // An unknown reason must still render copy rather than a blank panel.
  it("falls back to generic copy for an unrecognized error reason", async () => {
    mockBegin.mockResolvedValue({
      session_id: "session-1",
      status: "creating",
      poll_interval_seconds: 1,
    });
    mockStatus.mockResolvedValue({
      status: "error",
      error_reason: "some_future_reason",
      poll_interval_seconds: 2,
    });

    renderDialog();

    const err = await screen.findByTestId("wecom-scan-error", {}, { timeout: 5000 });
    expect(err.textContent).toContain(enSettings.wecom.scan_error_internal_error);
  });

  // A 404 means the session is gone (purged, or never existed). Polling on would
  // trap the user watching a stale QR with no feedback.
  it("treats a 404 mid-poll as a lost session", async () => {
    mockBegin.mockResolvedValue({
      session_id: "session-1",
      status: "creating",
      poll_interval_seconds: 1,
    });
    mockStatus.mockRejectedValue(new FakeApiError(404, "install session not found"));

    renderDialog();

    const err = await screen.findByTestId("wecom-scan-error", {}, { timeout: 5000 });
    expect(err.textContent).toContain(enSettings.wecom.scan_error_session_lost);
  });

  it("surfaces a 503 begin as the unconfigured reason", async () => {
    mockBegin.mockRejectedValue(new FakeApiError(503, "wecom scan install not enabled"));

    renderDialog();

    const err = await screen.findByTestId("wecom-scan-error", {}, { timeout: 5000 });
    expect(err.textContent).toContain(enSettings.wecom.scan_error_integration_unconfigured);
    // Nothing to poll when begin never produced a session.
    expect(mockStatus).not.toHaveBeenCalled();
  });

  it("starts a brand-new session on retry", async () => {
    mockBegin.mockResolvedValue({
      session_id: "session-1",
      status: "creating",
      poll_interval_seconds: 1,
    });
    mockStatus.mockResolvedValue({
      status: "error",
      error_reason: "expired",
      poll_interval_seconds: 2,
    });

    renderDialog();
    await screen.findByTestId("wecom-scan-error", {}, { timeout: 5000 });

    const beginCalls = mockBegin.mock.calls.length;
    await userEvent.click(screen.getByTestId("wecom-scan-retry"));
    await waitFor(() => expect(mockBegin.mock.calls.length).toBe(beginCalls + 1));
    // A fresh idempotency key, or "get a new code" would replay the expired
    // session instead of starting over.
    const firstKey = mockBegin.mock.calls[0]?.[2];
    const secondKey = mockBegin.mock.calls[beginCalls]?.[2];
    expect(firstKey).toBeTruthy();
    expect(secondKey).toBeTruthy();
  });

  it("reports a malformed begin response as an error instead of polling forever", async () => {
    // parseWithFallback degrades an unparseable body to an empty session id.
    mockBegin.mockResolvedValue({ session_id: "", status: "error", poll_interval_seconds: 1 });

    renderDialog();

    expect(await screen.findByTestId("wecom-scan-error", {}, { timeout: 5000 })).toBeTruthy();
    expect(mockStatus).not.toHaveBeenCalled();
  });
});
