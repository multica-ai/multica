import type { ReactNode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockTOTPDisable = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return { ApiError: actual.ApiError, api: { totpDisable: mockTOTPDisable } };
});

vi.mock("sonner", () => ({
  toast: { error: mockToastError, success: vi.fn() },
}));

import { ApiError } from "@multica/core/api";
import { TOTPDisableDialog } from "./totp-disable-dialog";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

describe("TOTPDisableDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("names the code input and explains a lockout instead of an invalid code", async () => {
    mockTOTPDisable.mockRejectedValueOnce(
      new ApiError("too many invalid codes; try again later", 429, "Too Many Requests"),
    );
    const onOpenChange = vi.fn();
    render(
      <TOTPDisableDialog open onOpenChange={onOpenChange} onSuccess={vi.fn()} />,
      { wrapper: Wrapper },
    );

    const input = screen.getByRole("textbox", {
      name: enSettings.security.two_factor.disable_prompt,
    });
    const user = userEvent.setup();
    await user.type(input, "123456");
    await user.click(screen.getByRole("button", { name: enSettings.security.two_factor.confirm }));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith(enSettings.security.two_factor.disable_locked);
    });
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });
});
