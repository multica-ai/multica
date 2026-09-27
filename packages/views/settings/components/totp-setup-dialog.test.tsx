import type { ReactNode } from "react";
import { act, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockTOTPSetupInit = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    totpSetupInit: mockTOTPSetupInit,
    totpSetupVerify: vi.fn(),
  },
}));

vi.mock("react-qr-code", () => ({
  QRCode: ({ value }: { value: string }) => <div data-testid="qr-code">{value}</div>,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { TOTPSetupDialog } from "./totp-setup-dialog";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

describe("TOTPSetupDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("reuses a pending setup request when the dialog is reopened", async () => {
    let resolveSetup!: (value: { secret: string; otpauth_url: string }) => void;
    mockTOTPSetupInit.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveSetup = resolve;
      }),
    );
    const onOpenChange = vi.fn();
    const onSuccess = vi.fn();

    const { rerender } = render(
      <TOTPSetupDialog
        open
        onOpenChange={onOpenChange}
        onSuccess={onSuccess}
      />,
      { wrapper: Wrapper },
    );

    rerender(
      <TOTPSetupDialog
        open={false}
        onOpenChange={onOpenChange}
        onSuccess={onSuccess}
      />,
    );
    rerender(
      <TOTPSetupDialog
        open
        onOpenChange={onOpenChange}
        onSuccess={onSuccess}
      />,
    );

    expect(mockTOTPSetupInit).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolveSetup({ secret: "shared-secret", otpauth_url: "otpauth://shared" });
    });

    expect(await screen.findByTestId("qr-code")).toHaveTextContent(
      "otpauth://shared",
    );
    expect(screen.getByText("shared-secret")).toBeInTheDocument();
  });

  it("closes with an error instead of showing a QR code for a malformed setup", async () => {
    mockTOTPSetupInit.mockResolvedValueOnce({ secret: "", otpauth_url: "" });
    const onOpenChange = vi.fn();

    render(
      <TOTPSetupDialog open onOpenChange={onOpenChange} onSuccess={vi.fn()} />,
      { wrapper: Wrapper },
    );

    await act(async () => {});
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(screen.queryByTestId("qr-code")).not.toBeInTheDocument();
  });
});
